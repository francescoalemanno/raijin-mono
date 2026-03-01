package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
	"github.com/francescoalemanno/raijin-mono/internal/input"
	"github.com/francescoalemanno/raijin-mono/libagent"
	"golang.org/x/text/unicode/norm"
)

var readDescription = fmt.Sprintf(
	"Read the contents of a file. Supports text files and images (jpg, png, gif, webp). Images are sent as attachments. For text files, output is truncated to %d lines or %dKB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete.",
	DefaultMaxLines,
	DefaultMaxBytes/1024,
)

var readAMPMVariantRe = regexp.MustCompile(` (AM|PM)\.`)

const narrowNoBreakSpace = "\u202F"

type readParams struct {
	Path   string `json:"path" description:"Path to the file or directory to read (relative or absolute)"`
	Offset *int   `json:"offset,omitempty" description:"Line number to start reading from (1-indexed)"`
	Limit  *int   `json:"limit,omitempty" description:"Maximum number of lines to read"`
}

type readToolDetails struct {
	Truncation *TruncationResult `json:"truncation,omitempty"`
}

func readToolNudge(content string) libagent.ToolResponse {
	return libagent.NewTextErrorResponse("An error during the call of the read tool happened: " + content + "\n\nFix the tool call and retry, provide a `path` parameter, if path is wrong use `glob` tool to find paths.")
}

// NewReadTool creates a read tool for reading files and directories.
func NewReadTool() libagent.Tool {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	handler := func(ctx context.Context, params readParams, call libagent.ToolCall) (libagent.ToolResponse, error) {
		if resp, blocked := toolExecutionGate(ctx, "read"); blocked {
			return resp, nil
		}
		if strings.TrimSpace(params.Path) == "" {
			return readToolNudge("path is required"), nil
		}

		resolvedPath := resolveReadPath(params.Path, cwd)
		fileInfo, err := os.Stat(resolvedPath)
		if err != nil {
			if os.IsNotExist(err) {
				return readToolNudge(fileNotFoundError(resolvedPath)), nil
			}
			return readToolNudge(fmt.Sprintf("accessing path: %s", err)), nil
		}

		if fileInfo.IsDir() {
			result, details, err := readDirectory(ctx, resolvedPath, params)
			if err != nil {
				if ctx.Err() != nil {
					return libagent.ToolResponse{}, ctx.Err()
				}
				return libagent.NewTextErrorResponse(err.Error()), nil
			}
			resp := libagent.NewTextResponse(result)
			if details != nil {
				resp = libagent.WithResponseMetadata(resp, details)
			}
			return resp, nil
		}

		ext := strings.TrimPrefix(filepath.Ext(resolvedPath), ".")
		if mediaType := input.ImageMediaType(ext); mediaType != "" {
			if fileInfo.Size() > input.MaxImageSize {
				return readToolNudge(fmt.Sprintf("image too large (%d bytes); maximum is %d bytes", fileInfo.Size(), input.MaxImageSize)), nil
			}
			data, err := os.ReadFile(resolvedPath)
			if err != nil {
				return readToolNudge(fmt.Sprintf("reading image: %s", err)), nil
			}
			encoded := base64.StdEncoding.EncodeToString(data)
			return libagent.NewMediaResponse([]byte(encoded), mediaType), nil
		}

		result, details, err := readText(ctx, resolvedPath, params)
		if err != nil {
			if ctx.Err() != nil {
				return libagent.ToolResponse{}, ctx.Err()
			}
			return libagent.NewTextErrorResponse(err.Error()), nil
		}

		resp := libagent.NewTextResponse(result)
		if details != nil {
			resp = libagent.WithResponseMetadata(resp, details)
		}
		return resp, nil
	}

	renderFunc := func(input json.RawMessage, output string, _ int) string {
		var params readParams
		if err := libagent.ParseJSONInput(input, &params); err != nil {
			return "read (failed)"
		}

		path := RenderPath(params.Path)
		header := fmt.Sprintf("read %s", path)
		if params.Offset != nil || params.Limit != nil {
			offset := 1
			if params.Offset != nil {
				offset = max(1, *params.Offset)
			}
			if params.Limit != nil {
				limit := max(0, *params.Limit)
				header = fmt.Sprintf("read %s [%d:%d]", path, offset, offset+limit)
			} else {
				header = fmt.Sprintf("read %s [%d:]", path, offset)
			}
		}

		if output == "" {
			return header
		}
		return header + "\n" + renderCodePreview(params.Path, output)
	}

	return WithRender(
		libagent.NewParallelTypedTool("read", readDescription, handler),
		renderFunc,
	)
}

