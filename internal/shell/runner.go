package shell

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ExecSpec describes a command invocation.
type ExecSpec struct {
	Path  string
	Args  []string
	Env   []string
	Dir   string
	Stdin io.Reader
}

// UserShellCommand resolves the command used to execute a shell expression.
func UserShellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}

	if bashPath, err := exec.LookPath("bash"); err == nil {
		return bashPath, []string{"-lc", command}
	}
	return "/bin/bash", []string{"-lc", command}
}

// Run executes a command and cancels it robustly across platforms.
func Run(ctx context.Context, spec ExecSpec, stdout, stderr io.Writer) error {
	if strings.TrimSpace(spec.Path) == "" {
		return errors.New("command path is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	cmd := exec.Command(spec.Path, spec.Args...)
	configureCommand(cmd)
	cmd.Env = spec.Env
	cmd.Dir = spec.Dir
	cmd.Stdin = spec.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		select {
		case err := <-done:
			return err
		default:
		}
		killProcessTree(cmd)
		<-done
		return ctx.Err()
	}
}

// PrependPath prepends dirs to the PATH variable in an environ slice.
func PrependPath(environ []string, dirs []string) []string {
	if len(dirs) == 0 {
		return append([]string(nil), environ...)
	}

	prefix := strings.Join(dirs, string(os.PathListSeparator))
	result := make([]string, 0, len(environ))
	found := false
	for _, entry := range environ {
		if after, ok := strings.CutPrefix(entry, "PATH="); ok {
			existing := after
			if existing == "" {
				result = append(result, "PATH="+prefix)
			} else {
				result = append(result, "PATH="+prefix+string(os.PathListSeparator)+existing)
			}
			found = true
		} else {
			result = append(result, entry)
		}
	}
	if !found {
		result = append(result, "PATH="+prefix)
	}
	return result
}
