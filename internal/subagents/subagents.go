package subagents

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/frontmatter"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/internal/vfs"
)

// Subagent represents a loadable subagent profile.
type Subagent struct {
	Name        string
	Description string
	Prompt      string
	Tools       []string
	Source      artifacts.Source
	FilePath    string
}

func init() {
	artifacts.RegisterLoader(artifacts.KindSubagent, loadSubagentArtifacts)
}

func loadSubagentArtifacts() ([]artifacts.Item, error) {
	merged := artifacts.Merge(
		func(s Subagent) string { return s.Name },
		loadSubagentsFromPath("embedded://subagents", artifacts.SourceEmbedded),
		loadSubagentsFromPath(paths.UserSubagentsDir(), artifacts.SourceUser),
		loadSubagentsFromPath(filepath.Join(".", paths.ProjectSubagentsDirRel), artifacts.SourceProject),
	)
	items := make([]artifacts.Item, 0, len(merged))
	for _, subagent := range merged {
		items = append(items, artifacts.Item{
			Kind:  artifacts.KindSubagent,
			Name:  subagent.Name,
			Value: subagent,
		})
	}
	return items, nil
}

// GetSubagents returns all available subagent profiles.
func GetSubagents() []Subagent {
	return artifacts.GetAllTyped[Subagent](artifacts.KindSubagent)
}

// Find returns a subagent profile by name.
func Find(name string) (Subagent, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Subagent{}, false
	}
	for _, subagent := range GetSubagents() {
		if subagent.Name == name {
			return subagent, true
		}
	}
	return Subagent{}, false
}

func loadSubagentsFromPath(root string, source artifacts.Source) []Subagent {
	if root == "" {
		return nil
	}

	v := vfs.NewFromWD()
	entries, err := v.ReadDir(root)
	if err != nil {
		return nil
	}

	var subagents []Subagent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		filePath := vfs.Join(root, entry.Name())
		raw, err := v.ReadFile(filePath)
		if err != nil {
			continue
		}
		subagent := parseSubagentFile(entry.Name(), filePath, string(raw), source)
		if subagent.Name == "" {
			continue
		}
		subagents = append(subagents, subagent)
	}
	return subagents
}

func parseSubagentFile(fileName, filePath, raw string, source artifacts.Source) Subagent {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fileName)), filepath.Ext(fileName))
	if name == "" {
		return Subagent{}
	}

	header, body, ok := frontmatter.Parse(strings.TrimSpace(raw))
	if ok {
		body = strings.TrimSpace(body)
	} else {
		body = strings.TrimSpace(raw)
	}
	if body == "" {
		return Subagent{}
	}

	desc := frontmatter.StripOptionalQuotes(frontmatter.FirstValue(header, "description"))
	if desc == "" {
		desc = frontmatter.FirstNonEmptyLine(body)
	}

	tools := normalizeTools(frontmatter.Values(header, "tools"))
	return Subagent{
		Name:        name,
		Description: desc,
		Prompt:      body,
		Tools:       tools,
		Source:      source,
		FilePath:    filePath,
	}
}

func normalizeTools(values []string) []string {
	var tools []string
	for _, value := range values {
		name := strings.ToLower(strings.TrimSpace(value))
		if name == "" || slices.Contains(tools, name) {
			continue
		}
		tools = append(tools, name)
	}
	return tools
}
