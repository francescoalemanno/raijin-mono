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

// RunFZF launches the embedded fzf picker.
//
// Modes:
//   - default / complete / repl-complete: read candidates from stdin
//   - paths: walk the current workspace and feed @-mention paths
func RunFZF(mode, query string, stdin io.Reader, stdout io.Writer) (int, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "default"
	}

	items, err := fzfItems(mode, stdin)
	if err != nil {
		return jfzf.ExitError, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	if len(items) == 1 {
		_, err := fmt.Fprintln(stdout, items[0])
		if err != nil {
			return jfzf.ExitError, err
		}
		return 0, nil
	}

	args := fzfArgs(mode, query)
	options, err := jfzf.ParseOptions(true, args)
	if err != nil {
		return jfzf.ExitError, err
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
	selected := <-resultChan
	for _, item := range selected {
		if _, writeErr := fmt.Fprintln(stdout, item); writeErr != nil && err == nil {
			err = writeErr
			code = jfzf.ExitError
		}
	}
	return code, err
}

func fzfArgs(mode, query string) []string {
	args := []string{"--reverse", "--border", "--no-scrollbar", "--select-1", "--exit-0"}
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
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
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
