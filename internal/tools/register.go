package tools

import (
	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/libagent"
)

// RegisterDefaultTools registers all default tools.
func RegisterDefaultTools(paths *PathRegistry) []libagent.Tool {
	builtin := []libagent.Tool{
		NewReadTool(),
		NewGlobTool(),
		NewGrepTool(),
		NewEditTool(),
		NewWriteTool(),
		NewBashTool(paths),
		NewWebFetchTool(),
	}

	plugins := LoadPluginTools()
	return artifacts.Merge(func(t libagent.Tool) string { return t.Info().Name }, builtin, plugins)
}
