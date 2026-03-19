package persist

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func testAssistant(text string, calls []libagent.ToolCallItem) *libagent.AssistantMessage {
	am := libagent.NewAssistantMessage(text, "", calls, libagent.UnixMilliToTime(1))
	am.Completed = true
	return am
}

func newEphemeralTestStore(t *testing.T) (*Store, Session) {
	t.Helper()
	st := &Store{
		dir:      t.TempDir(),
		sessions: make(map[string]Session),
		nodes:    make(map[string]*treeNode),
	}
	sess, err := st.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}
	return st, sess
}

func reloadTestStore(t *testing.T, src *Store, sessionID string) *Store {
	t.Helper()
	st := &Store{
		dir:      src.dir,
		sessions: make(map[string]Session),
		nodes:    make(map[string]*treeNode),
	}
	if err := st.loadSessionIndex(); err != nil {
		t.Fatalf("loadSessionIndex: %v", err)
	}
	st.mu.Lock()
	st.loaded = sessionID
	st.mu.Unlock()
	if err := st.loadSessionTree(sessionID); err != nil {
		t.Fatalf("loadSessionTree: %v", err)
	}
	return st
}

func TestCreateAndListMessages(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	u1 := &libagent.UserMessage{Role: "user", Content: "hello"}
	a1 := testAssistant("world", nil)
	u2 := &libagent.UserMessage{Role: "user", Content: "second"}

	if _, err := ms.Create(ctx, sess.ID, u1); err != nil {
		t.Fatalf("Create u1: %v", err)
	}
	if _, err := ms.Create(ctx, sess.ID, a1); err != nil {
		t.Fatalf("Create a1: %v", err)
	}
	if _, err := ms.Create(ctx, sess.ID, u2); err != nil {
		t.Fatalf("Create u2: %v", err)
	}

	msgs, err := ms.List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if msgs[0].(*libagent.UserMessage).Content != "hello" {
		t.Fatalf("msgs[0] content = %q, want hello", msgs[0].(*libagent.UserMessage).Content)
	}
}

func TestEnsureSessionPersistedWithoutMessages(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	if err := st.EnsureSessionPersisted(sess.ID); err != nil {
		t.Fatalf("EnsureSessionPersisted: %v", err)
	}

	st2 := &Store{
		dir:      st.dir,
		sessions: make(map[string]Session),
		nodes:    make(map[string]*treeNode),
	}
	if err := st2.loadSessionIndex(); err != nil {
		t.Fatalf("loadSessionIndex: %v", err)
	}
	if _, err := st2.GetSession(sess.ID); err != nil {
		t.Fatalf("GetSession after persist: %v", err)
	}
}

func TestNavigate_UserMessageRestoresEditorText(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	u1 := &libagent.UserMessage{Role: "user", Content: "first question"}
	a1 := testAssistant("answer", nil)
	u2 := &libagent.UserMessage{Role: "user", Content: "second question"}

	m1, _ := ms.Create(ctx, sess.ID, u1)
	ms.Create(ctx, sess.ID, a1) //nolint
	ms.Create(ctx, sess.ID, u2) //nolint

	msgID := libagent.MessageID(m1)
	editorText, err := st.Navigate(msgID)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if editorText != "first question" {
		t.Fatalf("editorText = %q, want %q", editorText, "first question")
	}

	// After navigation to u1 (parent = ""), List should return nothing.
	msgs, _ := ms.List(ctx, sess.ID)
	if len(msgs) != 0 {
		t.Fatalf("after navigate to root-parent, got %d msgs, want 0", len(msgs))
	}
}

func TestNavigate_NonUserMessageSetsLeaf(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	u1 := &libagent.UserMessage{Role: "user", Content: "q"}
	a1 := testAssistant("a", nil)
	u2 := &libagent.UserMessage{Role: "user", Content: "q2"}

	ms.Create(ctx, sess.ID, u1) //nolint
	m2, _ := ms.Create(ctx, sess.ID, a1)
	ms.Create(ctx, sess.ID, u2) //nolint

	editorText, err := st.Navigate(libagent.MessageID(m2))
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if editorText != "" {
		t.Fatalf("editorText = %q, want empty for non-user message", editorText)
	}

	msgs, _ := ms.List(ctx, sess.ID)
	if len(msgs) != 2 {
		t.Fatalf("got %d msgs, want 2 (u1 + a1)", len(msgs))
	}
}

