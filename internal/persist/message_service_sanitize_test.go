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

func TestReplayMessages_SanitizeKeepsStructuredAssistantContent(t *testing.T) {
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
	assistant := libagent.NewAssistantMessage("structured reply", "", []libagent.ToolCallItem{{
		ID:    "call-1",
		Name:  "read",
		Input: `{"path":"README.md"}`,
	}}, libagent.UnixMilliToTime(1))
	assistant.Completed = true
	assistant.Meta = libagent.MessageMeta{ID: "a1", SessionID: sid, CreatedAt: 1, UpdatedAt: 1}
	assistantWM := messageToWalMsg(assistant)
	write(walEntry{
		Typ: entryMsgCreate,
		Msg: &assistantWM,
	})
	write(walEntry{
		Typ: entryMsgCreate,
		Msg: &walMessage{Kind: "tool_result", ToolResult: &libagent.ToolResultMessage{
			Role:       "toolResult",
			ToolCallID: "call-1",
			ToolName:   "read",
			Content:    "ok",
			Timestamp:  libagent.UnixMilliToTime(1),
			Meta:       libagent.MessageMeta{ID: "t1", SessionID: sid, CreatedAt: 1, UpdatedAt: 1},
		}},
	})

	if err := f.Close(); err != nil {
		t.Fatalf("close wal: %v", err)
	}

	got, err := st.msgSvc.List(context.Background(), sid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2", len(got))
	}

	am, ok := got[0].(*libagent.AssistantMessage)
	if !ok {
		t.Fatalf("message[0] type=%T want *libagent.AssistantMessage", got[0])
	}
	if am.Text != "structured reply" {
		t.Fatalf("assistant text=%q want %q", am.Text, "structured reply")
	}
	if len(am.ToolCalls) != 1 {
		t.Fatalf("assistant tool calls=%d want 1", len(am.ToolCalls))
	}
	if am.ToolCalls[0].ID != "call-1" || am.ToolCalls[0].Name != "read" {
		t.Fatalf("assistant tool call=%+v", am.ToolCalls[0])
	}
	if _, ok := got[1].(*libagent.ToolResultMessage); !ok {
		t.Fatalf("message[1] type=%T want *libagent.ToolResultMessage", got[1])
	}
}
