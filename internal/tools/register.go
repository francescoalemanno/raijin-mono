package tools

import (
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

// RegisterDefaultTools registers all default tools.
func RegisterDefaultTools(paths *PathRegistry) []llm.Tool {
	builtin := []llm.Tool{
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
