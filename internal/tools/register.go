package tools

import (
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
		NewSkillTool(paths),
		NewWebFetchTool(),
	}

	plugins := LoadPluginTools()
	return mergeToolsByPrecedence(builtin, plugins)
}