func TestBranchingNavigation(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	// Build: u1 -> a1 -> u2 -> a2
	u1 := &libagent.UserMessage{Role: "user", Content: "root"}
	a1 := testAssistant("ok", nil)
	u2 := &libagent.UserMessage{Role: "user", Content: "branch A"}
	a2 := testAssistant("A answer", nil)

	m1, _ := ms.Create(ctx, sess.ID, u1)
	ms.Create(ctx, sess.ID, a1) //nolint
	ms.Create(ctx, sess.ID, u2) //nolint
	ms.Create(ctx, sess.ID, a2) //nolint

	// Navigate back to u1 (user message → leaf = parent of u1 = "")
	_, _ = st.Navigate(libagent.MessageID(m1))

	// Add branch B
	uB := &libagent.UserMessage{Role: "user", Content: "branch B"}
	aB := testAssistant("B answer", nil)
	ms.Create(ctx, sess.ID, uB) //nolint
	ms.Create(ctx, sess.ID, aB) //nolint

	msgs, _ := ms.List(ctx, sess.ID)
	if len(msgs) != 2 {
		t.Fatalf("branch B: got %d msgs, want 2", len(msgs))
	}
	if msgs[0].(*libagent.UserMessage).Content != "branch B" {
		t.Fatalf("branch B msg[0] = %q, want 'branch B'", msgs[0].(*libagent.UserMessage).Content)
	}
}

func TestReplayJournal_RoundTrip(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "persisted"}) //nolint
	ms.Create(ctx, sess.ID, testAssistant("yes", nil))                                 //nolint

	st.mu.Lock()
	leafBefore := st.leafID
	st.mu.Unlock()

	// Reload from disk into a new store.
	st2 := &Store{
		dir:      st.dir,
		sessions: make(map[string]Session),
		nodes:    make(map[string]*treeNode),
	}
	_ = st2.loadSessionIndex()
	st2.mu.Lock()
	st2.loaded = sess.ID
	st2.mu.Unlock()
	_ = st2.loadSessionTree(sess.ID)

	if st2.leafID != leafBefore {
		t.Fatalf("leafID after reload = %q, want %q", st2.leafID, leafBefore)
	}
	msgs, _ := st2.Messages().List(ctx, sess.ID)
	if len(msgs) != 2 {
		t.Fatalf("after reload: got %d msgs, want 2", len(msgs))
	}
}

func TestReplayJournal_RoundTrip_PreservesReasoningProviderMetadata(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	_, err := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "persist"})
	if err != nil {
		t.Fatalf("Create user: %v", err)
	}
	st.mu.Lock()
	parentID := st.leafID
	st.mu.Unlock()
	if parentID == "" {
		t.Fatal("expected non-empty leaf after creating user message")
	}

	seedAssistant := libagent.NewAssistantMessage("answer", "summary from raw", nil, libagent.UnixMilliToTime(1))
	seedAssistant.Completed = true
	seedMsg := messageToJMsg(seedAssistant)
	if seedMsg.Assistant == nil {
		t.Fatal("messageToJMsg returned nil assistant payload")
	}
	if len(seedMsg.AssistantContent) == 0 {
		t.Fatal("messageToJMsg returned empty assistant_content")
	}
	// Clear flattened fields so replay must recover from raw assistant_content.
	seedMsg.Assistant.Text = ""
	seedMsg.Assistant.Reasoning = ""
	seedMsg.Assistant.ToolCalls = nil
	seedMsg.Assistant.Meta.ID = "assistant-raw"
	seedMsg.Assistant.Meta.SessionID = sess.ID
	var rawContent json.RawMessage
	rawContent = append(rawContent, seedMsg.AssistantContent...)

	assistantID := "assistant-raw"
	entry := jEntry{
		Typ:      jTypeMsg,
		ID:       assistantID,
		ParentID: parentID,
		Msg: &jMsg{
			Kind:             "assistant",
			Assistant:        seedMsg.Assistant,
			AssistantContent: rawContent,
		},
	}
	if err := st.appendEntry(sess.ID, entry); err != nil {
		t.Fatalf("appendEntry: %v", err)
	}

	reloaded := reloadTestStore(t, st, sess.ID)
	gotMsgs, err := reloaded.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List after reload: %v", err)
	}
	if len(gotMsgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(gotMsgs))
	}

	gotAssistant, ok := gotMsgs[1].(*libagent.AssistantMessage)
	if !ok {
		t.Fatalf("message[1] type=%T want *AssistantMessage", gotMsgs[1])
	}
	if got := libagent.AssistantReasoning(gotAssistant); got != "summary from raw" {
		t.Fatalf("assistant reasoning=%q want %q", got, "summary from raw")
	}
	if got := libagent.AssistantText(gotAssistant); got != "answer" {
		t.Fatalf("assistant text=%q want %q", got, "answer")
	}
}

