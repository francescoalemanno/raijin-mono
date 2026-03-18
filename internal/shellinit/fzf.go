package shellinit

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
	jfzf "github.com/junegunn/fzf/src"
)

type RunFZFOptions struct {
	ExpectKeys              []string
	Bindings                []string
	DisableSingleItemBypass bool
	DisableSelectOne        bool
	DisableSort             bool
	Header                  string
}

type RunFZFResult struct {
	Code     int
	Selected []string
	Key      string
}

// RunFZF launches the embedded fzf picker.
//
// Modes:
//   - default / complete / repl-complete: read candidates from stdin
//   - paths: walk the current workspace and feed @-mention paths
func RunFZF(mode, query string, stdin io.Reader, stdout io.Writer) (int, error) {
	result, err := RunFZFWithOptions(mode, query, stdin, RunFZFOptions{})
	if err != nil {
		return result.Code, err
	}
	for _, item := range result.Selected {
		if _, writeErr := fmt.Fprintln(stdout, item); writeErr != nil {
			return jfzf.ExitError, writeErr
		}
	}
	return result.Code, nil
}

func RunFZFWithOptions(mode, query string, stdin io.Reader, cfg RunFZFOptions) (RunFZFResult, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "default"
	}

	items, err := fzfItems(mode, stdin)
	if err != nil {
		return RunFZFResult{Code: jfzf.ExitError}, err
	}
	if len(items) == 0 {
		return RunFZFResult{Code: 0}, nil
	}
	if len(items) == 1 && !cfg.DisableSingleItemBypass {
		return RunFZFResult{Code: 0, Selected: []string{items[0]}}, nil
	}

	args := fzfArgs(mode, query, cfg)
	options, err := jfzf.ParseOptions(true, args)
	if err != nil {
		return RunFZFResult{Code: jfzf.ExitError}, err
	}

	inputChan := make(chan string)
	go func() {
		defer close(inputChan)
		for _, item := range items {
			inputChan <- item
		}
	}()

	outputChan := make(chan string)
	resultChan := make(chan []string, 1)
	go func() {
		var selected []string
		for item := range outputChan {
			selected = append(selected, item)
		}
		resultChan <- selected
	}()

	options.Input = inputChan
	options.Output = outputChan

	code, err := jfzf.Run(options)
	close(outputChan)
	result := RunFZFResult{
		Code:     code,
		Selected: <-resultChan,
	}
	if len(cfg.ExpectKeys) == 0 || len(result.Selected) == 0 {
		return result, err
	}
	result.Key, result.Selected = splitExpectOutput(result.Selected, cfg.ExpectKeys)
	return result, err
}

func splitExpectOutput(lines []string, expectKeys []string) (string, []string) {
	if len(lines) == 0 {
		return "", nil
	}

	first := strings.TrimSpace(lines[0])
	if first == "" {
		if len(lines) == 1 {
			return "", nil
		}
		// Some fzf builds emit an empty first line for Enter when --expect is used.
		return "", lines[1:]
	}

	expectSet := make(map[string]struct{}, len(expectKeys))
	for _, key := range expectKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		expectSet[key] = struct{}{}
	}
	if _, ok := expectSet[first]; ok {
		return first, lines[1:]
	}

	// Other builds don't emit a key line when Enter is pressed; keep full selection.
	return "", lines
}

func fzfArgs(mode, query string, cfg RunFZFOptions) []string {
	args := []string{"--reverse", "--border", "--no-scrollbar", "--exit-0"}
	if !cfg.DisableSelectOne {
		args = append(args, "--select-1")
	}
	if cfg.DisableSort {
		args = append(args, "--no-sort")
	}
	switch mode {
	case "paths":
		args = append(args, "--scheme=path", "--prompt=@ ")
	case "complete":
		args = append(args, "--height=80%")
		args = append(args, "--prompt=Raijin > ")
	case "repl-complete":
		args = append(args, "--prompt=Raijin > ")
	default:
		args = append(args, "--height=80%")
		args = append(args, "--prompt=> ")
	}
	if query != "" {
		args = append(args, "--query="+query)
	}
	if header := strings.TrimSpace(cfg.Header); header != "" {
		args = append(args, "--header="+header)
	}
	if len(cfg.ExpectKeys) > 0 {
		keys := make([]string, 0, len(cfg.ExpectKeys))
		for _, key := range cfg.ExpectKeys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			keys = append(keys, key)
		}
		if len(keys) > 0 {
			args = append(args, "--expect="+strings.Join(keys, ","))
		}
	}
	for _, binding := range cfg.Bindings {
		binding = strings.TrimSpace(binding)
		if binding == "" {
			continue
		}
		args = append(args, "--bind="+binding)
	}
	return args
}

func fzfItems(mode string, stdin io.Reader) ([]string, error) {
	switch mode {
	case "default", "complete", "repl-complete":
		return readStdinItems(stdin)
	case "paths":
		return mentionPaths(".")
	default:
		return nil, fmt.Errorf("unsupported fzf mode %q", mode)
	}
}

func readStdinItems(stdin io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(stdin)
	const maxTokenSize = 1024 * 1024
	scanner.Buffer(make([]byte, 0, 64*1024), maxTokenSize)

	var items []string
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		items = append(items, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read fzf input: %w", err)
	}
	return items, nil
}

func mentionPaths(root string) ([]string, error) {
	cwd := root
	if cwd == "" {
		cwd = "."
	}
	absRoot, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve mention root: %w", err)
	}

	var items []string
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == absRoot {
			return nil
		}

		name := d.Name()
		if d.IsDir() && fsutil.ShouldSkipMentionDir(name) {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}
		rel = fsutil.NormalizePath(rel)
		if rel == "." || rel == "" {
			return nil
		}
		items = append(items, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk mention paths: %w", err)
	}
	sort.Strings(items)
	return items, nil
}
