package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
	"github.com/francescoalemanno/raijin-mono/libagent"
)

const editDescription = "Edit a file by replacing exact text. The oldText must match exactly (including whitespace). Use this for precise, surgical edits."

type editParams struct {
	Path    string `json:"path" description:"Path to the file to edit (relative or absolute)"`
	OldText string `json:"oldText" description:"Exact text to find and replace (must match exactly)"`
	NewText string `json:"newText" description:"New text to replace the old text with"`
}

// NewEditTool creates an edit tool.
func NewEditTool() libagent.Tool {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return createEditTool(cwd)
}

func createEditTool(cwd string) libagent.Tool {
	handler := func(ctx context.Context, params editParams, call libagent.ToolCall) (libagent.ToolResponse, error) {
		absolutePath := fsutil.ResolveToCwd(params.Path, cwd)

		buffer, err := os.ReadFile(absolutePath)
		if err != nil {
			if os.IsNotExist(err) {
				return libagent.NewTextErrorResponse(fmt.Sprintf("File not found: %s", params.Path)), nil
			}
			return libagent.NewTextErrorResponse(err.Error()), nil
		}
		rawContent := string(buffer)

		bom, content := stripBom(rawContent)
		originalEnding := detectLineEnding(content)
		normalizedContent := normalizeToLF(content)
		normalizedOldText := normalizeToLF(params.OldText)
		normalizedNewText := normalizeToLF(params.NewText)

		matchResult := fuzzyFindText(normalizedContent, normalizedOldText)
		if !matchResult.Found {
			return libagent.NewTextErrorResponse(fmt.Sprintf(
				"Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.",
				params.Path,
			)), nil
		}

		fuzzyContent := normalizeForFuzzyMatch(normalizedContent)
		fuzzyOldText := normalizeForFuzzyMatch(normalizedOldText)
		occurrences := len(strings.Split(fuzzyContent, fuzzyOldText)) - 1
		if occurrences > 1 {
			return libagent.NewTextErrorResponse(fmt.Sprintf(
				"Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.",
				occurrences,
				params.Path,
			)), nil
		}

		if err := ctx.Err(); err != nil {
			return libagent.ToolResponse{}, err
		}

		baseContent := matchResult.ContentForReplacement
		newContent := baseContent[:matchResult.Index] + normalizedNewText + baseContent[matchResult.Index+matchResult.MatchLength:]
		if baseContent == newContent {
			return libagent.NewTextErrorResponse(fmt.Sprintf(
				"No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.",
				params.Path,
			)), nil
		}

		finalContent := bom + restoreLineEndings(newContent, originalEnding)
		if err := os.WriteFile(absolutePath, []byte(finalContent), defaultFilePerm); err != nil {
			return libagent.NewTextErrorResponse(err.Error()), nil
		}

		details := generateDiffString(baseContent, newContent, 4)
		resp := libagent.NewTextResponse(fmt.Sprintf("Successfully replaced text in %s.", params.Path))
		resp = libagent.WithResponseMetadata(resp, details)
		return resp, nil
	}

	renderFunc := func(input json.RawMessage, output string, _ int) string {
		var params editParams
		if err := libagent.ParseJSONInput(input, &params); err != nil {
			return "edit (failed)"
		}
		header := fmt.Sprintf("edit %s", RenderPath(params.Path))
		if params.OldText == "" && params.NewText == "" {
			return header
		}

		if diff := renderDiffPreview(params.Path, params.OldText, params.NewText); diff != "" {
			return header + "\n" + diff
		}
		return header
	}

	return WithRender(
		libagent.NewParallelTypedTool("edit", editDescription, handler),
		renderFunc,
	)
}
