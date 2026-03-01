package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/frontmatter"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
)

//go:embed templates/*.md
var templatesFS embed.FS

type Source string

const (
	SourceEmbedded Source = "embedded"
	SourceUser     Source = "user"
	SourceProject  Source = "project"
)

const (
	projectPromptsDirRel = paths.ProjectPromptsDirRel
)

type Template struct {
	Name        string
	Description string
	Content     string
	Source      Source
	FilePath    string
}

type Diagnostic struct {
	Name       string
	WinnerPath string
	LoserPath  string
	Message    string
}

type LoadResult struct {
	Templates   []Template
	Diagnostics []Diagnostic
}

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

	addWithDiagnostics(loadEmbeddedTemplates())
	addWithDiagnostics(loadUserTemplates())
	addWithDiagnostics(loadProjectTemplates())

	result.Templates = make([]Template, 0, len(chosen))
	for _, tmpl := range chosen {
		result.Templates = append(result.Templates, tmpl)
	}
	sort.Slice(result.Templates, func(i, j int) bool {
		return result.Templates[i].Name < result.Templates[j].Name
	})
	return result
}

func loadEmbeddedTemplates() []Template {
	entries, err := fs.ReadDir(templatesFS, "templates")
	if err != nil {
		return nil
	}

	var templates []Template
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		filePath := filepath.Join("embedded://templates", entry.Name())
		raw, err := fs.ReadFile(templatesFS, filepath.Join("templates", entry.Name()))
		if err != nil {
			continue
		}
		tmpl := parseTemplateFile(entry.Name(), filePath, string(raw), SourceEmbedded)
		if tmpl.Name == "" {
			continue
		}
		templates = append(templates, tmpl)
	}
	return templates
}

func loadUserTemplates() []Template {
	dir := paths.UserPromptsDir()
	if dir == "" {
		return nil
	}
	return loadTemplatesFromDir(dir, SourceUser)
}

func loadProjectTemplates() []Template {
	return loadTemplatesFromDir(filepath.Join(".", projectPromptsDirRel), SourceProject)
}

func loadTemplatesFromDir(root string, source Source) []Template {
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


