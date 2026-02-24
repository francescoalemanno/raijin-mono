package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestTruncateHeadByLines(t *testing.T) {
	t.Parallel()

	input := "l1\nl2\nl3\nl4"
	got := TruncateHead(input, TruncationOptions{MaxLines: 2, MaxBytes: 1 << 20})

	if !got.Truncated {
		t.Fatalf("expected truncation")
	}
	if got.TruncatedBy != "lines" {
		t.Fatalf("truncatedBy = %q, want lines", got.TruncatedBy)
	}
	if got.Content != "l1\nl2" {
		t.Fatalf("content = %q, want first lines", got.Content)
	}
}

func TestTruncateHeadByBytesKeepsWholeLines(t *testing.T) {
	t.Parallel()

	input := "aaaa\nbbbb\ncccc"
	got := TruncateHead(input, TruncationOptions{MaxLines: 100, MaxBytes: 9})

	if !got.Truncated {
		t.Fatalf("expected truncation")
	}
	if got.TruncatedBy != "bytes" {
		t.Fatalf("truncatedBy = %q, want bytes", got.TruncatedBy)
	}
	if got.Content != "aaaa\nbbbb" {
		t.Fatalf("content = %q, want complete first two lines", got.Content)
	}
}

func TestTruncateHeadFirstLineExceedsLimit(t *testing.T) {
	t.Parallel()

	got := TruncateHead("0123456789", TruncationOptions{MaxLines: 10, MaxBytes: 5})

	if !got.FirstLineExceedsLimit {
		t.Fatalf("expected firstLineExceedsLimit")
	}
	if got.Content != "" {
		t.Fatalf("content = %q, want empty", got.Content)
	}
}

func TestTruncateTailByLines(t *testing.T) {
	t.Parallel()

	input := "l1\nl2\nl3\nl4"
	got := TruncateTail(input, TruncationOptions{MaxLines: 2, MaxBytes: 1 << 20})

	if !got.Truncated {
		t.Fatalf("expected truncation")
	}
	if got.TruncatedBy != "lines" {
		t.Fatalf("truncatedBy = %q, want lines", got.TruncatedBy)
	}
	if got.Content != "l3\nl4" {
		t.Fatalf("content = %q, want last lines", got.Content)
	}
}

func TestTruncateTailCanReturnPartialLine(t *testing.T) {
	t.Parallel()

	got := TruncateTail("abcdefghij", TruncationOptions{MaxLines: 10, MaxBytes: 4})

	if !got.Truncated {
		t.Fatalf("expected truncation")
	}
	if !got.LastLinePartial {
		t.Fatalf("expected lastLinePartial")
	}
	if got.Content != "ghij" {
		t.Fatalf("content = %q, want tail bytes", got.Content)
	}
}

func TestTruncateOutputUsesHead(t *testing.T) {
	t.Parallel()

	content := makeNumberedLines(DefaultMaxLines + 50)
	got := truncateOutput(content)

	if !strings.Contains(got, "line-0001") {
		t.Fatalf("expected head of content to be preserved")
	}
	if strings.Contains(got, fmt.Sprintf("line-%04d", DefaultMaxLines+50)) {
		t.Fatalf("did not expect tail line in head truncation")
	}
	if !strings.Contains(got, "[Output truncated:") {
		t.Fatalf("expected truncation notice")
	}
}

func makeNumberedLines(n int) string {
	lines := make([]string, n)
	for i := 1; i <= n; i++ {
		lines[i-1] = fmt.Sprintf("line-%04d", i)
	}
	return strings.Join(lines, "\n")
}
