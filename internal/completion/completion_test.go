package completion

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		line          string
		pos           int
		wantType      TokenType
		wantRaw       string
		wantQuery     string
		wantHasPrefix bool
	}{
		{
			name:     "empty line",
			line:     "",
			pos:      0,
			wantType: TokenUnknown,
		},
		{
			name:          "simple word - no prefix returns universal",
			line:          "hello",
			pos:           5,
			wantType:      TokenUniversal,
			wantRaw:       "hello",
			wantQuery:     "hello",
			wantHasPrefix: false,
		},
		{
			name:          "at mention",
			line:          "cat @file",
			pos:           9,
			wantType:      TokenFiles,
			wantRaw:       "@file",
			wantQuery:     "file",
			wantHasPrefix: true,
		},
		{
			name:          "slash command",
			line:          "/help",
			pos:           5,
			wantType:      TokenCommands,
			wantRaw:       "/help",
			wantQuery:     "help",
			wantHasPrefix: true,
		},
		{
			name:          "plus skill",
			line:          "+skill",
			pos:           6,
			wantType:      TokenSkills,
			wantRaw:       "+skill",
			wantQuery:     "skill",
			wantHasPrefix: true,
		},
		{
			name:     "percent prefix is unsupported",
			line:     "%explorer",
			pos:      9,
			wantType: TokenUnknown,
		},
		{
			name:          "mid token - no prefix returns universal",
			line:          "hello world",
			pos:           8,
			wantType:      TokenUniversal,
			wantRaw:       "world",
			wantQuery:     "world",
			wantHasPrefix: false,
		},
		{
			name:          "colon prefix returns universal with query after colon",
			line:          ":help",
			pos:           5,
			wantType:      TokenUniversal,
			wantRaw:       ":help",
			wantQuery:     "help",
			wantHasPrefix: true,
		},
		{
			name:          "colon + slash prefix returns commands",
			line:          ":/add",
			pos:           5,
			wantType:      TokenCommands,
			wantRaw:       ":/add",
			wantQuery:     "add",
			wantHasPrefix: true,
		},
		{
			name:          "colon + plus prefix returns skills",
			line:          ":+skill",
			pos:           7,
			wantType:      TokenSkills,
			wantRaw:       ":+skill",
			wantQuery:     "skill",
			wantHasPrefix: true,
		},
		{
			name:     "colon + percent prefix is unsupported",
			line:     ":%explorer",
			pos:      10,
			wantType: TokenUnknown,
		},
		{
			name:          "colon + at prefix returns files",
			line:          ":@file",
			pos:           6,
			wantType:      TokenFiles,
			wantRaw:       ":@file",
			wantQuery:     "file",
			wantHasPrefix: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := Parse(tc.line, tc.pos)
			if token.Type != tc.wantType {
				t.Errorf("Type = %v, want %v", token.Type, tc.wantType)
			}
			if tc.wantType != TokenUnknown {
				if token.Raw != tc.wantRaw {
					t.Errorf("Raw = %q, want %q", token.Raw, tc.wantRaw)
				}
				if token.Query != tc.wantQuery {
					t.Errorf("Query = %q, want %q", token.Query, tc.wantQuery)
				}
				if token.HasPrefix != tc.wantHasPrefix {
					t.Errorf("HasPrefix = %v, want %v", token.HasPrefix, tc.wantHasPrefix)
				}
			}
		})
	}
}

