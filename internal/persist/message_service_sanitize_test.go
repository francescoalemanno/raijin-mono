package persist

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/message"
)

func TestReplayMessages_SanitizeDropsOrphanToolArtifacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st := &Store{dir: dir}
	st.sessSvc = newSessionService(st)
	st.msgSvc = newMessageService(st)

	sid := "s1"
	path := filepath.Join(dir, sid+".wal")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create wal: %v", err)
	}

	write := func(entry walEntry) {
		t.Helper()
		entry.V = walVersion
		entry.SessionID = sid
		line, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}

	write(walEntry{
		Typ:     entrySessionCreate,
		Session: &walSession{ID: sid, CreatedAt: 1, UpdatedAt: 1},
	})
	write(walEntry{
		Typ: entryMsgCreate,
		Msg: &walMessage{
			ID:        "u1",
			Role:      message.User,
			SessionID: sid,
			Parts:     []walPart{{T: walPartText, Data: json.RawMessage(`{"text":"hello"}`)}},
			CreatedAt: 1,
			UpdatedAt: 1,
		},
	})
	write(walEntry{
		Typ: entryMsgCreate,
		Msg: &walMessage{
			ID:        "a1",
			Role:      message.Assistant,
			SessionID: sid,
			Parts: []walPart{
				{T: walPartToolCall, Data: json.RawMessage(`{"id":"call-1","name":"read","input":"{}","finished":true}`)},
				{T: walPartFinish, Data: json.RawMessage(`{"reason":"tool_use","time":1}`)},
			},
			CreatedAt: 1,
			UpdatedAt: 1,
		},
	})
	write(walEntry{
		Typ: entryMsgCreate,
		Msg: &walMessage{
			ID:        "t-orphan",
			Role:      message.Tool,
			SessionID: sid,
			Parts: []walPart{
				{T: walPartToolResult, Data: json.RawMessage(`{"tool_call_id":"orphan","name":"read","content":"x"}`)},
			},
			CreatedAt: 1,
			UpdatedAt: 1,
		},
	})

	if err := f.Close(); err != nil {
		t.Fatalf("close wal: %v", err)
	}

	got, err := st.msgSvc.List(context.Background(), sid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0].Role != message.User {
		t.Fatalf("remaining role=%s want user", got[0].Role)
	}
}
