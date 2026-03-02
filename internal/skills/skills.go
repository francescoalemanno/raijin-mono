package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/embedded"
	"github.com/francescoalemanno/raijin-mono/internal/frontmatter"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
)

// Skill represents a loadable skill definition.
type Skill struct {
	Name        string
	Description string
	Source      Source
	// LLMDescription overrides Description only for model-facing system prompt injection.
	LLMDescription string
	FilePath       string // path to the skill file (embedded://... for embedded, file path for external)
}

// Source identifies where a skill originated from.
type Source string

const (
	SourceEmbedded Source = "embedded"
	SourceUser     Source = "user"
	SourceProject  Source = "project"
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
func GetSkills() []Skill {
	return artifacts.GetAllTyped[Skill](artifacts.KindSkill)
}

// GetExternalSkills returns only external (user/project) skills.
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

func loadEmbeddedSkills() []Skill {
	files, err := embedded.ListFiles("skills", ".md")
	if err != nil {
		return nil
	}

	var result []Skill
	for _, file := range files {
		name := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
		name = strings.ToLower(name)
		if name == "" {
			continue
		}
		data, err := embedded.ReadFile("skills/" + file.Name())
		if err != nil {
			continue
		}
		rawContent := strings.TrimSpace(string(data))
		description, llmDescription := parseSkillHeader(rawContent)

		result = append(result, Skill{
			Name:           name,
			Description:    description,
			Source:         SourceEmbedded,
			LLMDescription: llmDescription,
			FilePath:       embedded.Scheme + "skills/" + file.Name(),
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
		path := filepath.Join(root, entry.Name(), paths.SkillFileName)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		rawContent := strings.TrimSpace(string(data))
		description, llmDescription := parseSkillHeader(rawContent)

		skill := Skill{
			Name:           skillName,
			Description:    description,
			Source:         source,
			LLMDescription: llmDescription,
			FilePath:       path,
		}

		result = append(result, skill)
	}
	return result
}

func loadProjectSkills() []Skill {
	return loadExternalSkillsFromDir(filepath.Join(".", paths.ProjectSkillsDirRel), SourceProject)
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

func parseSkillHeader(content string) (string, string) {
	header, body, ok := frontmatter.Parse(content)
	if !ok {
		description := frontmatter.FirstNonEmptyLine(content)
		return description, ""
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
