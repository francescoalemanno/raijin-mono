package shell

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPrependPath(t *testing.T) {
	t.Parallel()
	env := []string{"A=1", "PATH=/usr/bin", "B=2"}
	got := PrependPath(env, []string{"/custom/bin", "/more/bin"})
	wantPrefix := "PATH=/custom/bin" + string(pathListSeparator()) + "/more/bin" + string(pathListSeparator()) + "/usr/bin"
	if got[1] != wantPrefix {
		t.Fatalf("PATH mismatch: got %q", got[1])
	}
}

func TestRunCancelsQuickly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses unix shell semantics")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- Run(ctx, ExecSpec{Path: "/bin/sh", Args: []string{"-lc", "sleep 4 & wait"}}, nil, nil)
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("cancellation took too long: %v", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("run did not return promptly after cancellation")
	}
}

func pathListSeparator() rune {
	if runtime.GOOS == "windows" {
		return ';'
	}
	return ':'
}

func TestUserShellCommand(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")

	path, args := UserShellCommand("echo hi")

	if runtime.GOOS == "windows" {
		if path != "cmd" {
			t.Fatalf("path = %q, want cmd", path)
		}
		if len(args) != 2 || args[0] != "/C" || args[1] != "echo hi" {
			t.Fatalf("args = %#v, want [/C echo hi]", args)
		}
		return
	}

	wantPath := "/bin/bash"
	if bashPath, err := exec.LookPath("bash"); err == nil {
		wantPath = bashPath
	}
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	if base := filepath.Base(path); base != "bash" && base != "bash.exe" {
		t.Fatalf("expected bash executable, got %q", path)
	}
	if len(args) != 2 || args[0] != "-lc" || args[1] != "echo hi" {
		t.Fatalf("args = %#v, want [-lc echo hi]", args)
	}
}
