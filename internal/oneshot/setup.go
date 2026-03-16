package oneshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"golang.org/x/term"
)

type shellSetupResult struct {
	shell      string
	configPath string
	initLine   string
	added      bool
}

func handleSetup(opts Options, rawArgs string) error {
	shell, err := resolveSetupShell(rawArgs)
	if err != nil {
		return err
	}

	result, err := ensureShellIntegration(shell)
	if err != nil {
		return err
	}

	if result.added {
		fmt.Fprintf(stderrWriter, "%s Shell integration configured for %s in %s\n", renderStatusSuccess("✓"), result.shell, result.configPath)
	} else {
		fmt.Fprintf(stderrWriter, "%s Shell integration already configured for %s in %s\n", renderStatusSuccess("✓"), result.shell, result.configPath)
	}

	store, err := loadOrReuseStore(opts.Store)
	if err != nil {
		return fmt.Errorf("shell integration configured, but failed to load model store: %w", err)
	}

	defaultName, hasDefault := configuredDefaultModel(store)
	if hasDefault {
		fmt.Fprintf(stderrWriter, "%s Default model already configured: %s\n", renderStatusSuccess("✓"), defaultName)
		return nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("shell integration configured, but no default model is set; run /add-model in an interactive terminal")
	}

	fmt.Fprintf(stderrWriter, "%s No default model configured. Starting model setup…\n", renderStatusInfo("●"))
	setupOpts := opts
	setupOpts.Store = store
	return handleModelsAdd(setupOpts)
}

func resolveSetupShell(rawArgs string) (string, error) {
	arg := strings.TrimSpace(rawArgs)
	if arg == "" {
		return detectSetupShellFromEnv(os.Getenv("SHELL"))
	}

	fields := strings.Fields(arg)
	if len(fields) != 1 {
		return "", errors.New("usage: /setup [zsh|bash|fish]")
	}

	shell, ok := normalizeSetupShell(fields[0])
	if !ok {
		return "", fmt.Errorf("unsupported shell %q; supported: zsh, bash, fish", fields[0])
	}
	return shell, nil
}

func detectSetupShellFromEnv(envShell string) (string, error) {
	shell, ok := normalizeSetupShell(envShell)
	if ok {
		return shell, nil
	}
	if strings.TrimSpace(envShell) == "" {
		return "", errors.New("unable to detect shell (SHELL is empty); run /setup [zsh|bash|fish]")
	}
	return "", fmt.Errorf("unsupported shell %q; run /setup [zsh|bash|fish]", filepath.Base(envShell))
}

func normalizeSetupShell(raw string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return "", false
	}
	name = filepath.Base(name)
	switch name {
	case "zsh", "bash", "fish":
		return name, true
	default:
		return "", false
	}
}

func loadOrReuseStore(store *modelconfig.ModelStore) (*modelconfig.ModelStore, error) {
	if store != nil {
		return store, nil
	}
	return modelconfig.LoadModelStore()
}

func configuredDefaultModel(store *modelconfig.ModelStore) (string, bool) {
	if store == nil {
		return "", false
	}
	defaultName := strings.TrimSpace(store.DefaultName())
	if defaultName == "" {
		return "", false
	}
	if _, ok := store.Get(defaultName); !ok {
		return "", false
	}
	return defaultName, true
}

func ensureShellIntegration(shell string) (shellSetupResult, error) {
	configPath, err := shellConfigPath(shell)
	if err != nil {
		return shellSetupResult{}, err
	}
	initLine, err := shellInitLine(shell)
	if err != nil {
		return shellSetupResult{}, err
	}
	added, err := appendShellIntegration(configPath, shell, initLine)
	if err != nil {
		return shellSetupResult{}, err
	}
	return shellSetupResult{
		shell:      shell,
		configPath: configPath,
		initLine:   initLine,
		added:      added,
	}, nil
}

func shellConfigPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc"), nil
	case "bash":
		return pickBashConfigPath(home), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	default:
		return "", fmt.Errorf("unsupported shell %q; supported: zsh, bash, fish", shell)
	}
}

func pickBashConfigPath(home string) string {
	candidates := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".profile"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(home, ".bashrc")
}

func shellInitLine(shell string) (string, error) {
	switch shell {
	case "zsh", "bash":
		return fmt.Sprintf("eval \"$(raijin --init %s)\"", shell), nil
	case "fish":
		return "raijin --init fish | source", nil
	default:
		return "", fmt.Errorf("unsupported shell %q; supported: zsh, bash, fish", shell)
	}
}

func appendShellIntegration(path, shell, line string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	markerStart := fmt.Sprintf("# >>> raijin shell setup (%s) >>>", shell)
	markerEnd := fmt.Sprintf("# <<< raijin shell setup (%s) <<<", shell)
	text := string(content)
	if strings.Contains(text, line) || strings.Contains(text, markerStart) {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create config directory for %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var block strings.Builder
	if len(content) > 0 && !strings.HasSuffix(text, "\n") {
		block.WriteString("\n")
	}
	if len(content) > 0 {
		block.WriteString("\n")
	}
	block.WriteString(markerStart)
	block.WriteString("\n")
	block.WriteString(line)
	block.WriteString("\n")
	block.WriteString(markerEnd)
	block.WriteString("\n")

	if _, err := f.WriteString(block.String()); err != nil {
		return false, fmt.Errorf("append setup block to %s: %w", path, err)
	}
	return true, nil
}
