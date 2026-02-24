package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

const (
	globDescription  = "Search for files by glob pattern. Returns matching file paths relative to the search directory. Output is truncated to 1000 results or 50KB (whichever is hit first)."
	defaultGlobLimit = 1000
	minGlobLimit     = 10
)

type globParams struct {
	Pattern string `json:"pattern" description:"Glob pattern to match files, e.g. '*.go', '**/*.json', or 'src/**/*.test.go'"`
	Path    string `json:"path,omitempty" description:"Directory to search in (default: current directory)"`
	Limit   int    `json:"limit,omitempty" description:"Maximum number of results (default: 1000, minimum: 10)"`
}

type globToolDetails struct {
	Truncation         *TruncationResult `json:"truncation,omitempty"`
	ResultLimitReached *int              `json:"resultLimitReached,omitempty"`
}

// NewGlobTool creates a glob tool for finding files by pattern.
func NewGlobTool() llm.Tool {
	handler := func(ctx context.Context, params globParams, _ llm.ToolCall) (llm.ToolResponse, error) {
		if resp, blocked := toolExecutionGate(ctx, "glob"); blocked {
			return resp, nil
		}
		if params.Pattern == "" {
			return llm.NewTextErrorResponse("pattern is required"), nil
		}

		searchPath := params.Path
		if searchPath == "" {
			searchPath = "."
		}
		cwd, err := os.Getwd()
		if err != nil {
			return llm.NewTextErrorResponse(fmt.Sprintf("resolving cwd: %v", err)), nil
		}
		searchPath = fsutil.ResolveToCwd(searchPath, cwd)

		info, err := os.Stat(searchPath)
		if err != nil {
			if os.IsNotExist(err) {
				return llm.NewTextErrorResponse(fmt.Sprintf("Path not found: %s", searchPath)), nil
			}
			return llm.NewTextErrorResponse(fmt.Sprintf("accessing path: %s", err)), nil
		}
		if !info.IsDir() {
			return llm.NewTextErrorResponse(fmt.Sprintf("Path is not a directory: %s", searchPath)), nil
		}

		files, err := globWithDoubleStar(ctx, params.Pattern, searchPath)
		if err != nil {
			if ctx.Err() != nil {
				return llm.ToolResponse{}, ctx.Err()
			}
			return llm.NewTextErrorResponse(fmt.Sprintf("finding files: %s", err)), nil
		}

		if len(files) == 0 {
			return llm.NewTextResponse("No files found matching pattern"), nil
		}

		effectiveLimit := defaultGlobLimit
		if params.Limit > 0 {
			effectiveLimit = max(minGlobLimit, params.Limit)
		}

		limitReached := len(files) > effectiveLimit
		if limitReached {
			files = files[:effectiveLimit]
		}

		rawOutput := strings.Join(files, "\n")
		truncation := TruncateHead(rawOutput, TruncationOptions{
			MaxLines: max(1, len(files)),
			MaxBytes: DefaultMaxBytes,
		})

		output := truncation.Content
		details := globToolDetails{}
		notices := make([]string, 0, 2)

		if limitReached {
			notices = append(notices, fmt.Sprintf(
				"%d results limit reached. Use limit=%d for more, or refine pattern",
				effectiveLimit,
				effectiveLimit*2,
			))
			limitCopy := effectiveLimit
			details.ResultLimitReached = &limitCopy
		}

		if truncation.Truncated {
			notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
			truncationCopy := truncation
			details.Truncation = &truncationCopy
		}

		if len(notices) > 0 {
			output += "\n\n[" + strings.Join(notices, ". ") + "]"
		}

		resp := llm.NewTextResponse(output)
		if details.ResultLimitReached != nil || details.Truncation != nil {
			resp = llm.WithResponseMetadata(resp, details)
		}
		return resp, nil
	}

	renderFunc := func(input json.RawMessage, output string, _ int) string {
		var params globParams
		if err := llm.ParseJSONInput(input, &params); err != nil {
			return "glob (failed)"
		}

		header := fmt.Sprintf("glob %s", params.Pattern)
		if params.Path != "" && params.Path != "." {
			header += fmt.Sprintf(" in %s", RenderPath(params.Path))
		}
		if params.Limit > 0 {
			header += fmt.Sprintf(" (limit=%d)", max(minGlobLimit, params.Limit))
		}
		if output == "" {
			return header
		}
		return header + "\n" + output
	}

	return WithRender(
		llm.NewParallelAgentTool("glob", globDescription, handler),
		renderFunc,
	)
}

type fileInfo struct {
	path    string
	modTime int64
}

func globWithDoubleStar(ctx context.Context, pattern, searchPath string) ([]string, error) {
	pattern = fsutil.NormalizePath(pattern)

	if _, err := doublestar.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %w", err)
	}

	var found []fileInfo
	err := filepath.WalkDir(searchPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			name := filepath.Base(path)
			if fsutil.VCSDirs[name] || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(searchPath, path)
		if err != nil {
			return nil
		}
		relPath = fsutil.NormalizePath(relPath)

		matched, err := doublestar.Match(pattern, relPath)
		if err != nil {
			return fmt.Errorf("invalid glob pattern: %w", err)
		}
		if !matched {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		found = append(found, fileInfo{path: relPath, modTime: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return nil, fmt.Errorf("walk error: %w", err)
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].modTime > found[j].modTime
	})

	results := make([]string, len(found))
	for i, m := range found {
		results[i] = m.path
	}
	return results, nil
}
