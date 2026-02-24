package substitution

import "github.com/francescoalemanno/raijin-mono/internal/paths"

// DefaultNamedValues returns the standard placeholder values shared by skills and templates.
func DefaultNamedValues(arguments string) map[string]string {
	return map[string]string{
		"ARGUMENTS":           arguments,
		"PROJECT_AGENTS_DIR":  paths.ProjectAgentsDirRel,
		"PROJECT_SKILLS_DIR":  paths.ProjectSkillsDirRel,
		"PROJECT_PROMPTS_DIR": paths.ProjectPromptsDirRel,
		"PROJECT_PLUGINS_DIR": paths.ProjectPluginsDirRel,
		"USER_SKILLS_DIR":     paths.UserSkillsDir(),
		"USER_PROMPTS_DIR":    paths.UserPromptsDir(),
		"USER_PLUGINS_DIR":    paths.UserPluginsDir(),
		"SKILL_FILE":          paths.SkillFileName,
		"SCRIPTS_DIR":         paths.ScriptsDirName,
	}
}

func BracesStyle() NamedStyle {
	return NamedStyle{
		Open:   "{{",
		Close:  "}}",
		Escape: `\`,
	}
}
