package skills

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/frontmatter"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/internal/substitution"
)

//go:embed skills/*.md
var skillsFS embed.FS

// Skill represents a loadable skill definition.
// Exported for use by the skill tool and agent prompt injection.
type Skill struct {
	Name        string
	Description string
	Source      Source
	// LLMDescription overrides Description only for model-facing system prompt injection.
	LLMDescription string
	// HideFromLLM controls whether the skill is omitted from the system prompt skill list.
	HideFromLLM bool
	Content     string // markdown body without frontmatter
	ScriptsDir  string // absolute path to scripts/ dir, empty if none
}

type Source string

const (
	SourceEmbedded Source = "embedded"
	SourceUser     Source = "user"
	SourceProject  Source = "project"
)

const (
	projectAgentsDirRel   = paths.ProjectAgentsDirRel
	projectSkillsDirRel   = paths.ProjectSkillsDirRel
	projectScriptsDirName = paths.ScriptsDirName
	externalSkillFile     = paths.SkillFileName
)

func init() {
	artifacts.RegisterLoader(artifacts.KindSkill, loadSkillArtifacts)
}

func loadSkillArtifacts() ([]artifacts.Item, error) {
	merged := mergeAllSkills()
	items := make([]artifacts.Item, 0, len(merged))
	for _, skill := range merged {
		items = append(items, artifacts.Item{
			Kind:  artifacts.KindSkill,
			Name:  skill.Name,
			Value: skill,
		})
	}
	return items, nil
}

func mergeAllSkills() []Skill {
	m := make(map[string]Skill)

	for _, s := range loadEmbeddedSkills() {
		m[s.Name] = s
	}
	for _, s := range loadExternalSkillsMerged() {
		m[s.Name] = s
	}

	out := make([]Skill, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// GetSkills returns all available skills.
// Used by agent to inject into system prompt.
func GetSkills() []Skill {
	return artifacts.GetAllTyped[Skill](artifacts.KindSkill)
}

// GetExternalSkills returns only external (user/project) skills.
// Project skills override user skills on name collisions.
func GetExternalSkills() []Skill {
	all := GetSkills()
	out := make([]Skill, 0, len(all))
	for _, skill := range all {
		if skill.Source == SourceEmbedded {
			continue
		}
		out = append(out, skill)
	}
	return out
}

// GetSkill returns a skill by name.
func GetSkill(name string) (Skill, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Skill{}, false
	}
	for _, skill := range GetSkills() {
		if skill.Name == name {
			return skill, true
		}
	}
	return Skill{}, false
}

// GetProjectScriptsPaths returns the absolute path to .agents/scripts if it
// exists as a directory. This is a project-level scripts directory whose
// contents should be available on PATH for bash tool invocations.
func GetProjectScriptsPaths() []string {
	dir := filepath.Join(".", projectAgentsDirRel, projectScriptsDirName)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	return []string{abs}
}

// RenderSkill renders a skill by name with optional arguments.
func RenderSkill(name string, arguments string) (string, Skill, error) {
	renderedBody, skill, err := RenderSkillAttachment(name, arguments)
	if err != nil {
		return "", Skill{}, err
	}

	skillOpen := fmt.Sprintf("<skill name=%q>\n\n", skill.Name)
	skillClose := "\n\n</skill>"

	var sb strings.Builder
	sb.WriteString(llmInvokeHeader)
	sb.WriteString(skillOpen)
	sb.WriteString(renderedBody)
	sb.WriteString(skillClose)
	sb.WriteString(llmInvokeFooter)

	return sb.String(), skill, nil
}

// RenderSkillAttachment renders only the skill body for user attachment injection.
// It does not add LLM invocation wrappers.
func RenderSkillAttachment(name string, arguments string) (string, Skill, error) {
	skill, ok := GetSkill(name)
	if !ok {
		return "", Skill{}, fmt.Errorf("unknown skill: %q", name)
	}

	renderedBody := substitution.ExpandAll(context.Background(), strings.TrimSpace(skill.Content), arguments, substitution.ArgModeText)
	return renderedBody, skill, nil
}

func loadEmbeddedSkills() []Skill {
	entries, err := fs.ReadDir(skillsFS, "skills")
	if err != nil {
		return nil
	}

	var result []Skill
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		name = strings.ToLower(name)
		if name == "" {
			continue
		}
		data, err := fs.ReadFile(skillsFS, filepath.Join("skills", entry.Name()))
		if err != nil {
			continue
		}
		rawContent := strings.TrimSpace(string(data))
		description, llmDescription, hideFromLLM, body := parseSkillMarkdown(rawContent)

		result = append(result, Skill{
			Name:           name,
			Description:    description,
			Source:         SourceEmbedded,
			LLMDescription: llmDescription,
			HideFromLLM:    hideFromLLM,
			Content:        body,
		})
	}
	return result
}

