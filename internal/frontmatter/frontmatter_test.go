package frontmatter

import (
	"testing"
)

func TestParseHeaderAndBody(t *testing.T) {
	t.Parallel()

	content := `---
description: "demo"
---

body`

	header, body, ok := Parse(content)
	if !ok {
		t.Fatalf("expected frontmatter to be parsed")
	}

	if got := FirstValue(header, "description"); got != "demo" {
		t.Fatalf("description = %q, want %q", got, "demo")
	}

	if body != "body" {
		t.Fatalf("body = %q, want %q", body, "body")
	}
}

func TestParseNoHeader(t *testing.T) {
	t.Parallel()

	content := "hello\nworld"
	header, body, ok := Parse(content)
	if ok {
		t.Fatalf("expected no frontmatter")
	}
	if header != nil {
		t.Fatalf("header = %#v, want nil", header)
	}
	if body != content {
		t.Fatalf("body = %q, want %q", body, content)
	}
}

func TestStripOptionalQuotes(t *testing.T) {
	t.Parallel()

	if got := StripOptionalQuotes(`"line\nbreak"`); got != "line\nbreak" {
		t.Fatalf("StripOptionalQuotes() = %q, want %q", got, "line\nbreak")
	}
	if got := StripOptionalQuotes(`'hello'`); got != "hello" {
		t.Fatalf("StripOptionalQuotes() = %q, want %q", got, "hello")
	}
	if got := StripOptionalQuotes("plain"); got != "plain" {
		t.Fatalf("StripOptionalQuotes() = %q, want %q", got, "plain")
	}
}

func TestFirstNonEmptyLine(t *testing.T) {
	t.Parallel()

	content := "\n  \n  first line \nsecond line"
	if got := FirstNonEmptyLine(content); got != "first line" {
		t.Fatalf("FirstNonEmptyLine() = %q, want %q", got, "first line")
	}
}
