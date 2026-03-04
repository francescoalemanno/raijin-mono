package skills

import (
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/frontmatter"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/internal/vfs"
)

// Skill represents a loadable skill definition.
type Skill struct {
	Name        string
	Description string
	Source      artifacts.Source
	// LLMDescription overrides Description only for model-facing system prompt injection.
	LLMDescription string
	FilePath       string // path to the skill file (embedded://... for embedded, file path for external)
}

func init() {
	artifacts.RegisterLoader(artifacts.KindSkill, loadSkillArtifacts)
}

func loadSkillArtifacts() ([]artifacts.Item, error) {
	merged := artifacts.Merge(
		func(s Skill) string { return s.Name },
		loadSkillsFromPath("embedded://skills", artifacts.SourceEmbedded),
		loadSkillsFromPath(paths.UserSkillsDir(), artifacts.SourceUser),
		loadSkillsFromPath(filepath.Join(".", paths.ProjectSkillsDirRel), artifacts.SourceProject),
	)
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

// GetSkills returns all available skills.
func GetSkills() []Skill {
	return artifacts.GetAllTyped[Skill](artifacts.KindSkill)
}

// GetExternalSkills returns only external (user/project) skills.
func GetExternalSkills() []Skill {
	all := GetSkills()
	out := make([]Skill, 0, len(all))
	for _, skill := range all {
		if skill.Source == artifacts.SourceEmbedded {
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

func loadSkillsFromPath(root string, source artifacts.Source) []Skill {
	if root == "" {
		return nil
	}

	v := vfs.NewFromWD()
	entries, err := v.ReadDir(root)
	if err != nil {
		return nil
	}

	var result []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := strings.ToLower(entry.Name())
		if skillName == "" {
			continue
		}
		filePath := vfs.Join(root, entry.Name(), paths.SkillFileName)
		data, err := v.ReadFile(filePath)
		if err != nil {
			continue
		}
		description, llmDescription := parseSkillHeader(strings.TrimSpace(string(data)))
		result = append(result, Skill{
			Name:           skillName,
			Description:    description,
			Source:         source,
			LLMDescription: llmDescription,
			FilePath:       filePath,
		})
	}
	return result
}

func parseSkillHeader(content string) (string, string) {
	header, body, ok := frontmatter.Parse(content)
	if !ok {
		return frontmatter.FirstNonEmptyLine(content), ""
	}

	description := frontmatter.StripOptionalQuotes(frontmatter.FirstValue(header, "description"))
	llmDescription := frontmatter.StripOptionalQuotes(frontmatter.FirstValueFrom(
		header,
		"llmdescription",
		"llm_description",
		"llm-description",
	))
	if description == "" {
		description = frontmatter.FirstNonEmptyLine(body)
	}
	return description, llmDescription
}

// PromptDescription returns the description that should be exposed in model-facing prompts.
func (s Skill) PromptDescription() string {
	if s.LLMDescription != "" {
		return s.LLMDescription
	}
	return s.Description
}