func TestNavigate_ReplayRoundTrip_PersistsAssistantSelection(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	_, _ = ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "first"})  //nolint:errcheck
	a1, _ := ms.Create(ctx, sess.ID, testAssistant("answer one", nil))                     //nolint:errcheck
	_, _ = ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "second"}) //nolint:errcheck

	if _, err := st.Navigate(libagent.MessageID(a1)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	st2 := reloadTestStore(t, st, sess.ID)
	if st2.leafID != libagent.MessageID(a1) {
		t.Fatalf("leafID after reload = %q, want %q", st2.leafID, libagent.MessageID(a1))
	}

	msgs, err := st2.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List after reload: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("after reload: got %d msgs, want 2", len(msgs))
	}
	if got := libagent.AssistantText(msgs[1].(*libagent.AssistantMessage)); got != "answer one" {
		t.Fatalf("assistant text after reload = %q, want %q", got, "answer one")
	}
}

func TestTitleFromFirstUserMessage(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a ", 50)
	title := TitleFromFirstUserMessage(long)
	runes := []rune(title)
	if len(runes) > titleMaxLen {
		t.Fatalf("title len %d > %d", len(runes), titleMaxLen)
	}
	if !strings.HasSuffix(title, "…") {
		t.Fatalf("long title should end with ellipsis, got %q", title)
	}
}

func TestAppendCompaction_ListShowsSummaryAndKeptMessages(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	u1, _ := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "first"})  //nolint:errcheck
	a1, _ := ms.Create(ctx, sess.ID, testAssistant("one", nil))                              //nolint:errcheck
	u2, _ := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "second"}) //nolint:errcheck
	a2, _ := ms.Create(ctx, sess.ID, testAssistant("two", nil))                              //nolint:errcheck

	if err := st.AppendCompaction("summary checkpoint", libagent.MessageID(u2), 1234); err != nil {
		t.Fatalf("AppendCompaction: %v", err)
	}

	got, err := ms.List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List after compaction: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3 (summary + u2 + a2)", len(got))
	}
	summary, ok := got[0].(*libagent.UserMessage)
	if !ok {
		t.Fatalf("got[0] type = %T, want *libagent.UserMessage", got[0])
	}
	if !strings.Contains(summary.Content, "summary checkpoint") {
		t.Fatalf("summary content missing checkpoint text: %q", summary.Content)
	}
	if got[1].(*libagent.UserMessage).Content != "second" {
		t.Fatalf("got[1] content = %q, want second", got[1].(*libagent.UserMessage).Content)
	}
	if libagent.AssistantText(got[2].(*libagent.AssistantMessage)) != "two" {
		t.Fatalf("got[2] text = %q, want two", libagent.AssistantText(got[2].(*libagent.AssistantMessage)))
	}

	// Continue conversation from compaction node.
	if _, err := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "third"}); err != nil {
		t.Fatalf("Create after compaction: %v", err)
	}
	got, err = ms.List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List after new message: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d messages, want 4 after appending new message", len(got))
	}
	if got[3].(*libagent.UserMessage).Content != "third" {
		t.Fatalf("last message = %q, want third", got[3].(*libagent.UserMessage).Content)
	}

	// Keep variables used to avoid lint complaining if future assertions change.
	_ = libagent.MessageID(u1)
	_ = libagent.MessageID(a1)
	_ = libagent.MessageID(a2)
}

func TestAppendCompaction_ReplayRoundTrip(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	_, _ = ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "first"})    //nolint:errcheck
	u2, _ := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "second"}) //nolint:errcheck
	_, _ = ms.Create(ctx, sess.ID, testAssistant("second-answer", nil))                      //nolint:errcheck

	if err := st.AppendCompaction("persist me", libagent.MessageID(u2), 777); err != nil {
		t.Fatalf("AppendCompaction: %v", err)
	}

	st2 := &Store{
		dir:      st.dir,
		sessions: make(map[string]Session),
		nodes:    make(map[string]*treeNode),
	}
	if err := st2.loadSessionIndex(); err != nil {
		t.Fatalf("loadSessionIndex: %v", err)
	}
	st2.mu.Lock()
	st2.loaded = sess.ID
	st2.mu.Unlock()
	if err := st2.loadSessionTree(sess.ID); err != nil {
		t.Fatalf("loadSessionTree: %v", err)
	}

	replayed, err := st2.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("replayed List: %v", err)
	}
	if len(replayed) != 3 {
		t.Fatalf("replayed messages = %d, want 3", len(replayed))
	}
	if !strings.Contains(replayed[0].(*libagent.UserMessage).Content, "persist me") {
		t.Fatalf("replayed summary content mismatch: %q", replayed[0].(*libagent.UserMessage).Content)
	}
}

