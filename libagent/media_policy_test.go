package libagent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLimitImageAttachments_KeepsNewestTwenty(t *testing.T) {
	t.Parallel()

	msgs := make([]Message, 0, 25)
	for i := range 25 {
		msgs = append(msgs, &UserMessage{
			Role:      "user",
			Content:   fmt.Sprintf("message-%02d", i),
			Timestamp: time.Unix(int64(i), 0),
			Files: []FilePart{{
				Filename:  fmt.Sprintf("img-%02d.png", i),
				MediaType: "image/png",
				Data:      []byte{byte(i)},
			}},
		})
	}

	got := limitImageAttachments(msgs, 20)
	if len(got) != len(msgs) {
		t.Fatalf("len(got)=%d want %d", len(got), len(msgs))
	}

	for i := 0; i < 5; i++ {
		um := got[i].(*UserMessage)
		if len(um.Files) != 0 {
			t.Fatalf("message %d files=%d want 0", i, len(um.Files))
		}
		if !strings.Contains(um.Content, excisedAttachmentText) {
			t.Fatalf("message %d content missing excision placeholder: %q", i, um.Content)
		}
	}

	for i := 5; i < len(got); i++ {
		um := got[i].(*UserMessage)
		if len(um.Files) != 1 {
			t.Fatalf("message %d files=%d want 1", i, len(um.Files))
		}
		if strings.Contains(um.Content, excisedAttachmentText) {
			t.Fatalf("message %d content unexpectedly contains excision placeholder", i)
		}
	}
}

func TestLimitImageAttachments_PreservesNonImageFiles(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&UserMessage{
			Role:      "user",
			Content:   "mixed attachments",
			Timestamp: time.Unix(1, 0),
			Files: []FilePart{
				{Filename: "old.png", MediaType: "image/png", Data: []byte("old")},
				{Filename: "notes.pdf", MediaType: "application/pdf", Data: []byte("pdf")},
			},
		},
	}

	got := limitImageAttachments(msgs, 0)
	um := got[0].(*UserMessage)
	if len(um.Files) != 1 {
		t.Fatalf("files=%d want 1", len(um.Files))
	}
	if um.Files[0].Filename != "notes.pdf" {
		t.Fatalf("kept file=%q want notes.pdf", um.Files[0].Filename)
	}
	if !strings.Contains(um.Content, excisedAttachmentText) {
		t.Fatalf("content missing excision placeholder: %q", um.Content)
	}
}

func TestLimitImageAttachments_RepeatsPlaceholderForEachExcisedImage(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&UserMessage{
			Role:      "user",
			Timestamp: time.Unix(1, 0),
			Files: []FilePart{
				{Filename: "a.png", MediaType: "image/png", Data: []byte("a")},
				{Filename: "b.png", MediaType: "image/png", Data: []byte("b")},
				{Filename: "c.png", MediaType: "image/png", Data: []byte("c")},
			},
		},
	}

	got := limitImageAttachments(msgs, 0)
	um := got[0].(*UserMessage)
	if len(um.Files) != 0 {
		t.Fatalf("files=%d want 0", len(um.Files))
	}
	want := strings.Join([]string{
		excisedAttachmentText,
		excisedAttachmentText,
		excisedAttachmentText,
	}, "\n")
	if um.Content != want {
		t.Fatalf("content=%q want %q", um.Content, want)
	}
}

func TestLimitImageAttachments_KeepsNewestImagesWithinMessage(t *testing.T) {
	t.Parallel()

	msgs := []Message{
		&UserMessage{
			Role:      "user",
			Content:   "batch",
			Timestamp: time.Unix(1, 0),
			Files: []FilePart{
				{Filename: "old.png", MediaType: "image/png", Data: []byte("old")},
				{Filename: "mid.png", MediaType: "image/png", Data: []byte("mid")},
				{Filename: "new.png", MediaType: "image/png", Data: []byte("new")},
			},
		},
	}

	got := limitImageAttachments(msgs, 2)
	um := got[0].(*UserMessage)
	if len(um.Files) != 2 {
		t.Fatalf("files=%d want 2", len(um.Files))
	}
	if um.Files[0].Filename != "mid.png" || um.Files[1].Filename != "new.png" {
		t.Fatalf("kept files=%v want [mid.png new.png]", []string{um.Files[0].Filename, um.Files[1].Filename})
	}
	if !strings.Contains(um.Content, excisedAttachmentText) {
		t.Fatalf("content missing excision placeholder: %q", um.Content)
	}
}

func TestRuntimeMediaTransform_StripsUnsupportedMediaAfterBudgeting(t *testing.T) {
	t.Parallel()

	transform := runtimeMediaTransform(MediaSupport{Known: true, Enabled: false}, DefaultMaxImages)
	msgs := []Message{
		&UserMessage{
			Role:      "user",
			Content:   "see image",
			Timestamp: time.Unix(1, 0),
			Files: []FilePart{{
				Filename:  "img.png",
				MediaType: "image/png",
				Data:      []byte("img"),
			}},
		},
	}

	got, err := transform(context.Background(), msgs)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	um := got[0].(*UserMessage)
	if len(um.Files) != 0 {
		t.Fatalf("files=%d want 0", len(um.Files))
	}
}

func TestRuntimeMediaTransform_UsesConfiguredImageBudget(t *testing.T) {
	t.Parallel()

	transform := runtimeMediaTransform(MediaSupport{Known: true, Enabled: true}, 1)
	msgs := []Message{
		&UserMessage{
			Role:      "user",
			Content:   "older",
			Timestamp: time.Unix(1, 0),
			Files: []FilePart{{
				Filename:  "old.png",
				MediaType: "image/png",
				Data:      []byte("old"),
			}},
		},
		&UserMessage{
			Role:      "user",
			Content:   "newer",
			Timestamp: time.Unix(2, 0),
			Files: []FilePart{{
				Filename:  "new.png",
				MediaType: "image/png",
				Data:      []byte("new"),
			}},
		},
	}

	got, err := transform(context.Background(), msgs)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if len(got[0].(*UserMessage).Files) != 0 {
		t.Fatalf("older message kept image unexpectedly")
	}
	if len(got[1].(*UserMessage).Files) != 1 {
		t.Fatalf("newer message files=%d want 1", len(got[1].(*UserMessage).Files))
	}
}

func TestRuntimeMediaTransform_StripsUnsupportedMediaCaseInsensitively(t *testing.T) {
	t.Parallel()

	transform := runtimeMediaTransform(MediaSupport{Known: true, Enabled: false}, DefaultMaxImages)
	msgs := []Message{
		&UserMessage{
			Role:      "user",
			Content:   "see image",
			Timestamp: time.Unix(1, 0),
			Files: []FilePart{{
				Filename:  "img.png",
				MediaType: " Image/PNG ",
				Data:      []byte("img"),
			}},
		},
	}

	got, err := transform(context.Background(), msgs)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if len(got[0].(*UserMessage).Files) != 0 {
		t.Fatalf("files=%d want 0", len(got[0].(*UserMessage).Files))
	}
}
