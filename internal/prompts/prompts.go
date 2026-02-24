package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/core"
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
	Name         string
	Description  string
	Content      string
	Source       Source
	FilePath     string
	AllowedTools []string
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
		Name:         name,
		Description:  desc,
		Content:      body,
		Source:       source,
		FilePath:     filePath,
		AllowedTools: meta.AllowedTools,
	}
}

type templateMeta struct {
	Description  string
	AllowedTools []string
}

func parseTemplateMarkdown(content string) (templateMeta, string) {
	content = strings.TrimSpace(content)
	header, body, ok := frontmatter.Parse(content)
	if !ok {
		return templateMeta{}, content
	}

	meta := templateMeta{}
	meta.Description = frontmatter.StripOptionalQuotes(frontmatter.FirstValue(header, "description"))
	for _, value := range frontmatter.Values(header, "allowed-tools") {
		meta.AllowedTools = append(meta.AllowedTools, parseAllowedToolsValue(value)...)
	}

	meta.AllowedTools = core.Dedupe(meta.AllowedTools)
	return meta, strings.TrimSpace(body)
}

func parseAllowedToolsValue(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
		if value == "" {
			return nil
		}
	}
	var parts []string
	if strings.Contains(value, ",") {
		parts = strings.Split(value, ",")
	} else {
		parts = strings.Fields(value)
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if normalized := normalizeToolName(part); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func normalizeToolName(s string) string {
	s = frontmatter.StripOptionalQuotes(strings.TrimSpace(s))
	return core.Normalize(s)
}