func readDirectory(ctx context.Context, dirPath string, params readParams) (string, *readToolDetails, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", nil, fmt.Errorf("reading directory: %w", err)
	}

	totalEntries := len(entries)
	start := readOffset(params.Offset)
	if start >= totalEntries && totalEntries > 0 {
		return "", nil, fmt.Errorf("offset %d is beyond end of directory (%d entries total)", readOffsetDisplay(params.Offset), totalEntries)
	}

	all := make([]string, 0, max(0, totalEntries-start))
	for i := start; i < totalEntries; i++ {
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		name := entries[i].Name()
		if entries[i].IsDir() {
			name += "/"
		}
		all = append(all, name)
	}

	selected := all
	userLimitedEntries := -1
	if params.Limit != nil {
		limit := max(0, *params.Limit)
		if limit < len(selected) {
			selected = selected[:limit]
		}
		userLimitedEntries = len(selected)
	}

	raw := strings.Join(selected, "\n")
	truncation := TruncateHead(raw, TruncationOptions{})
	outputText := truncation.Content
	var details *readToolDetails

	switch {
	case truncation.FirstLineExceedsLimit:
		outputText = fmt.Sprintf("[Entry %d exceeds %s limit. Narrow your path and retry.]", start+1, FormatSize(DefaultMaxBytes))
		details = detailsFromTruncation(truncation)
	case truncation.Truncated:
		endEntryDisplay := start + truncation.OutputLines
		nextOffset := endEntryDisplay + 1
		if truncation.TruncatedBy == "lines" {
			outputText += fmt.Sprintf("\n\n[Showing entries %d-%d of %d. Use offset=%d to continue.]", start+1, endEntryDisplay, totalEntries, nextOffset)
		} else {
			outputText += fmt.Sprintf("\n\n[Showing entries %d-%d of %d (%s limit). Use offset=%d to continue.]", start+1, endEntryDisplay, totalEntries, FormatSize(DefaultMaxBytes), nextOffset)
		}
		details = detailsFromTruncation(truncation)
	case userLimitedEntries >= 0 && start+userLimitedEntries < totalEntries:
		remaining := totalEntries - (start + userLimitedEntries)
		nextOffset := start + userLimitedEntries + 1
		if outputText == "" {
			outputText = "(empty directory)"
		}
		outputText += fmt.Sprintf("\n\n[%d more entries. Use offset=%d to continue.]", remaining, nextOffset)
	}

	if outputText == "" {
		outputText = "(empty directory)"
	}
	return outputText, details, nil
}

