package main

import (
	"reflect"
	"strings"
	"testing"
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
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(mode, query string, candidates []string) (string, error) {
		if mode != replPickerModeComplete {
			t.Fatalf("mode = %q, want %q", mode, replPickerModeComplete)
		}
		if query != "add" {
			t.Fatalf("query = %q, want %q", query, "add")
		}
		return "/add-model", nil
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

func TestReplCompleteLineDoesNotExpandMidSentenceSlash(t *testing.T) {
	line := "please /add"
	newLine, newPos, ok := replCompleteLine(line, len(line), '\t')
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if newLine != line || newPos != len(line) {
		t.Fatalf("expected unchanged line/pos for mid-sentence slash completion, got %q @%d", newLine, newPos)
	}
}

func TestReplCompleteLineDoesNotExpandColonPrefix(t *testing.T) {
	line := ":/add"
	newLine, newPos, ok := replCompleteLine(line, len(line), '\t')
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if newLine != line || newPos != len(line) {
		t.Fatalf("expected unchanged line/pos for : prefix, got %q @%d", newLine, newPos)
	}
}

func TestReplAutoCompleterDoesNotSuggestColonPrefix(t *testing.T) {
	c := newREPLCompleter()
	out, prefixLen := c.Do([]rune(":/add"), len(":/add"))
	if len(out) != 0 || prefixLen != 0 {
		t.Fatalf("expected no suggestions for : prefix, got %#v with prefix len %d", out, prefixLen)
	}
}

func TestReplCompleteLineShowsMatchesForMultipleCandidates(t *testing.T) {
	line := "/s"
	var shown []string
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(mode, query string, candidates []string) (string, error) {
		if mode != replPickerModeComplete {
			t.Fatalf("mode = %q, want %q", mode, replPickerModeComplete)
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
	joined := strings.Join(shown, "\n")
	if !strings.Contains(joined, "/sessions") || !strings.Contains(joined, "/setup") {
		t.Fatalf("expected shown candidates to include /sessions and /setup, got %q", shown)
	}
}

func TestReplCompleteLineUsesPickerSelection(t *testing.T) {
	line := "/sts"
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(mode, query string, candidates []string) (string, error) {
		if mode != replPickerModeComplete {
			t.Fatalf("mode = %q, want %q", mode, replPickerModeComplete)
		}
		if query != "sts" {
			t.Fatalf("query = %q, want %q", query, "sts")
		}
		joined := strings.Join(candidates, "\n")
		if !strings.Contains(joined, "/status") {
			t.Fatalf("expected candidate list to include /status, got %q", candidates)
		}
		return "/status", nil
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

func TestReplBuildPickerRequestForMention(t *testing.T) {
	line := "look at @TOD"
	newLine, newPos, req, ok := replBuildPickerRequest(line, len(line), '\t')
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if newLine != line || newPos != len(line) {
		t.Fatalf("expected unchanged line until picker runs, got %q @%d", newLine, newPos)
	}
	if req == nil {
		t.Fatal("expected mention picker request")
	}
	if req.mode != replPickerModeMention {
		t.Fatalf("mode = %q, want %q", req.mode, replPickerModeMention)
	}
	if req.query != "TOD" {
		t.Fatalf("query = %q, want %q", req.query, "TOD")
	}
}

func TestReplBuildPickerRequestForMultipleCompletions(t *testing.T) {
	line := "/s"
	newLine, newPos, req, ok := replBuildPickerRequest(line, len(line), '\t')
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if newLine != line || newPos != len(line) {
		t.Fatalf("expected unchanged line until picker runs, got %q @%d", newLine, newPos)
	}
	if req == nil {
		t.Fatal("expected completion picker request")
	}
	if req.mode != replPickerModeComplete {
		t.Fatalf("mode = %q, want %q", req.mode, replPickerModeComplete)
	}
	if req.query != "s" {
		t.Fatalf("query = %q, want %q", req.query, "s")
	}
	if len(req.candidates) < 2 {
		t.Fatalf("expected multiple candidates, got %q", req.candidates)
	}
}

func TestReplCompleteLineUsesMentionPicker(t *testing.T) {
	line := "@rep"
	newLine, newPos, ok := replCompleteLineWithPicker(line, len(line), '\t', func(mode, query string, candidates []string) (string, error) {
		if mode != replPickerModeMention {
			t.Fatalf("mode = %q, want %q", mode, replPickerModeMention)
		}
		if query != "rep" {
			t.Fatalf("query = %q, want %q", query, "rep")
		}
		if len(candidates) != 0 {
			t.Fatalf("mention picker should not receive completion candidates, got %q", candidates)
		}
		return "reports/output.txt", nil
	}, nil)
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if newLine != "@reports/output.txt" {
		t.Fatalf("newLine = %q, want %q", newLine, "@reports/output.txt")
	}
	if newPos != len(newLine) {
		t.Fatalf("newPos = %d, want %d", newPos, len(newLine))
	}
}

func TestReplCompleteLineFormatsMentionWithSpaces(t *testing.T) {
	line := "@rep"
	newLine, _, ok := replCompleteLineWithPicker(line, len(line), '\t', func(mode, query string, candidates []string) (string, error) {
		return `reports/my file.txt`, nil
	}, nil)
	if !ok {
		t.Fatalf("expected tab key to be handled")
	}
	if newLine != `@"reports/my file.txt"` {
		t.Fatalf("newLine = %q, want %q", newLine, `@"reports/my file.txt"`)
	}
}

func TestReplRunPickerRequestFormatsMentionSelection(t *testing.T) {
	req := &replPickerRequest{
		mode:       replPickerModeMention,
		line:       "look at @rep",
		tokenStart: len("look at "),
		tokenEnd:   len("look at @rep"),
		query:      "rep",
	}
	got, err := replApplyPickerSelection(req, `reports/my file.txt`)
	if err != nil {
		t.Fatalf("replApplyPickerSelection() error = %v", err)
	}
	if got != `look at @"reports/my file.txt"` {
		t.Fatalf("got %q", got)
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

func TestReplFormatMentionEscapesQuotesAndBackslashes(t *testing.T) {
	got := replFormatMention(`dir\sub "quoted".txt`)
	want := `@"dir\\sub \"quoted\".txt"`
	if got != want {
		t.Fatalf("replFormatMention() = %q, want %q", got, want)
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	got := longestCommonPrefix([]string{"/status", "/setup", "/sessions"})
	if got != "/s" {
		t.Fatalf("longestCommonPrefix = %q, want %q", got, "/s")
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
