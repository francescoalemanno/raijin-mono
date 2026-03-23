package tools

import (
	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/libagent"
)

// RuntimeAccessor exposes the current runtime model and registered tools.
type RuntimeAccessor interface {
	Model() libagent.RuntimeModel
	Tools() []libagent.Tool
}

// RegisterDefaultTools registers all default tools.
func RegisterDefaultTools(paths *PathRegistry, runtime RuntimeAccessor) []libagent.Tool {
	builtin := []libagent.Tool{
		NewReadTool(),
		NewGlobTool(),
		NewGrepTool(),
		NewEditTool(),
		NewWriteTool(),
		NewBashTool(paths),
		NewSubagentTool(runtime),
	}

	plugins := LoadPluginTools()
	return artifacts.Merge(func(t libagent.Tool) string { return t.Info().Name }, builtin, plugins)
}
