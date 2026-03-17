package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	shellrun "github.com/francescoalemanno/raijin-mono/internal/shell"
	"github.com/francescoalemanno/raijin-mono/libagent"
)

type bashParams struct {
	Command string `json:"command" description:"Bash script to execute"`
	Timeout int    `json:"timeout,omitempty" description:"Timeout in seconds (optional, no default timeout)"`
}

type bashToolDetails struct {
	Truncation     *TruncationResult `json:"truncation,omitempty"`
	FullOutputPath string            `json:"fullOutputPath,omitempty"`
}

func RenderBashSingleLinePreview(toolCallParams string) string {
	return renderSingleLineForTool("bash", toolCallParams, renderBashToolPreview)
}

func renderBashToolPreview(name string, params map[string]any) string {
	command := stringParam(params, "command")
	timeout := intParam(params, "timeout")
	if command == "" {
		return renderGenericPreview(name, params)
	}
	commandPreview := truncateForPreview(strings.TrimSpace(command), 72)
	if timeout > 0 {
		return fmt.Sprintf("%s %s (timeout=%ds)", name, commandPreview, timeout)
	}
	return fmt.Sprintf("%s %s", name, commandPreview)
}

// NewBashTool creates a bash tool with optional path registry for extra PATH directories.
func NewBashTool(paths *PathRegistry) libagent.Tool {
	handler := func(ctx context.Context, params bashParams, call libagent.ToolCall) (libagent.ToolResponse, error) {
		if strings.TrimSpace(params.Command) == "" {
			return libagent.NewTextErrorResponse("command is required"), nil
		}

		commandCtx := ctx
		cancel := func() {}
		if params.Timeout > 0 {
			commandCtx, cancel = context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
		}
		defer cancel()

		environ := os.Environ()
		if paths != nil {
			if extra := paths.Paths(); len(extra) > 0 {
				environ = shellrun.PrependPath(environ, extra)
			}
		}

		commandPath, commandArgs := shellrun.UserShellCommand(params.Command)
		var buf bytes.Buffer
		err := shellrun.Run(commandCtx, shellrun.ExecSpec{
			Path: commandPath,
			Args: commandArgs,
			Env:  environ,
		}, &buf, &buf)

		fullOutput := buf.String()
		truncation := TruncateTail(fullOutput, TruncationOptions{})
		outputText := truncation.Content
		if outputText == "" {
			outputText = "(no output)"
		}

		var details *bashToolDetails
		if truncation.Truncated {
			tempPath := spillToTempFile(fullOutput)
			details = &bashToolDetails{Truncation: truncationPtr(truncation), FullOutputPath: tempPath}

			startLine := truncation.TotalLines - truncation.OutputLines + 1
			endLine := truncation.TotalLines

			switch {
			case truncation.LastLinePartial:
				lastLine := ""
				if idx := strings.LastIndex(fullOutput, "\n"); idx >= 0 {
					lastLine = fullOutput[idx+1:]
				} else {
					lastLine = fullOutput
				}
				outputText += fmt.Sprintf(
					"\n\n[Showing last %s of line %d (line is %s). Full output: %s]",
					FormatSize(truncation.OutputBytes),
					endLine,
					FormatSize(len(lastLine)),
					tempPath,
				)
			case truncation.TruncatedBy == "lines":
				outputText += fmt.Sprintf(
					"\n\n[Showing lines %d-%d of %d. Full output: %s]",
					startLine,
					endLine,
					truncation.TotalLines,
					tempPath,
				)
			default:
				outputText += fmt.Sprintf(
					"\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]",
					startLine,
					endLine,
					truncation.TotalLines,
					FormatSize(DefaultMaxBytes),
					tempPath,
				)
			}
		}

		if errors.Is(commandCtx.Err(), context.Canceled) {
			if outputText != "" {
				outputText += "\n\n"
			}
			outputText += "Command aborted"
			resp := libagent.NewTextErrorResponse(outputText)
			if details != nil {
				resp = libagent.WithResponseMetadata(resp, details)
			}
			return resp, nil
		}

		if params.Timeout > 0 && errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			if outputText != "" {
				outputText += "\n\n"
			}
			outputText += fmt.Sprintf("Command timed out after %d seconds", params.Timeout)
			resp := libagent.NewTextErrorResponse(outputText)
			if details != nil {
				resp = libagent.WithResponseMetadata(resp, details)
			}
			return resp, nil
		}

		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode := exitErr.ExitCode()
				outputText += fmt.Sprintf("\n\nCommand exited with code %d", exitCode)
				resp := libagent.NewTextErrorResponse(outputText)
				if details != nil {
					resp = libagent.WithResponseMetadata(resp, details)
				}
				return resp, nil
			}
			return libagent.NewTextErrorResponse(err.Error()), nil
		}

		resp := libagent.NewTextResponse(outputText)
		if details != nil {
			resp = libagent.WithResponseMetadata(resp, details)
		}
		return resp, nil
	}

	return WrapTool(
		libagent.NewParallelTypedTool("bash", fmt.Sprintf(
			"Execute non-interactive bash scripts in the current working directory. Returns stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.",
			DefaultMaxLines,
			DefaultMaxBytes/1024,
		), handler),
		RenderBashSingleLinePreview,
		nil,
	)
}

func truncationPtr(truncation TruncationResult) *TruncationResult {
	copy := truncation
	return &copy
}

// spillToTempFile writes content to a temp file and returns its path.
// Returns "" if the write fails.
func spillToTempFile(content string) string {
	f, err := os.CreateTemp("", "raijin-bash-*.log")
	if err != nil {
		return ""
	}
	defer f.Close()
	_, _ = f.WriteString(content)
	return f.Name()
}