func loadExternalSkillsFromDir(root string, source Source) []Skill {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var result []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := strings.ToLower(entry.Name())
		path := filepath.Join(root, entry.Name(), externalSkillFile)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		rawContent := strings.TrimSpace(string(data))
		description, llmDescription, hideFromLLM, body := parseSkillMarkdown(rawContent)

		skill := Skill{
			Name:           skillName,
			Description:    description,
			Source:         source,
			LLMDescription: llmDescription,
			HideFromLLM:    hideFromLLM,
			Content:        body,
		}

		scriptsPath := filepath.Join(root, entry.Name(), "scripts")
		if info, err := os.Stat(scriptsPath); err == nil && info.IsDir() {
			if abs, err := filepath.Abs(scriptsPath); err == nil {
				skill.ScriptsDir = abs
			}
		}

		result = append(result, skill)
	}
	return result
}

func loadProjectSkills() []Skill {
	return loadExternalSkillsFromDir(filepath.Join(".", projectSkillsDirRel), SourceProject)
}

func loadUserSkills() []Skill {
	dir := paths.UserSkillsDir()
	if dir == "" {
		return nil
	}
	return loadExternalSkillsFromDir(dir, SourceUser)
}

func loadExternalSkillsMerged() []Skill {
	m := make(map[string]Skill)
	for _, s := range loadUserSkills() {
		m[s.Name] = s
	}
	for _, s := range loadProjectSkills() {
		m[s.Name] = s
	}

	out := make([]Skill, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func parseSkillMarkdown(content string) (string, string, bool, string) {
	header, body, ok := frontmatter.Parse(content)
	if !ok {
		description := frontmatter.FirstNonEmptyLine(content)
		return description, "", false, content
	}

	description := frontmatter.StripOptionalQuotes(frontmatter.FirstValue(header, "description"))
	llmDescription := frontmatter.StripOptionalQuotes(frontmatter.FirstValueFrom(
		header,
		"llmdescription",
		"llm_description",
		"llm-description",
	))
	hideFromLLM := parseBoolFrontmatter(
		header,
		"hide-from-llm",
		"hide_from_llm",
		"hidefromllm",
	)
	if description == "" {
		description = frontmatter.FirstNonEmptyLine(body)
	}
	return description, llmDescription, hideFromLLM, body
}

// PromptDescription returns the description that should be exposed in model-facing prompts.
func (s Skill) PromptDescription() string {
	if s.LLMDescription != "" {
		return s.LLMDescription
	}
	return s.Description
}

// ShouldAdvertiseToLLM reports whether this skill should be listed in the system prompt.
func (s Skill) ShouldAdvertiseToLLM() bool {
	return !s.HideFromLLM
}

func parseBoolFrontmatter(header frontmatter.Header, keys ...string) bool {
	raw := frontmatter.StripOptionalQuotes(frontmatter.FirstValueFrom(header, keys...))
	if raw == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return false
	}
}

const llmInvokeHeader = `<skill_instructions>
You loaded this skill to help complete the current task. Follow its instructions.
</skill_instructions>

`

const llmInvokeFooter = `

<skill_instructions>
Apply the skill instructions above to complete the task at hand.
</skill_instructions>`
