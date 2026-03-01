package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
	"github.com/francescoalemanno/raijin-mono/libagent"
)

const grepDescription = "Search file contents for a pattern. Returns matching lines with file paths and line numbers."

// regexCache provides thread-safe caching of compiled regex patterns with FIFO eviction.
type regexCache struct {
	cache   map[string]*regexp.Regexp
	order   []string
	maxSize int
	mu      sync.RWMutex
}

func newRegexCache() *regexCache {
	return &regexCache{
		cache:   make(map[string]*regexp.Regexp),
		order:   make([]string, 0, 128),
		maxSize: 128,
	}
}

func (rc *regexCache) get(pattern string) (*regexp.Regexp, error) {
	rc.mu.RLock()
	if regex, exists := rc.cache[pattern]; exists {
		rc.mu.RUnlock()
		return regex, nil
	}
	rc.mu.RUnlock()

	rc.mu.Lock()
	defer rc.mu.Unlock()

	if regex, exists := rc.cache[pattern]; exists {
		return regex, nil
	}

	for len(rc.cache) >= rc.maxSize {
		oldest := rc.order[0]
		rc.order = rc.order[1:]
		delete(rc.cache, oldest)
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	rc.cache[pattern] = regex
	rc.order = append(rc.order, pattern)
	return regex, nil
}

var searchRegexCache = newRegexCache()

type grepParams struct {
	Pattern     string `json:"pattern" description:"Search pattern (regex or literal string)"`
	Path        string `json:"path,omitempty" description:"Directory or file to search (default: current directory)"`
	Include     string `json:"include,omitempty" description:"Filter files by glob pattern, e.g. '*.go' or '**/*.test.go'"`
	LiteralText bool   `json:"literal_text,omitempty" description:"Treat pattern as literal string instead of regex (default: false)"`
}

type grepMatch struct {
	path     string
	modTime  time.Time
	lineNum  int
	charNum  int
	lineText string
}

// NewGrepTool creates a grep tool for searching file contents.
func NewGrepTool() libagent.Tool {
	handler := func(ctx context.Context, params grepParams, call libagent.ToolCall) (libagent.ToolResponse, error) {
		if params.Pattern == "" {
			return libagent.NewTextErrorResponse("pattern is required"), nil
		}

		searchPattern := params.Pattern
		if params.LiteralText {
			searchPattern = regexp.QuoteMeta(params.Pattern)
		}

		searchPath := params.Path
		if searchPath == "" {
			searchPath = "."
		}
		searchPath = fsutil.ExpandPath(searchPath)

		matches, err := searchFiles(ctx, searchPattern, searchPath, params.Include)
		if err != nil {
			if ctx.Err() != nil {
				return libagent.ToolResponse{}, ctx.Err()
			}
			return libagent.NewTextErrorResponse(fmt.Sprintf("searching files: %s", err)), nil
		}

		var output strings.Builder
		if len(matches) == 0 {
			output.WriteString("No files found")
		} else {
			fmt.Fprintf(&output, "Found %d matches\n", len(matches))

			currentFile := ""
			for _, match := range matches {
				if currentFile != match.path {
					if currentFile != "" {
						output.WriteString("\n")
					}
					currentFile = match.path
					fmt.Fprintf(&output, "%s:\n", fsutil.NormalizePath(match.path))
				}
				if match.lineNum > 0 {
					lineText, _ := TruncateLine(match.lineText, GrepMaxLineLength)
					if match.charNum > 0 {
						fmt.Fprintf(&output, "  Line %d, Char %d: %s\n", match.lineNum, match.charNum, lineText)
					} else {
						fmt.Fprintf(&output, "  Line %d: %s\n", match.lineNum, lineText)
					}
				} else {
					fmt.Fprintf(&output, "  %s\n", match.path)
				}
			}

		}

		return libagent.NewTextResponse(output.String()), nil
	}

	renderFunc := func(input json.RawMessage, output string, _ int) string {
		var params grepParams
		if err := libagent.ParseJSONInput(input, &params); err != nil {
			return "grep (failed)"
		}
		var parts []string
		parts = append(parts, fmt.Sprintf("grep %q", params.Pattern))
		if params.Path != "" && params.Path != "." {
			path := RenderPath(params.Path)
			parts = append(parts, fmt.Sprintf("in %s", path))
		}
		if params.Include != "" {
			parts = append(parts, fmt.Sprintf("(%s)", params.Include))
		}
		header := strings.Join(parts, " ")
		if output == "" {
			return header
		}
		return header + "\n" + output
	}

	return WithRender(
		libagent.NewParallelTypedTool("grep", grepDescription, handler),
		renderFunc,
	)
}

func searchFiles(ctx context.Context, pattern, rootPath, include string) ([]grepMatch, error) {
	matches, err := searchFilesWithRegex(ctx, pattern, rootPath, include)
	if err != nil {
		return nil, err
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})

	return matches, nil
}

func searchFilesWithRegex(ctx context.Context, pattern, rootPath, include string) ([]grepMatch, error) {
	var matches []grepMatch

	regex, err := searchRegexCache.get(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	err = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if info.IsDir() {
			name := filepath.Base(path)
			if fsutil.VCSDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		if include != "" {
			matched, _ := doublestar.Match(fsutil.NormalizePath(include), fsutil.NormalizePath(path))
			if !matched {
				return nil
			}
		}

		match, lineNum, charNum, lineText, err := fileContainsPattern(path, regex)
		if err != nil {
			return nil
		}

		if match {
			matches = append(matches, grepMatch{
				path:     path,
				modTime:  info.ModTime(),
				lineNum:  lineNum,
				charNum:  charNum,
				lineText: lineText,
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return matches, nil
}

func fileContainsPattern(filePath string, pattern *regexp.Regexp) (bool, int, int, string, error) {
	if !fsutil.IsTextFile(filePath) {
		return false, 0, 0, "", nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false, 0, 0, "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, defaultScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if loc := pattern.FindStringIndex(line); loc != nil {
			charNum := loc[0] + 1
			return true, lineNum, charNum, line, nil
		}
	}

	return false, 0, 0, "", scanner.Err()
}