func TestGetTree_HidesUnsafeAssistantSelectionThatWouldSplitToolCoupling(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	if _, err := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "start"}); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	a1, err := ms.Create(ctx, sess.ID, testAssistant("running tool", []libagent.ToolCallItem{{
		ID:    "call-1",
		Name:  "read",
		Input: `{"path":"file.txt"}`,
	}}))
	if err != nil {
		t.Fatalf("Create assistant: %v", err)
	}
	tr1, err := ms.Create(ctx, sess.ID, &libagent.ToolResultMessage{
		Role:       "toolResult",
		ToolCallID: "call-1",
		ToolName:   "read",
		Content:    "ok",
	})
	if err != nil {
		t.Fatalf("Create tool result: %v", err)
	}
	if _, err := ms.Create(ctx, sess.ID, testAssistant("done", nil)); err != nil {
		t.Fatalf("Create assistant 2: %v", err)
	}

	tree := st.GetTree()
	if treeContainsID(tree, libagent.MessageID(a1)) {
		t.Fatalf("unsafe assistant entry should be hidden from /tree")
	}
	if !treeContainsID(tree, libagent.MessageID(tr1)) {
		t.Fatalf("tool result entry should remain visible")
	}
}

func TestGetTree_AllVisibleEntriesNavigateToBijectiveToolCoupling(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	if _, err := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "start"}); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	if _, err := ms.Create(ctx, sess.ID, testAssistant("running tool", []libagent.ToolCallItem{{
		ID:    "call-1",
		Name:  "read",
		Input: `{"path":"file.txt"}`,
	}})); err != nil {
		t.Fatalf("Create assistant: %v", err)
	}
	if _, err := ms.Create(ctx, sess.ID, &libagent.ToolResultMessage{
		Role:       "toolResult",
		ToolCallID: "call-1",
		ToolName:   "read",
		Content:    "ok",
	}); err != nil {
		t.Fatalf("Create tool result: %v", err)
	}
	if _, err := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "next"}); err != nil {
		t.Fatalf("Create user 2: %v", err)
	}
	if _, err := ms.Create(ctx, sess.ID, testAssistant("done", nil)); err != nil {
		t.Fatalf("Create assistant 2: %v", err)
	}

	tree := st.GetTree()
	if len(tree) == 0 {
		t.Fatal("expected non-empty tree")
	}

	for _, entry := range tree {
		if _, err := st.Navigate(entry.ID); err != nil {
			t.Fatalf("Navigate(%s): %v", entry.ID, err)
		}
		msgs, err := ms.List(ctx, sess.ID)
		if err != nil {
			t.Fatalf("List after navigate: %v", err)
		}
		if !libagent.HasBijectiveToolCoupling(msgs) {
			t.Fatalf("visible tree entry %s navigates to non-bijective tool history", entry.ID)
		}
	}
}

func TestNavigate_ReplayRoundTrip_UserSelectionRestoresParentLeaf(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	u1, _ := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "first question"}) //nolint:errcheck
	_, _ = ms.Create(ctx, sess.ID, testAssistant("answer", nil))                                     //nolint:errcheck
	_, _ = ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "second question"})  //nolint:errcheck

	editorText, err := st.Navigate(libagent.MessageID(u1))
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if editorText != "first question" {
		t.Fatalf("editorText = %q, want %q", editorText, "first question")
	}

	st2 := reloadTestStore(t, st, sess.ID)
	if st2.leafID != "" {
		t.Fatalf("leafID after reload = %q, want root", st2.leafID)
	}

	msgs, err := st2.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List after reload: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("after reload: got %d msgs, want 0", len(msgs))
	}
}

