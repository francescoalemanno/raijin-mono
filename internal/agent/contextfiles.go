package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
)

var agentsTargets = []string{"agents.md", "agent.md"}

// File is a discovered context file.
type File struct {
	Name    string
	Path    string
	Dir     string
	Content string
}

func init() {
	artifacts.RegisterLoader(artifacts.KindContext, loadContextArtifacts)
}

func loadContextArtifacts() ([]artifacts.Item, error) {
	file, ok := loadNearestAgentsFile()
	if !ok {
		return nil, nil
	}
	return []artifacts.Item{
		{
			Kind:  artifacts.KindContext,
			Name:  strings.ToLower(file.Name),
			Value: file,
		},
	}, nil
}

// GetAgentsFile returns the nearest AGENTS.md/agent.md discovered from cwd to home/root.
func GetAgentsFile() (File, bool) {
	for _, file := range artifacts.GetAllTyped[File](artifacts.KindContext) {
		lower := strings.ToLower(file.Name)
		if lower == "agents.md" || lower == "agent.md" {
			return file, true
		}
	}
	return File{}, false
}

// SameDir reports whether two paths refer to the same directory.
func SameDir(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

func loadNearestAgentsFile() (File, bool) {
	startDir, err := filepath.Abs(".")
	if err != nil {
		return File{}, false
	}
	homeDir, _ := os.UserHomeDir()

	dir := startDir
	for {
		if file, ok := findAgentsFileInDir(dir); ok {
			return file, true
		}

		if homeDir != "" && SameDir(dir, homeDir) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || SameDir(parent, dir) {
			break
		}
		dir = parent
	}
	return File{}, false
}

func findAgentsFileInDir(dir string) (File, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return File{}, false
	}

	matched := make(map[string]string, len(agentsTargets))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		lower := strings.ToLower(entry.Name())
		for _, target := range agentsTargets {
			if lower != target {
				continue
			}
			if _, exists := matched[target]; !exists {
				matched[target] = entry.Name()
			}
		}
	}

	for _, target := range agentsTargets {
		name, ok := matched[target]
		if !ok {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return File{
			Name:    name,
			Path:    path,
			Dir:     dir,
			Content: string(data),
		}, true
	}
	return File{}, false
}
