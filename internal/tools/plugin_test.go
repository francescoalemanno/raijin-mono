package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
)

func writePluginScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverPluginArtifacts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	dir := t.TempDir()

	// Valid plugin
	writePluginScript(t, dir, "hello.sh", `#!/bin/sh
if [ "$1" = "--info" ]; then
  echo '{"name":"hello","description":"Say hello","parameters":{"name":{"type":"string","description":"Name to greet"}},"required":["name"]}'
  exit 0
fi
INPUT=$(cat)
NAME=$(echo "$INPUT" | grep -o '"name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"name"[[:space:]]*:[[:space:]]*"//;s/"$//')
echo "Hello, ${NAME}!"
`)

	// Not executable (should be skipped)
	notExec := filepath.Join(dir, "notexec.sh")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\necho hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Invalid --info output (should be skipped)
	writePluginScript(t, dir, "bad.sh", `#!/bin/sh
if [ "$1" = "--info" ]; then
  echo 'not json'
  exit 0
fi
`)

	plugins, errs := discoverPluginArtifacts(dir)
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d (errs: %v)", len(plugins), errs)
	}

	tool := newPluginTool(plugins[0].meta, plugins[0].scriptPath)
	info := tool.Info()
	if info.Name != "hello" {
		t.Errorf("expected name 'hello', got %q", info.Name)
	}
	if info.Description != "Say hello" {
		t.Errorf("expected description 'Say hello', got %q", info.Description)
	}
	if len(info.Required) != 1 || info.Required[0] != "name" {
		t.Errorf("unexpected required: %v", info.Required)
	}

	// Test Run
	resp, err := tool.Run(context.Background(), llm.ToolCall{
		Input: `{"name":"World"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if resp.Content != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %q", resp.Content)
	}
}

func TestDiscoverPluginArtifactsEmptyDir(t *testing.T) {
	plugins, errs := discoverPluginArtifacts(t.TempDir())
	if len(plugins) != 0 {
		t.Fatalf("expected 0 plugins, got %d", len(plugins))
	}
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestDiscoverPluginArtifactsNonexistentDir(t *testing.T) {
	plugins, errs := discoverPluginArtifacts("/nonexistent/path/to/plugins")
	if len(plugins) != 0 {
		t.Fatalf("expected 0 plugins, got %v", plugins)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no errors for nonexistent dir, got %v", errs)
	}
}

func withCwd(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
}

func pluginScript(toolName, description, output string) string {
	return "#!/bin/sh\n" +
		"if [ \"$1\" = \"--info\" ]; then\n" +
		"  echo '{\"name\":\"" + toolName + "\",\"description\":\"" + description + "\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo '" + output + "'\n"
}

func TestRegisterDefaultToolsPrecedence_ProjectUserBuiltin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withCwd(t, project)

	globalDir := paths.UserPluginsDir()
	localDir := filepath.Join(project, localPluginsDir)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writePluginScript(t, globalDir, "read-user.sh", pluginScript("read", "User read", "user"))
	writePluginScript(t, localDir, "read-project.sh", pluginScript("read", "Project read", "project"))

	if err := artifacts.Reload(); err != nil {
		t.Fatalf("artifacts.Reload() error: %v", err)
	}

	registered := RegisterDefaultTools(NewPathRegistry())
	readCount := 0
	for _, tool := range registered {
		if tool.Info().Name == "read" {
			readCount++
		}
	}
	if readCount != 1 {
		t.Fatalf("expected exactly one read tool, got %d", readCount)
	}

	readTool := FindTool(registered, "read")
	if readTool == nil {
		t.Fatalf("expected read tool")
	}
	resp, err := readTool.Run(context.Background(), llm.ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsError {
		t.Fatalf("expected project plugin read to override builtin, got error: %s", resp.Content)
	}
	if got := strings.TrimSpace(resp.Content); got != "project" {
		t.Fatalf("read output = %q, want %q", got, "project")
	}
}

func TestLoadPluginInfosPrecedence_ProjectOverUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	withCwd(t, project)

	globalDir := paths.UserPluginsDir()
	localDir := filepath.Join(project, localPluginsDir)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writePluginScript(t, globalDir, "dup-user.sh", pluginScript("dup", "from-user", "user"))
	writePluginScript(t, localDir, "dup-project.sh", pluginScript("dup", "from-project", "project"))

	if err := artifacts.Reload(); err != nil {
		t.Fatalf("artifacts.Reload() error: %v", err)
	}

	infos := LoadPluginInfos()
	var filtered []PluginInfo
	for _, info := range infos {
		if info.Name == "dup" {
			filtered = append(filtered, info)
		}
	}
	if len(filtered) != 1 {
		t.Fatalf("expected single dup info, got %d (%#v)", len(filtered), filtered)
	}
	if filtered[0].Description != "from-project" {
		t.Fatalf("description = %q, want %q", filtered[0].Description, "from-project")
	}
}

func TestPluginRenderIncludesParameterPreview(t *testing.T) {
	t.Parallel()

	tool := newPluginTool(pluginMeta{
		Name:        "hello",
		Description: "Say hello",
	}, "/tmp/hello-plugin")

	rendered := RenderTool(tool, json.RawMessage(`{"name":"World","times":2}`), "", 0)
	if !strings.HasPrefix(rendered, "plugin:hello ") {
		t.Fatalf("expected plugin header, got %q", rendered)
	}
	if !strings.Contains(rendered, `"name":"World"`) {
		t.Fatalf("expected name parameter preview, got %q", rendered)
	}
	if !strings.Contains(rendered, `"times":2`) {
		t.Fatalf("expected times parameter preview, got %q", rendered)
	}
}

func TestPluginRenderTruncatesLongParameterPreview(t *testing.T) {
	t.Parallel()

	tool := newPluginTool(pluginMeta{
		Name:        "long",
		Description: "Long input",
	}, "/tmp/long-plugin")

	raw := json.RawMessage(`{"payload":"` + strings.Repeat("x", 200) + `"}`)
	rendered := RenderTool(tool, raw, "", 0)

	prefix := "plugin:long "
	if !strings.HasPrefix(rendered, prefix) {
		t.Fatalf("expected plugin header, got %q", rendered)
	}

	preview := strings.TrimPrefix(rendered, prefix)
	if utf8.RuneCountInString(preview) != 96 {
		t.Fatalf("preview length = %d, want 96", utf8.RuneCountInString(preview))
	}
	if !strings.HasSuffix(preview, "…") {
		t.Fatalf("expected truncated preview suffix, got %q", preview)
	}
}
