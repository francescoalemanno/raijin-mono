package paths

import (
	"os"
	"path/filepath"
)

// UserConfigPath returns $HOME/.config joined with the given path elements
// (e.g. "raijin", "plugins"). Returns "" on error.
func UserConfigPath(elem ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{home, ".config"}, elem...)...)
}

// RaijinPath returns the full path for a file or directory within the raijin
// user config directory. Returns "" on error.
func RaijinPath(elem ...string) string {
	return UserConfigPath(append([]string{"raijin"}, elem...)...)
}

// RaijinConfigPath returns the path to the main raijin config file.
func RaijinConfigPath() string {
	return RaijinPath("config.toml")
}

// RaijinModelsPath returns the path to the raijin models config file.
func RaijinModelsPath() string {
	return RaijinPath("models.toml")
}

// RaijinSessionsDir returns the path to the raijin sessions directory.
func RaijinSessionsDir() string {
	return RaijinPath("sessions")
}

// RaijinBindingsDir returns the path to the raijin bindings directory.
func RaijinBindingsDir() string {
	return RaijinPath("bindings")
}

// RaijinAuthPath returns the path to the raijin auth file.
func RaijinAuthPath() string {
	return RaijinPath("auth.json")
}

// UserSkillsDir returns the path to the user skills directory.
func UserSkillsDir() string {
	return RaijinPath("agents", "skills")
}

// UserPromptsDir returns the path to the user prompts directory.
func UserPromptsDir() string {
	return RaijinPath("agents", "prompts")
}

// UserSubagentsDir returns the path to the user subagents directory.
func UserSubagentsDir() string {
	return RaijinPath("agents", "subagents")
}

// UserToolsDir returns the path to the user tools directory.
func UserToolsDir() string {
	return RaijinPath("tools")
}

// Relative path constants for use with filepath.Join or RaijinPath.
const (
	// Project-relative paths
	ProjectAgentsDirRel    = "./.agents"
	ProjectSkillsDirRel    = "./.agents/skills"
	ProjectPromptsDirRel   = "./.agents/prompts"
	ProjectSubagentsDirRel = "./.agents/subagents"
	ProjectToolsDirRel     = "./.agents/tools"

	// User config subpaths (relative to raijin/)
	UserSkillsDirRel    = "agents/skills"
	UserPromptsDirRel   = "agents/prompts"
	UserSubagentsDirRel = "agents/subagents"
	UserToolsDirRel     = "tools"

	// File names
	SkillFileName  = "SKILL.md"
	ScriptsDirName = "scripts"
)
