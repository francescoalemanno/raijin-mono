package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/skills"

	"github.com/francescoalemanno/raijin-mono/libagent"
)

const skillDescription = "Load/run a skill when the task at hand matches the skill description."

type skillParams struct {
	Name      string `json:"name" description:"Name of the skill to load"`
	Arguments string `json:"arguments,omitempty" description:"Optional task details to pass to the skill"`
}

// RegisterSkillScriptsPath adds a skill scripts directory to the shared PATH registry.
func RegisterSkillScriptsPath(paths *PathRegistry, scriptsDir string) {
	scriptsDir = strings.TrimSpace(scriptsDir)
	if scriptsDir == "" || paths == nil {
		return
	}
	paths.Add(scriptsDir)
}

func NewSkillTool(paths *PathRegistry) libagent.Tool {
	handler := func(ctx context.Context, params skillParams, call libagent.ToolCall) (libagent.ToolResponse, error) {
		if resp, blocked := toolExecutionGate(ctx, "skill"); blocked {
			return resp, nil
		}
		name := strings.ToLower(strings.TrimSpace(params.Name))
		if name == "" {
			return libagent.NewTextErrorResponse("skill name is required"), nil
		}

		arguments := strings.TrimSpace(params.Arguments)

		rendered, skill, err := skills.RenderSkill(name, arguments)
		if err != nil {
			if ctx.Err() != nil {
				return libagent.ToolResponse{}, ctx.Err()
			}
			return libagent.NewTextErrorResponse(fmt.Sprintf("skill error: %s", err)), nil
		}

		RegisterSkillScriptsPath(paths, skill.ScriptsDir)
		return libagent.NewTextResponse(rendered), nil
	}

	renderFunc := func(input json.RawMessage, _ string, _ int) string {
		var params skillParams
		if err := libagent.ParseJSONInput(input, &params); err != nil {
			return "skill (failed)"
		}
		if params.Arguments != "" {
			return fmt.Sprintf("skill: %s (%s)", params.Name, params.Arguments)
		}
		return fmt.Sprintf("skill: %s", params.Name)
	}

	return WithRender(
		libagent.NewTypedTool("skill", skillDescription, handler),
		renderFunc,
	)
}
