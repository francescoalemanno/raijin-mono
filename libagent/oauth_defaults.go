package libagent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libagent/oauth"
)

// DefaultLoginCallbacks returns OAuth callbacks that work out of the box
// without any configuration:
//
//   - OnAuth:            opens the authorization URL in the system browser
//     (falls back to printing it if the browser cannot be launched),
//     then prints any extra instructions (e.g. the device code).
//   - OnProgress:        prints status messages to stderr.
//   - OnPrompt:          prints the prompt to stderr and reads a line from stdin.
//   - OnManualCodeInput: prints a hint to stderr and reads a line from stdin,
//     racing with the local callback server for providers
//     that start one (Gemini CLI, Antigravity, OpenAI Codex).
func DefaultLoginCallbacks() oauth.LoginCallbacks {
	return oauth.LoginCallbacks{
		OnAuth: func(info oauth.AuthInfo) {
			if info.Instructions != "" {
				fmt.Fprintf(os.Stderr, "%s\n", info.Instructions)
			}
			fmt.Fprintf(os.Stderr, "Opening browser for authentication...\n")
			if err := openBrowser(info.URL); err != nil {
				fmt.Fprintf(os.Stderr, "Open this URL in your browser:\n  %s\n", info.URL)
			} else {
				fmt.Fprintf(os.Stderr, "If the browser did not open, visit:\n  %s\n", info.URL)
			}
		},

		OnProgress: func(msg string) {
			fmt.Fprintln(os.Stderr, msg)
		},

		OnPrompt: func(_ context.Context, p oauth.Prompt) (string, error) {
			if p.Placeholder != "" {
				fmt.Fprintf(os.Stderr, "%s [%s]: ", p.Message, p.Placeholder)
			} else {
				fmt.Fprintf(os.Stderr, "%s: ", p.Message)
			}
			line, err := readStdinLine()
			if err != nil {
				return "", err
			}
			return line, nil
		},

		// OnManualCodeInput races with the local callback server. The user can
		// paste the full redirect URL; pressing Enter with an empty line lets
		// the server callback win (if one is running).
		OnManualCodeInput: func(_ context.Context) (string, error) {
			fmt.Fprintln(os.Stderr, "Paste the redirect URL here (or press Enter to wait for browser callback):")
			return readStdinLine()
		},
	}
}

// LoginCallbacksWithPrinter returns OAuth callbacks that route status messages
// to a custom printer function instead of stderr. Useful for TUI applications.
func LoginCallbacksWithPrinter(printer func(string)) oauth.LoginCallbacks {
	cb := DefaultLoginCallbacks()
	cb.OnProgress = func(msg string) {
		printer(msg)
	}
	cb.OnAuth = func(info oauth.AuthInfo) {
		if info.Instructions != "" {
			printer(info.Instructions)
		}
		printer("Opening browser for authentication...")
		if err := openBrowser(info.URL); err != nil {
			printer("Open this URL in your browser: " + info.URL)
		}
	}
	return cb
}

// openBrowser attempts to open url in the system default browser.
// Returns an error if the command could not be started.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// readStdinLine reads one line from stdin, stripping the trailing newline.
func readStdinLine() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return strings.TrimRight(line, "\r\n"), err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