func TestParseLastToken(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantType TokenType
		wantRaw  string
	}{
		{
			name:     "empty",
			line:     "",
			wantType: TokenUnknown,
		},
		{
			name:     "ends with space",
			line:     "hello ",
			wantType: TokenUnknown,
		},
		{
			name:     "single word - no prefix returns universal",
			line:     "hello",
			wantType: TokenUniversal,
			wantRaw:  "hello",
		},
		{
			name:     "last token is file",
			line:     "cat @path",
			wantType: TokenFiles,
			wantRaw:  "@path",
		},
		{
			name:     "last token is command",
			line:     "/help",
			wantType: TokenCommands,
			wantRaw:  "/help",
		},
		{
			name:     "last token is skill",
			line:     "+myskill",
			wantType: TokenSkills,
			wantRaw:  "+myskill",
		},
		{
			name:     "last token percent prefix is unsupported",
			line:     "%explorer",
			wantType: TokenUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := ParseLastToken(tc.line)
			if token.Type != tc.wantType {
				t.Errorf("Type = %v, want %v", token.Type, tc.wantType)
			}
			if tc.wantType != TokenUnknown && token.Raw != tc.wantRaw {
				t.Errorf("Raw = %q, want %q", token.Raw, tc.wantRaw)
			}
		})
	}
}