func TestNavigate_ReplayRoundTrip_NewMessagesOverrideStoredSelection(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	_, _ = ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "root"}) //nolint:errcheck
	a1, _ := ms.Create(ctx, sess.ID, testAssistant("branch point", nil))                 //nolint:errcheck
	_, _ = ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "old"})  //nolint:errcheck
	_, _ = ms.Create(ctx, sess.ID, testAssistant("old answer", nil))                     //nolint:errcheck

	if _, err := st.Navigate(libagent.MessageID(a1)); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	_, _ = ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "new"}) //nolint:errcheck
	a3, _ := ms.Create(ctx, sess.ID, testAssistant("new answer", nil))                  //nolint:errcheck

	st2 := reloadTestStore(t, st, sess.ID)
	if st2.leafID != libagent.MessageID(a3) {
		t.Fatalf("leafID after reload = %q, want %q", st2.leafID, libagent.MessageID(a3))
	}

	msgs, err := st2.Messages().List(ctx, sess.ID)
	if err != nil {
		t.Fatalf("List after reload: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("after reload: got %d msgs, want 4", len(msgs))
	}
	if got := msgs[2].(*libagent.UserMessage).Content; got != "new" {
		t.Fatalf("new branch user content = %q, want %q", got, "new")
	}
	if got := libagent.AssistantText(msgs[3].(*libagent.AssistantMessage)); got != "new answer" {
		t.Fatalf("new branch assistant text = %q, want %q", got, "new answer")
	}
}

func treeContainsID(entries []TreeEntry, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func TestRemoveSession_AllowsRemovingLoadedSession(t *testing.T) {
	t.Parallel()

	st, s1 := newEphemeralTestStore(t)
	if err := st.EnsureSessionPersisted(s1.ID); err != nil {
		t.Fatalf("EnsureSessionPersisted(s1): %v", err)
	}
	s2, err := st.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral(s2): %v", err)
	}
	if err := st.EnsureSessionPersisted(s2.ID); err != nil {
		t.Fatalf("EnsureSessionPersisted(s2): %v", err)
	}
	if err := st.OpenSession(s1.ID); err != nil {
		t.Fatalf("OpenSession(s1): %v", err)
	}

	if err := st.RemoveSession(s1.ID); err != nil {
		t.Fatalf("RemoveSession(loaded): %v", err)
	}

	if _, err := st.GetSession(s2.ID); err != nil {
		t.Fatalf("expected remaining session %q to be present: %v", s2.ID, err)
	}
	if _, err := st.GetSession(s1.ID); err == nil {
		t.Fatalf("expected removed session %q to be absent", s1.ID)
	}
}

func TestRemoveSession_RemovingLastLoadedSessionLeavesNoSummaries(t *testing.T) {
	t.Parallel()

	st, s1 := newEphemeralTestStore(t)
	if err := st.EnsureSessionPersisted(s1.ID); err != nil {
		t.Fatalf("EnsureSessionPersisted(s1): %v", err)
	}

	if err := st.RemoveSession(s1.ID); err != nil {
		t.Fatalf("RemoveSession(loaded): %v", err)
	}
	if got := len(st.ListSessionSummaries()); got != 0 {
		t.Fatalf("ListSessionSummaries len = %d, want 0", got)
	}
}

func TestBindingRoundTripSupportsEphemeralSessionReattach(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	st, err := OpenStore()
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	sess, err := st.CreateEphemeral()
	if err != nil {
		t.Fatalf("CreateEphemeral: %v", err)
	}
	if err := st.SaveBinding(Binding{
		Key:              "bind-test",
		SessionID:        sess.ID,
		OwnerPID:         1234,
		Ephemeral:        true,
		SessionCreatedAt: sess.CreatedAt,
		SessionUpdatedAt: sess.UpdatedAt,
	}); err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	reopened, err := OpenStore()
	if err != nil {
		t.Fatalf("OpenStore(reopened): %v", err)
	}
	binding, err := reopened.LoadBinding("bind-test")
	if err != nil {
		t.Fatalf("LoadBinding: %v", err)
	}
	reattached, err := reopened.CreateEphemeralWithID(binding.SessionID, binding.SessionCreatedAt)
	if err != nil {
		t.Fatalf("CreateEphemeralWithID: %v", err)
	}
	if reattached.ID != sess.ID {
		t.Fatalf("reattached session id = %q, want %q", reattached.ID, sess.ID)
	}
	msgs, err := reopened.Messages().List(context.Background(), reattached.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected empty ephemeral history after reattach, got %d messages", len(msgs))
	}
}
