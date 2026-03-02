package substitution

import (
	"github.com/francescoalemanno/raijin-mono/internal/embedded"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
)

// DefaultNamedValues returns the standard placeholder values shared by skills and templates.
func DefaultNamedValues(_ string) map[string]string {
	return map[string]string{
		"PROJECT_AGENTS_DIR":   paths.ProjectAgentsDirRel,
		"PROJECT_SKILLS_DIR":   paths.ProjectSkillsDirRel,
		"PROJECT_PROMPTS_DIR":  paths.ProjectPromptsDirRel,
		"PROJECT_PLUGINS_DIR":  paths.ProjectPluginsDirRel,
		"USER_SKILLS_DIR":      paths.UserSkillsDir(),
		"USER_PROMPTS_DIR":     paths.UserPromptsDir(),
		"USER_PLUGINS_DIR":     paths.UserPluginsDir(),
		"EMBEDDED_SKILLS_DIR":  embedded.Scheme + "skills",
		"EMBEDDED_PROMPTS_DIR": embedded.Scheme + "templates",
		"SKILL_FILE":           paths.SkillFileName,
	}
}

func BracesStyle() NamedStyle {
	return NamedStyle{
		Open:   "{{",
		Close:  "}}",
		Escape: `\`,
	}
}
