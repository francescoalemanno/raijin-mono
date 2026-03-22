package main

import (
	"reflect"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/completion"
)

func TestReplSubprocessArgs(t *testing.T) {
	base := []string{"--profile-dir", "/tmp/raijin-prof", "--new"}
	got := replSubprocessArgs(base, "fix this bug")
	want := []string{"--profile-dir", "/tmp/raijin-prof", "--new", "fix this bug"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replSubprocessArgs() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(base, []string{"--profile-dir", "/tmp/raijin-prof", "--new"}) {
		t.Fatalf("replSubprocessArgs() mutated base args: %#v", base)
	}
}

func TestIsREPLExitInput(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{in: "/exit", want: true},
		{in: "exit", want: true},
		{in: "quit", want: true},
		{in: "  Exit  ", want: true},
		{in: "/new", want: false},
		{in: "hello", want: false},
	}
	for _, tc := range cases {
		if got := isREPLExitInput(tc.in); got != tc.want {
			t.Fatalf("isREPLExitInput(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestReplCompleteLineIgnoresNonTabKeys(t *testing.T) {
	_, _, ok := replCompleteLine("/add", len("/add"), 'x')
	if ok {
		t.Fatalf("expected non-tab key to bypass autocomplete")
	}
}

func TestReplCompleteLineExpandsSingleMatch(t *testing.T) {
	line := "/add"
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(query string, candidates []completion.Candidate, tokenType completion.TokenType) (string, error) {
		if tokenType != completion.TokenCommands {
			t.Fatalf("tokenType = %v, want TokenCommands", tokenType)
		}
		if query != "add" {
			t.Fatalf("query = %q, want %q", query, "add")
		}
		return "add-model", nil
	}, nil)
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if want := "/add-model"; newLine != want {
		t.Fatalf("newLine = %q, want %q", newLine, want)
	}
	if newPos != len(newLine) {
		t.Fatalf("newPos = %d, want %d", newPos, len(newLine))
	}
}

func TestReplCompleteLineExpandsMidSentenceSlash(t *testing.T) {
	// With the unified completion system, mid-sentence completions work
	line := "please /add"
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(query string, candidates []completion.Candidate, tokenType completion.TokenType) (string, error) {
		return "add-model", nil
	}, nil)
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if want := "please /add-model"; newLine != want {
		t.Fatalf("newLine = %q, want %q", newLine, want)
	}
	if newPos != len(newLine) {
		t.Fatalf("newPos = %d, want %d", newPos, len(newLine))
	}
}

func TestReplCompleteLineExpandsColonPrefix(t *testing.T) {
	// Colon prefix now triggers completion: ":/add" -> ": /add-model"
	line := ":/add"
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(query string, candidates []completion.Candidate, tokenType completion.TokenType) (string, error) {
		return "/add-model", nil
	}, nil)
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if want := ": /add-model"; newLine != want {
		t.Fatalf("newLine = %q, want %q", newLine, want)
	}
	if newPos != len(newLine) {
		t.Fatalf("newPos = %d, want %d", newPos, len(newLine))
	}
}

func TestReplAutoCompleterSuggestsColonPrefix(t *testing.T) {
	c := newREPLCompleter()
	out, prefixLen := c.Do([]rune(":/add"), len(":/add"))
	// Should now return suggestions for colon + slash prefix
	if len(out) == 0 {
		t.Fatalf("expected suggestions for :/add, got none")
	}
	if prefixLen == 0 {
		t.Fatalf("expected prefixLen > 0 for :/add")
	}
}

func TestReplCompleteLineShowsMatchesForMultipleCandidates(t *testing.T) {
	line := "/s"
	var shown []string
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(query string, candidates []completion.Candidate, tokenType completion.TokenType) (string, error) {
		if tokenType != completion.TokenCommands {
			t.Fatalf("tokenType = %v, want TokenCommands", tokenType)
		}
		if query != "s" {
			t.Fatalf("query = %q, want %q", query, "s")
		}
		return "", nil
	}, func(candidates []string) {
		shown = append([]string{}, candidates...)
	})
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if newLine != line || newPos != len(line) {
		t.Fatalf("expected unchanged line/pos when no longer shared prefix, got %q @%d", newLine, newPos)
	}
	if len(shown) < 2 {
		t.Fatalf("expected multiple shown candidates, got %q", shown)
	}
}

func TestReplCompleteLineUsesPickerSelection(t *testing.T) {
	line := "/sts"
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(query string, candidates []completion.Candidate, tokenType completion.TokenType) (string, error) {
		if tokenType != completion.TokenCommands {
			t.Fatalf("tokenType = %v, want TokenCommands", tokenType)
		}
		if query != "sts" {
			t.Fatalf("query = %q, want %q", query, "sts")
		}
		// Return the display value; the function will find the matching candidate
		return "status", nil
	}, nil)
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if newLine != "/status" {
		t.Fatalf("newLine = %q, want %q", newLine, "/status")
	}
	if newPos != len(newLine) {
		t.Fatalf("newPos = %d, want %d", newPos, len(newLine))
	}
}