func TestApply(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		token    Token
		selected string
		want     string
	}{
		{
			name: "replace at end",
			line: "cat @fil",
			token: Token{
				Type:  TokenFiles,
				Raw:   "@fil",
				Start: 4,
				End:   8,
			},
			selected: "@file.txt",
			want:     "cat @file.txt",
		},
		{
			name: "replace in middle",
			line: "cat @fil and more",
			token: Token{
				Type:  TokenFiles,
				Raw:   "@fil",
				Start: 4,
				End:   8,
			},
			selected: "@file.txt",
			want:     "cat @file.txt and more",
		},
		{
			name: "colon prefix adds space",
			line: ":add",
			token: Token{
				Type:  TokenUniversal,
				Raw:   ":add",
				Start: 0,
				End:   4,
			},
			selected: "/add-model",
			want:     ": /add-model",
		},
		{
			name: "colon slash prefix adds space",
			line: ":/add",
			token: Token{
				Type:  TokenCommands,
				Raw:   ":/add",
				Start: 0,
				End:   5,
			},
			selected: "/add-model",
			want:     ": /add-model",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Apply(tc.line, tc.token, tc.selected)
			if got != tc.want {
				t.Errorf("Apply() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetCandidates(t *testing.T) {
	// Test that candidates are returned with proper prefixes
	tests := []struct {
		name          string
		tokenType     TokenType
		checkPrefix   string
		checkNonEmpty bool
	}{
		{
			name:          "commands have slash prefix",
			tokenType:     TokenCommands,
			checkPrefix:   "/",
			checkNonEmpty: true,
		},
		{
			name:          "skills have plus prefix",
			tokenType:     TokenSkills,
			checkPrefix:   "+",
			checkNonEmpty: true,
		},
		{
			name:          "files have at prefix",
			tokenType:     TokenFiles,
			checkPrefix:   "@",
			checkNonEmpty: false, // may be empty in test environment
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := Token{Type: tc.tokenType}
			candidates := GetCandidates(token)

			if tc.checkNonEmpty && len(candidates) == 0 {
				t.Error("expected non-empty candidates")
			}

			for _, c := range candidates {
				if !strings.HasPrefix(c.Value, tc.checkPrefix) {
					t.Errorf("candidate %q missing prefix %q", c.Value, tc.checkPrefix)
				}
				if strings.HasPrefix(c.Display, tc.checkPrefix) {
					t.Errorf("candidate display %q should not have prefix %q", c.Display, tc.checkPrefix)
				}
			}
		})
	}
}

func TestFilterCandidates(t *testing.T) {
	candidates := []Candidate{
		{Value: "/help", Display: "help", QueryText: "help"},
		{Value: "/new", Display: "new", QueryText: "new"},
		{Value: "/status", Display: "status", QueryText: "status"},
	}

	tests := []struct {
		name  string
		query string
		want  int // expected number of matches
	}{
		{
			name:  "empty query returns all",
			query: "",
			want:  3,
		},
		{
			name:  "exact match",
			query: "help",
			want:  1,
		},
		{
			name:  "fuzzy match",
			query: "st",
			want:  1, // status
		},
		{
			name:  "prefix match",
			query: "ne",
			want:  1, // new
		},
		{
			name:  "no match",
			query: "xyz",
			want:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := Token{Type: TokenCommands, Query: tc.query}
			filtered := FilterCandidates(candidates, token)
			if len(filtered) != tc.want {
				t.Errorf("got %d matches, want %d", len(filtered), tc.want)
			}
		})
	}
}

func TestUniversalCandidates(t *testing.T) {
	// Universal mode should include commands and skills.
	token := Token{Type: TokenUniversal}
	candidates := GetCandidates(token)

	var hasCommand, hasSkill bool
	for _, c := range candidates {
		if strings.HasPrefix(c.Value, "/") {
			hasCommand = true
		}
		if strings.HasPrefix(c.Value, "+") {
			hasSkill = true
		}
	}

	if !hasCommand {
		t.Error("universal candidates should include commands")
	}
	if !hasSkill {
		t.Error("universal candidates should include skills")
	}
}

func TestCommandCandidatesIncludePlanningCommands(t *testing.T) {
	candidates := commandCandidates()
	hasPlan := false
	for _, c := range candidates {
		if c.Value == "/plan" && c.Display == "plan" {
			hasPlan = true
		}
	}
	if !hasPlan {
		t.Fatalf("expected /plan in command candidates, got %#v", candidates)
	}
	for _, c := range candidates {
		if c.Value == "/start-plan" || c.Value == "/read-plan" {
			t.Fatalf("expected old Ralph commands to be absent, got %#v", candidates)
		}
	}
}

func TestCommandCandidatesIncludePreviewDocs(t *testing.T) {
	candidates := commandCandidates()
	for _, c := range candidates {
		if c.Value != "/help" {
			continue
		}
		if !strings.Contains(c.Preview, "Show this help message") {
			t.Fatalf("preview for /help = %q, want builtin description", c.Preview)
		}
		return
	}
	t.Fatal("expected /help in command candidates")
}

func TestSkillCandidatesIncludePreviewDocs(t *testing.T) {
	candidates := skillCandidates()
	for _, c := range candidates {
		if c.Value != "+make-skill" {
			continue
		}
		if !strings.Contains(c.Preview, "Creates or updates skills for Raijin") {
			t.Fatalf("preview for +make-skill = %q, want skill description", c.Preview)
		}
		return
	}
	t.Fatal("expected +make-skill in skill candidates")
}

func TestCompletionTokenBounds(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		wantStart int
		wantEnd   int
		wantOk    bool
	}{
		{
			name:      "empty",
			current:   "",
			wantStart: 0,
			wantEnd:   0,
			wantOk:    false,
		},
		{
			name:      "single token",
			current:   "help",
			wantStart: 0,
			wantEnd:   4,
			wantOk:    true,
		},
		{
			name:      "with trailing space",
			current:   "help  ",
			wantStart: 0,
			wantEnd:   4,
			wantOk:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end, ok := CompletionTokenBounds(tc.current)
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if ok {
				if start != tc.wantStart {
					t.Errorf("start = %d, want %d", start, tc.wantStart)
				}
				if end != tc.wantEnd {
					t.Errorf("end = %d, want %d", end, tc.wantEnd)
				}
			}
		})
	}
}

func TestPrefixNotStripped(t *testing.T) {
	// Ensure that the prefix is preserved in the returned value
	candidates := commandCandidates()
	if len(candidates) == 0 {
		t.Skip("no command candidates available")
	}

	for _, c := range candidates {
		if !strings.HasPrefix(c.Value, "/") {
			t.Errorf("command candidate %q should have / prefix", c.Value)
		}
	}

	skillCandidates := skillCandidates()
	for _, c := range skillCandidates {
		if !strings.HasPrefix(c.Value, "+") {
			t.Errorf("skill candidate %q should have + prefix", c.Value)
		}
	}
}
