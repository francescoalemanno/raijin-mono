package prompts

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/embedded"
	"github.com/francescoalemanno/raijin-mono/internal/frontmatter"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
)

// Template represents a prompt template.
type Template struct {
	Name        string
	Description string
	Content     string
	Source      artifacts.Source
	FilePath    string
}

func init() {
	artifacts.RegisterLoader(artifacts.KindPrompt, loadPromptArtifacts)
}

func loadPromptArtifacts() ([]artifacts.Item, error) {
	merged := artifacts.Merge(
		func(t Template) string { return t.Name },
		loadEmbedded(),
		loadFromDir(paths.UserPromptsDir(), artifacts.SourceUser),
		loadFromDir(filepath.Join(".", paths.ProjectPromptsDirRel), artifacts.SourceProject),
	)
	items := make([]artifacts.Item, 0, len(merged))
	for _, tmpl := range merged {
		items = append(items, artifacts.Item{
			Kind:  artifacts.KindPrompt,
			Name:  tmpl.Name,
			Value: tmpl,
		})
	}
	return items, nil
}

// GetTemplates returns all available prompt templates.
func GetTemplates() []Template {
	return artifacts.GetAllTyped[Template](artifacts.KindPrompt)
}

// Find returns a template by name (case-insensitive).
func Find(name string) (Template, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, t := range GetTemplates() {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

func loadEmbedded() []Template {
	files, err := embedded.ListFiles("templates", ".md")
	if err != nil {
		return nil
	}

	var templates []Template
	for _, file := range files {
		filePath := "templates/" + file.Name()
		raw, err := embedded.ReadFile(filePath)
		if err != nil {
			continue
		}
		tmpl := parseTemplateFile(file.Name(), embedded.Scheme+filePath, string(raw), artifacts.SourceEmbedded)
		if tmpl.Name == "" {
			continue
		}
		templates = append(templates, tmpl)
	}
	return templates
}

func loadFromDir(root string, source artifacts.Source) []Template {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var templates []Template
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if stats, err := os.Stat(path); err != nil || !stats.Mode().IsRegular() {
				continue
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		tmpl := parseTemplateFile(entry.Name(), path, string(raw), source)
		if tmpl.Name == "" {
			continue
		}
		templates = append(templates, tmpl)
	}
	return templates
}

func parseTemplateFile(fileName, filePath, raw string, source artifacts.Source) Template {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fileName)), filepath.Ext(fileName))
	if name == "" {
		return Template{}
	}
	header, body, ok := frontmatter.Parse(strings.TrimSpace(raw))
	var desc string
	if ok {
		desc = frontmatter.StripOptionalQuotes(frontmatter.FirstValue(header, "description"))
		body = strings.TrimSpace(body)
	} else {
		body = strings.TrimSpace(raw)
	}
	if desc == "" {
		desc = frontmatter.FirstNonEmptyLine(body)
	}
	return Template{
		Name:        name,
		Description: desc,
		Content:     body,
		Source:      source,
		FilePath:    filePath,
	}
}
