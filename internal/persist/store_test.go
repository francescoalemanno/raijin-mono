package persist

import (
	"context"
	"strings"
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

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

func TestCreateAndListMessages(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	u1 := &libagent.UserMessage{Role: "user", Content: "hello"}
	a1 := &libagent.AssistantMessage{Role: "assistant", Text: "world", Completed: true}
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

func TestNavigate_UserMessageRestoresEditorText(t *testing.T) {
	t.Parallel()

	st, sess := newEphemeralTestStore(t)
	ctx := context.Background()
	ms := st.Messages()

	u1 := &libagent.UserMessage{Role: "user", Content: "first question"}
	a1 := &libagent.AssistantMessage{Role: "assistant", Text: "answer", Completed: true}
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
	a1 := &libagent.AssistantMessage{Role: "assistant", Text: "a", Completed: true}
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
	a1 := &libagent.AssistantMessage{Role: "assistant", Text: "ok", Completed: true}
	u2 := &libagent.UserMessage{Role: "user", Content: "branch A"}
	a2 := &libagent.AssistantMessage{Role: "assistant", Text: "A answer", Completed: true}

	m1, _ := ms.Create(ctx, sess.ID, u1)
	ms.Create(ctx, sess.ID, a1) //nolint
	ms.Create(ctx, sess.ID, u2) //nolint
	ms.Create(ctx, sess.ID, a2) //nolint

	// Navigate back to u1 (user message → leaf = parent of u1 = "")
	_, _ = st.Navigate(libagent.MessageID(m1))

	// Add branch B
	uB := &libagent.UserMessage{Role: "user", Content: "branch B"}
	aB := &libagent.AssistantMessage{Role: "assistant", Text: "B answer", Completed: true}
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

	ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "persisted"})                   //nolint
	ms.Create(ctx, sess.ID, &libagent.AssistantMessage{Role: "assistant", Text: "yes", Completed: true}) //nolint

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
	st2.current = sess.ID
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

	u1, _ := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "first"})                       //nolint:errcheck
	a1, _ := ms.Create(ctx, sess.ID, &libagent.AssistantMessage{Role: "assistant", Text: "one", Completed: true}) //nolint:errcheck
	u2, _ := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "second"})                      //nolint:errcheck
	a2, _ := ms.Create(ctx, sess.ID, &libagent.AssistantMessage{Role: "assistant", Text: "two", Completed: true}) //nolint:errcheck

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
	if got[2].(*libagent.AssistantMessage).Text != "two" {
		t.Fatalf("got[2] text = %q, want two", got[2].(*libagent.AssistantMessage).Text)
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

	_, _ = ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "first"})                                 //nolint:errcheck
	u2, _ := ms.Create(ctx, sess.ID, &libagent.UserMessage{Role: "user", Content: "second"})                              //nolint:errcheck
	_, _ = ms.Create(ctx, sess.ID, &libagent.AssistantMessage{Role: "assistant", Text: "second-answer", Completed: true}) //nolint:errcheck

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
	st2.current = sess.ID
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