func TestReplCompleteLineUsesMentionPicker(t *testing.T) {
	line := "@rep"
	_, _, ok := replCompleteLineWithPicker(line, len(line), '\t', func(query string, candidates []completion.Candidate, tokenType completion.TokenType) (string, error) {
		if tokenType != completion.TokenFiles {
			t.Fatalf("tokenType = %v, want TokenFiles", tokenType)
		}
		if query != "rep" {
			t.Fatalf("query = %q, want %q", query, "rep")
		}
		// Return a display that matches one of the candidates
		// The completion system will look up the full value
		if len(candidates) > 0 {
			return candidates[0].Display, nil
		}
		return "", nil
	}, nil)
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	// If no candidates, line stays the same
	// If candidates exist, the selected one will be used
	// Either way, the function should complete without error
}

func TestReplCompleteLineFormatsMentionWithSpaces(t *testing.T) {
	// Test the mention formatting function directly
	formatted := completion.MentionEscape("reports/my file.txt")
	if formatted != `@"reports/my file.txt"` {
		t.Fatalf("MentionEscape = %q, want %q", formatted, `@"reports/my file.txt"`)
	}

	// Test without spaces
	formatted = completion.MentionEscape("reports/file.txt")
	if formatted != "@reports/file.txt" {
		t.Fatalf("MentionEscape = %q, want %q", formatted, "@reports/file.txt")
	}
}

func TestReplMultilineContinuation(t *testing.T) {
	cases := []struct {
		line     string
		wantText string
		wantOK   bool
	}{
		{line: `hello world\`, wantText: "hello world", wantOK: true},
		{line: `first line\  `, wantText: "first line", wantOK: true},
		{line: "no continuation", wantText: "", wantOK: false},
		{line: "", wantText: "", wantOK: false},
		{line: `\`, wantText: "", wantOK: true},
		{line: `path\\`, wantText: `path\`, wantOK: true},
	}
	for _, tc := range cases {
		text, ok := replMultilineContinuation(tc.line)
		if ok != tc.wantOK || text != tc.wantText {
			t.Fatalf("replMultilineContinuation(%q) = (%q, %v), want (%q, %v)",
				tc.line, text, ok, tc.wantText, tc.wantOK)
		}
	}
}

func TestParseStatusOutput(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "full output",
			input: "Model: anthropic/claude-sonnet-4-20250514\nReasoning: medium\nContext: 12.3% (24k/200k)\n",
			want:  "anthropic/claude-sonnet-4-20250514 · medium · ctx:12.3%",
		},
		{
			name:  "unknown context",
			input: "Model: openai/gpt-4\nReasoning: low\nContext: unknown (5k used)\n",
			want:  "openai/gpt-4 · low · ctx:unknown",
		},
		{
			name:  "model only",
			input: "Model: openai/gpt-4\nReasoning: low\n",
			want:  "openai/gpt-4 · low",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStatusOutput(tc.input)
			if got != tc.want {
				t.Fatalf("parseStatusOutput() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseContextPercent(t *testing.T) {
	cases := []struct {
		in     string
		want   float64
		wantOK bool
	}{
		{in: "ctx:12.3%", want: 12.3, wantOK: true},
		{in: "ctx:99", want: 99, wantOK: true},
		{in: "ctx:unknown", want: 0, wantOK: false},
	}

	for _, tc := range cases {
		got, ok := parseContextPercent(tc.in)
		if ok != tc.wantOK {
			t.Fatalf("parseContextPercent(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Fatalf("parseContextPercent(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	got := longestCommonPrefix([]string{"/status", "/setup", "/sessions"})
	if got != "/s" {
		t.Fatalf("longestCommonPrefix = %q, want %q", got, "/s")
	}
}

func TestReplCompleterDo(t *testing.T) {
	c := newREPLCompleter()

	// Test with slash command
	out, prefixLen := c.Do([]rune("/h"), len("/h"))
	if len(out) == 0 {
		t.Fatal("expected suggestions for /h")
	}
	if prefixLen != 2 {
		t.Fatalf("expected prefixLen = 2, got %d", prefixLen)
	}

	// Test with colon prefix (should return universal candidates)
	out, _ = c.Do([]rune(":help"), len(":help"))
	if len(out) == 0 {
		t.Fatalf("expected suggestions for :help, got none")
	}

	// Test with bare token that matches universal candidates
	// Use empty query to get all candidates
	_, _ = c.Do([]rune(""), 0)
	// Empty line returns unknown token, so no suggestions

	// Test with partial match to commands
	out, _ = c.Do([]rune("stat"), len("stat"))
	if len(out) == 0 {
		t.Fatalf("expected suggestions for 'stat', got none")
	}
}
