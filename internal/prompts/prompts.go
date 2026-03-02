package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/embedded"
	"github.com/francescoalemanno/raijin-mono/internal/frontmatter"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
)

// Source identifies where a template originated from.
type Source string

const (
	SourceEmbedded Source = "embedded"
	SourceUser     Source = "user"
	SourceProject  Source = "project"
)

// Template represents a prompt template.
type Template struct {
	Name        string
	Description string
	Content     string
	Source      Source
	FilePath    string
}

// Diagnostic records a collision between templates.
type Diagnostic struct {
	Name       string
	WinnerPath string
	LoserPath  string
	Message    string
}

// LoadResult holds the result of loading templates.
type LoadResult struct {
	Templates   []Template
	Diagnostics []Diagnostic
}

// Find returns a template by name (case-insensitive).
func (r LoadResult) Find(name string) (Template, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, t := range r.Templates {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

// Load discovers prompt templates from embedded, user, and project directories
// with precedence project > user > embedded.
func Load() LoadResult {
	result := LoadResult{}
	chosen := make(map[string]Template)

	addWithDiagnostics := func(templates []Template) {
		for _, tmpl := range templates {
			existing, exists := chosen[tmpl.Name]
			if exists {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					Name:       tmpl.Name,
					WinnerPath: tmpl.FilePath,
					LoserPath:  existing.FilePath,
					Message:    fmt.Sprintf("template /%s from %s overrides %s", tmpl.Name, tmpl.FilePath, existing.FilePath),
				})
			}
			chosen[tmpl.Name] = tmpl
		}
	}

	addWithDiagnostics(loadEmbedded())
	addWithDiagnostics(loadFromDir(paths.UserPromptsDir(), SourceUser))
	addWithDiagnostics(loadFromDir(filepath.Join(".", paths.ProjectPromptsDirRel), SourceProject))

	result.Templates = make([]Template, 0, len(chosen))
	for _, tmpl := range chosen {
		result.Templates = append(result.Templates, tmpl)
	}
	sort.Slice(result.Templates, func(i, j int) bool {
		return result.Templates[i].Name < result.Templates[j].Name
	})
	return result
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
		tmpl := parseTemplateFile(file.Name(), embedded.Scheme+filePath, string(raw), SourceEmbedded)
		if tmpl.Name == "" {
			continue
		}
		templates = append(templates, tmpl)
	}
	return templates
}

func loadFromDir(root string, source Source) []Template {
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

func parseTemplateFile(fileName, filePath, raw string, source Source) Template {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fileName)), filepath.Ext(fileName))
	if name == "" {
		return Template{}
	}
	meta, body := parseTemplateMarkdown(raw)
	desc := strings.TrimSpace(meta.Description)
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

type templateMeta struct {
	Description string
}

func parseTemplateMarkdown(content string) (templateMeta, string) {
	content = strings.TrimSpace(content)
	header, body, ok := frontmatter.Parse(content)
	if !ok {
		return templateMeta{}, content
	}

	meta := templateMeta{}
	meta.Description = frontmatter.StripOptionalQuotes(frontmatter.FirstValue(header, "description"))
	return meta, strings.TrimSpace(body)
}
