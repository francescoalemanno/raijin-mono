package persist

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
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
		Msg: &walMessage{Kind: "user", User: &libagent.UserMessage{
			Role:      "user",
			Content:   "hello",
			Timestamp: libagent.UnixMilliToTime(1),
			Meta:      libagent.MessageMeta{ID: "u1", SessionID: sid, CreatedAt: 1, UpdatedAt: 1},
		}},
	})
	write(walEntry{
		Typ: entryMsgCreate,
		Msg: &walMessage{Kind: "assistant", Assistant: &libagent.AssistantMessage{
			Role:      "assistant",
			ToolCalls: []libagent.ToolCallItem{{ID: "call-1", Name: "read", Input: "{}"}},
			Completed: true,
			Timestamp: libagent.UnixMilliToTime(1),
			Meta:      libagent.MessageMeta{ID: "a1", SessionID: sid, CreatedAt: 1, UpdatedAt: 1},
		}},
	})
	write(walEntry{
		Typ: entryMsgCreate,
		Msg: &walMessage{Kind: "tool_result", ToolResult: &libagent.ToolResultMessage{
			Role:       "toolResult",
			ToolCallID: "orphan",
			ToolName:   "read",
			Content:    "x",
			Timestamp:  libagent.UnixMilliToTime(1),
			Meta:       libagent.MessageMeta{ID: "t-orphan", SessionID: sid, CreatedAt: 1, UpdatedAt: 1},
		}},
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
	if _, ok := got[0].(*libagent.UserMessage); !ok {
		t.Fatalf("remaining message type=%T want *libagent.UserMessage", got[0])
	}
}
