package vetting_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/francescoalemanno/raijin-mono"

// topLevelDir returns the top-level directory of a repo-relative file path
// (e.g. "libagent" for "libagent/catalog.go").
func topLevelDir(repoRelPath string) string {
	return strings.SplitN(repoRelPath, string(os.PathSeparator), 2)[0]
}

// isLocalImport checks if an import path belongs to this module and returns
// the repo-relative import suffix (e.g. "internal/paths").
func isLocalImport(imp string) (string, bool) {
	if strings.HasPrefix(imp, modulePath+"/") {
		return strings.TrimPrefix(imp, modulePath+"/"), true
	}
	return "", false
}

func isCharmImport(imp string) bool {
	return strings.HasPrefix(imp, "charm.land/") ||
		strings.Contains(imp, "charmbracelet")
}

type violation struct {
	file    string
	imp     string
	ruleNum int
	reason  string
}

// allowlist contains known existing violations that should not block CI.
// Each entry maps "repoRelFile -> importPath".
// TODO: fix these violations and remove from allowlist.
var allowlist = map[string]string{
	// Rule 2: llmbridge importing internal
	"llmbridge/pkg/config/load.go":               modulePath + "/internal/paths",
	"llmbridge/pkg/config/openai_codex_oauth.go": modulePath + "/internal/paths",
	// Rule 3: libtui importing internal
	"libtui/pkg/tui/tui.go": modulePath + "/internal/paths",
}

func isAllowlisted(repoRelFile, imp string) bool {
	if allowed, ok := allowlist[repoRelFile]; ok {
		return allowed == imp
	}
	return false
}

func TestImportPolicy(t *testing.T) {
	repoRoot := findRepoRoot(t)

	var violations []violation

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "vendor" || base == "pi-mono" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}

		dir := topLevelDir(relPath)

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			// Skip files that don't parse (e.g. generated code with build tags).
			return nil
		}

		for _, imp := range f.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)

			// Rule 1: only libagent may import charm libraries.
			if isCharmImport(impPath) && dir != "libagent" {
				violations = append(violations, violation{
					file:    relPath,
					imp:     impPath,
					ruleNum: 1,
					reason:  "only libagent/ may import charm.land/charmbracelet libraries",
				})
			}

			// Rule 2: llmbridge must not import internal, libtui, or cmd.
			if dir == "llmbridge" {
				if local, ok := isLocalImport(impPath); ok {
					target := topLevelDir(local)
					if target == "internal" || target == "libtui" || target == "cmd" {
						violations = append(violations, violation{
							file:    relPath,
							imp:     impPath,
							ruleNum: 2,
							reason:  "llmbridge/ must not import " + target + "/",
						})
					}
				}
			}

			// Rule 3: libtui must not import other repo packages.
			if dir == "libtui" {
				if local, ok := isLocalImport(impPath); ok {
					target := topLevelDir(local)
					if target != "libtui" {
						violations = append(violations, violation{
							file:    relPath,
							imp:     impPath,
							ruleNum: 3,
							reason:  "libtui/ must not import " + target + "/",
						})
					}
				}
			}

			// Rule 4: internal must not import cmd.
			if dir == "internal" {
				if local, ok := isLocalImport(impPath); ok {
					target := topLevelDir(local)
					if target == "cmd" {
						violations = append(violations, violation{
							file:    relPath,
							imp:     impPath,
							ruleNum: 4,
							reason:  "internal/ must not import cmd/",
						})
					}
				}
			}

			// Rule 5: no Go code may import pi-mono.
			if local, ok := isLocalImport(impPath); ok {
				if topLevelDir(local) == "pi-mono" {
					violations = append(violations, violation{
						file:    relPath,
						imp:     impPath,
						ruleNum: 5,
						reason:  "no Go code may import pi-mono/ (TypeScript project)",
					})
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}

	// Separate new violations from allowlisted ones.
	var newViolations []violation
	for _, v := range violations {
		if !isAllowlisted(v.file, v.imp) {
			newViolations = append(newViolations, v)
		}
	}

	// Report allowlisted violations as informational.
	for _, v := range violations {
		if isAllowlisted(v.file, v.imp) {
			t.Logf("ALLOWLISTED (rule %d): %s imports %q — %s", v.ruleNum, v.file, v.imp, v.reason)
		}
	}

	// Fail on new violations.
	for _, v := range newViolations {
		t.Errorf("NEW import violation (rule %d): %s imports %q — %s", v.ruleNum, v.file, v.imp, v.reason)
	}
}

// findRepoRoot walks up from the working directory to find the go.mod file.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}
