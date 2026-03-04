package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
	"github.com/francescoalemanno/raijin-mono/internal/vfs"
	"github.com/francescoalemanno/raijin-mono/libagent"
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
func NewGlobTool() libagent.Tool {
	v := vfs.NewFromWD()

	handler := func(ctx context.Context, params globParams, _ libagent.ToolCall) (libagent.ToolResponse, error) {
		if params.Pattern == "" {
			return libagent.NewTextErrorResponse("pattern is required"), nil
		}

		searchPath := params.Path
		if searchPath == "" {
			searchPath = "."
		}

		info, err := v.Stat(searchPath)
		if err != nil {
			return libagent.NewTextErrorResponse(vfs.DescribeAccessError(searchPath, err)), nil
		}
		if !info.IsDir() {
			return libagent.NewTextErrorResponse(fmt.Sprintf("Path is not a directory: %s", searchPath)), nil
		}

		files, err := globWithDoubleStar(ctx, v, params.Pattern, searchPath)
		if err != nil {
			if ctx.Err() != nil {
				return libagent.ToolResponse{}, ctx.Err()
			}
			return libagent.NewTextErrorResponse(fmt.Sprintf("finding files: %s", err)), nil
		}

		if len(files) == 0 {
			return libagent.NewTextResponse("No files found matching pattern"), nil
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

		resp := libagent.NewTextResponse(output)
		if details.ResultLimitReached != nil || details.Truncation != nil {
			resp = libagent.WithResponseMetadata(resp, details)
		}
		return resp, nil
	}

	renderFunc := func(input json.RawMessage, output string, _ int) string {
		var params globParams
		if err := libagent.ParseJSONInput(input, &params); err != nil {
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
		libagent.NewParallelTypedTool("glob", globDescription, handler),
		renderFunc,
	)
}

type fileInfo struct {
	path    string
	modTime int64
}

func globWithDoubleStar(ctx context.Context, v vfs.FS, pattern, searchPath string) ([]string, error) {
	pattern = fsutil.NormalizePath(pattern)

	if _, err := doublestar.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("invalid glob pattern: %w", err)
	}

	resolvedRoot, err := v.Resolve(searchPath)
	if err != nil {
		return nil, err
	}

	var found []fileInfo
	err = v.Walk(searchPath, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			name := d.Name()
			if fsutil.VCSDirs[name] || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}

		relPath, err := vfs.RelToRoot(resolvedRoot, p)
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

// relative paths are resolved through vfs.RelToRoot inline at call sites.