func readText(ctx context.Context, path string, params readParams) (string, *readToolDetails, error) {
	if ctx.Err() != nil {
		return "", nil, ctx.Err()
	}

	buffer, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("opening file: %w", err)
	}

	content := string(buffer)
	if !utf8.ValidString(content) {
		return "", nil, errors.New("file content is not valid UTF-8")
	}

	allLines := strings.Split(content, "\n")
	totalFileLines := len(allLines)

	startLine := readOffset(params.Offset)
	startLineDisplay := startLine + 1
	if startLine >= totalFileLines {
		return "", nil, fmt.Errorf("offset %d is beyond end of file (%d lines total)", readOffsetDisplay(params.Offset), totalFileLines)
	}

	var selectedContent string
	userLimitedLines := -1
	if params.Limit != nil {
		limit := max(0, *params.Limit)
		endLine := min(startLine+limit, totalFileLines)
		selectedContent = strings.Join(allLines[startLine:endLine], "\n")
		userLimitedLines = endLine - startLine
	} else {
		selectedContent = strings.Join(allLines[startLine:], "\n")
	}

	truncation := TruncateHead(selectedContent, TruncationOptions{})
	var outputText string
	var details *readToolDetails

	switch {
	case truncation.FirstLineExceedsLimit:
		firstLineSize := FormatSize(len(allLines[startLine]))
		outputText = fmt.Sprintf(
			"[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startLineDisplay,
			firstLineSize,
			FormatSize(DefaultMaxBytes),
			startLineDisplay,
			params.Path,
			DefaultMaxBytes,
		)
		details = detailsFromTruncation(truncation)
	case truncation.Truncated:
		endLineDisplay := startLineDisplay + truncation.OutputLines - 1
		nextOffset := endLineDisplay + 1
		outputText = truncation.Content
		if truncation.TruncatedBy == "lines" {
			outputText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startLineDisplay, endLineDisplay, totalFileLines, nextOffset)
		} else {
			outputText += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]", startLineDisplay, endLineDisplay, totalFileLines, FormatSize(DefaultMaxBytes), nextOffset)
		}
		details = detailsFromTruncation(truncation)
	case userLimitedLines >= 0 && startLine+userLimitedLines < totalFileLines:
		remaining := totalFileLines - (startLine + userLimitedLines)
		nextOffset := startLine + userLimitedLines + 1
		outputText = truncation.Content
		outputText += fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset)
	default:
		outputText = truncation.Content
	}

	return outputText, details, nil
}

func detailsFromTruncation(truncation TruncationResult) *readToolDetails {
	copy := truncation
	return &readToolDetails{Truncation: &copy}
}

func readOffset(offset *int) int {
	if offset == nil {
		return 0
	}
	return max(0, *offset-1)
}

func readOffsetDisplay(offset *int) int {
	if offset == nil {
		return 1
	}
	if *offset < 1 {
		return 1
	}
	return *offset
}

func fileNotFoundError(filePath string) string {
	dir := filepath.Dir(filePath)
	base := filepath.Base(filePath)

	dirEntries, dirErr := os.ReadDir(dir)
	if dirErr == nil {
		var suggestions []string
		for _, entry := range dirEntries {
			if strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(base)) ||
				strings.Contains(strings.ToLower(base), strings.ToLower(entry.Name())) {
				suggestions = append(suggestions, filepath.Join(dir, entry.Name()))
				if len(suggestions) >= 3 {
					break
				}
			}
		}

		if len(suggestions) > 0 {
			return fmt.Sprintf("File not found: %s\n\nDid you mean one of these?\n%s",
				filePath, strings.Join(suggestions, "\n"))
		}
	}

	return fmt.Sprintf("File not found: %s", filePath)
}

func resolveReadPath(filePath, cwd string) string {
	resolved := fsutil.ResolveToCwd(filePath, cwd)
	if fileExists(resolved) {
		return resolved
	}

	amPmVariant := tryMacOSScreenshotPath(resolved)
	if amPmVariant != resolved && fileExists(amPmVariant) {
		return amPmVariant
	}

	nfdVariant := tryNFDVariant(resolved)
	if nfdVariant != resolved && fileExists(nfdVariant) {
		return nfdVariant
	}

	curlyVariant := tryCurlyQuoteVariant(resolved)
	if curlyVariant != resolved && fileExists(curlyVariant) {
		return curlyVariant
	}

	nfdCurlyVariant := tryCurlyQuoteVariant(nfdVariant)
	if nfdCurlyVariant != resolved && fileExists(nfdCurlyVariant) {
		return nfdCurlyVariant
	}

	return resolved
}

func tryMacOSScreenshotPath(filePath string) string {
	return readAMPMVariantRe.ReplaceAllString(filePath, narrowNoBreakSpace+"$1.")
}

func tryNFDVariant(filePath string) string {
	return norm.NFD.String(filePath)
}

func tryCurlyQuoteVariant(filePath string) string {
	return strings.ReplaceAll(filePath, "'", "\u2019")
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}
