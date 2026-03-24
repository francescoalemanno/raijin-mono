package shellinit

import (
	"io"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/completion"
)

func TestCandidatesUniversal(t *testing.T) {
	tests := []struct {
		input    string
		expected bool // true if we expect candidates
	}{
		{"ls", true},   // bare tokens now have universal completion
		{"ls ", false}, // trailing space = no active token
		{":", true},    // : is now treated as universal (combined with bare tokens)
		{":h", true},   // :h gets universal completion
		{"/", true},
		{"/s", true},
		{"cat @", true},
		{"cat @f", true},
		{"cat +", true},
		{"cat +s", true},
		{"git commit", true}, // last token "commit" has universal completion
		{":status ", false},  // trailing space = no active token
	}

	for _, tc := range tests {
		token := completion.ParseLastToken(tc.input)
		hasCandidates := token.Type != completion.TokenUnknown
		if hasCandidates != tc.expected {
			t.Errorf("ParseLastToken(%q) hasCandidates = %v, want %v", tc.input, hasCandidates, tc.expected)
		}
	}
}

func TestCompleteSelectionEmptySignal(t *testing.T) {
	// Stub FZF - return nothing to simulate cancellation
	prev := runFZFForComplete
	t.Cleanup(func() {
		runFZFForComplete = prev
	})
	runFZFForComplete = func(mode, query string, stdin io.Reader, stdout io.Writer) (int, error) {
		return 0, nil
	}

	// Ambiguous input should return the original string as-is when the picker emits nothing.
	got := CompleteSelection("s")
	if got != "s" {
		t.Errorf("CompleteSelection(\"s\") = %q, want \"s\"", got)
	}

	// :h should return something starting with :help or similar
	// But since FZF is stubbed to return nothing, it will return the original input
	got = CompleteSelection(":h")
	if got != ":h" {
		t.Errorf("CompleteSelection(\":h\") = %q, want \":h\" when picker cancelled", got)
	}
}
