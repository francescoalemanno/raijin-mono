package substitution

import (
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/internal/vfs"
)

// DefaultNamedValues returns the standard placeholder values shared by skills and templates.
func DefaultNamedValues(_ string) map[string]string {
	return map[string]string{
		"PROJECT_AGENTS_DIR":   paths.ProjectAgentsDirRel,
		"PROJECT_SKILLS_DIR":   paths.ProjectSkillsDirRel,
		"PROJECT_PROMPTS_DIR":  paths.ProjectPromptsDirRel,
		"PROJECT_TOOLS_DIR":    paths.ProjectToolsDirRel,
		"USER_SKILLS_DIR":      paths.UserSkillsDir(),
		"USER_PROMPTS_DIR":     paths.UserPromptsDir(),
		"USER_TOOLS_DIR":       paths.UserToolsDir(),
		"EMBEDDED_SKILLS_DIR":  vfs.Scheme + "skills",
		"EMBEDDED_PROMPTS_DIR": vfs.Scheme + "templates",
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
