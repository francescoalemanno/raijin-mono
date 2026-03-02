package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaySessionMeta_UsesOnlySessionEntries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.wal")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create wal: %v", err)
	}

	writeEntry := func(entry walEntry) {
		t.Helper()
		entry.V = walVersion
		line, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}

	writeEntry(walEntry{
		Typ: entrySessionCreate,
		Session: &walSession{
			ID:        "s1",
			Title:     "Untitled Session",
			CreatedAt: 1,
			UpdatedAt: 1,
		},
	})

	// Large msg payload should be skipped during session metadata replay.
	writeEntry(walEntry{
		Typ: entryMsgUpdate,
		Msg: &walMessage{
			ID:        "m1",
			Role:      "assistant",
			SessionID: "s1",
			Parts: []walPart{
				{
					T:    walPartText,
					Data: json.RawMessage(`{"text":"` + strings.Repeat("x", 128*1024) + `"}`),
				},
			},
			CreatedAt: 2,
			UpdatedAt: 2,
		},
	})

	writeEntry(walEntry{
		Typ: entrySessionTitle,
		Session: &walSession{
			ID:        "s1",
			Title:     "Final title",
			CreatedAt: 1,
			UpdatedAt: 3,
		},
	})

	if err := f.Close(); err != nil {
		t.Fatalf("close wal: %v", err)
	}

	sess, err := replaySessionMeta(path)
	if err != nil {
		t.Fatalf("replaySessionMeta: %v", err)
	}
	if sess.ID != "s1" {
		t.Fatalf("session id = %q, want %q", sess.ID, "s1")
	}
	if sess.Title != "Final title" {
		t.Fatalf("session title = %q, want %q", sess.Title, "Final title")
	}
}

func TestReplaySessionMeta_NoSessionEntries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.wal")
	if err := os.WriteFile(path, []byte(`{"v":1,"typ":"msg.create","sid":"s1"}`+"\n"), filePerm); err != nil {
		t.Fatalf("write wal: %v", err)
	}

	sess, err := replaySessionMeta(path)
	if err != nil {
		t.Fatalf("replaySessionMeta: %v", err)
	}
	if sess.ID != "" {
		t.Fatalf("session id = %q, want empty", sess.ID)
	}
}
