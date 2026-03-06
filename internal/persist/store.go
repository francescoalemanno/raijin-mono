// Package persist provides append-only JSONL tree-based session persistence.
// Each session is stored as a single newline-delimited JSON file under
// ~/.config/raijin/sessions/<uuid>.jsonl. Entries form a tree via parentId;
// an in-memory leaf pointer tracks the current position in the tree.
// A state.json in the same directory tracks the most recently active session.
package persist

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/francescoalemanno/raijin-mono/internal/paths"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

const (
	journalVersion = 2
	dirPerm        = 0o755
	filePerm       = 0o644
	titleMaxLen    = 72 // runes
	compactionPref = "[Context checkpoint created by /compact]\n\n"
)

// jType identifies a journal entry.
type jType string

const (
	jTypeSession    jType = "session"    // file header
	jTypeMsg        jType = "msg"        // message created
	jTypeMsgUpdate  jType = "msg.update" // message updated (streaming)
	jTypeTitle      jType = "title"      // session title changed
	jTypeCompaction jType = "compaction" // compaction checkpoint
)

// jEntry is the NDJSON envelope written to the journal file.
type jEntry struct {
	V        int    `json:"v"`
	T        int64  `json:"t"`
	Typ      jType  `json:"typ"`
	SessID   string `json:"sid,omitempty"`
	ID       string `json:"id,omitempty"`
	ParentID string `json:"pid,omitempty"`

	// jTypeSession / jTypeTitle
	Title    string `json:"title,omitempty"`
	SessTime int64  `json:"sess_t,omitempty"` // CreatedAt for jTypeSession

	// jTypeMsg / jTypeMsgUpdate
	Msg *jMsg `json:"msg,omitempty"`

	// jTypeCompaction
	Summary      string `json:"summary,omitempty"`
	FirstKeptID  string `json:"first_kept_id,omitempty"`
	TokensBefore int64  `json:"tokens_before,omitempty"`
}

// jMsg is the serialisable form of a runtime libagent message.
type jMsg struct {
	Kind       string                      `json:"kind"`
	User       *libagent.UserMessage       `json:"user,omitempty"`
	Assistant  *libagent.AssistantMessage  `json:"assistant,omitempty"`
	ToolResult *libagent.ToolResultMessage `json:"tool_result,omitempty"`
}

// treeNode is one in-memory node in the session's message tree.
type treeNode struct {
	id           string
	parentID     string
	typ          jType
	msg          libagent.Message // nil for non-message entries
	summary      string           // set when typ == jTypeCompaction
	firstKeptID  string           // set when typ == jTypeCompaction
	tokensBefore int64            // set when typ == jTypeCompaction
	children     []string
}

// SessionSummary is the information exposed to the /sessions selector.
type SessionSummary struct {
	ID        string
	ShortID   string
	Title     string
	UpdatedAt int64
}

// TreeEntry is a node exposed to the /tree selector UI.
type TreeEntry struct {
	ID             string
	ParentID       string
	Msg            libagent.Message // nil if not a message node
	IsLeaf         bool             // true if this is the current leaf
	IsOnActivePath bool             // true if on the path from root to current leaf
	Depth          int              // branch-point depth for indentation
	ShowConnector  bool             // true when parent has multiple children
	IsLastSibling  bool             // true when last among siblings (└─ vs ├─)
	Gutters        []GutterInfo     // continuation columns to draw │
}

// GutterInfo describes one │ column in the tree rendering prefix.
type GutterInfo struct {
	Position int  // depth level where this gutter lives
	Show     bool // true = draw │, false = draw space
}

var ErrSessionNotFound = errors.New("session not found")

// Session represents persisted session metadata.
type Session struct {
	ID        string
	Title     string
	CreatedAt int64
	UpdatedAt int64
}

// Store is the entry point for persistence. It manages a directory of
// session journal files and keeps the current session's tree in memory.
type Store struct {
	dir string
	mu  sync.Mutex

	// session index (loaded at startup from file headers)
	sessions map[string]Session
	current  string // current active session ID

	// current session's in-memory tree
	nodes   map[string]*treeNode
	order   []string // message node IDs in append order (for List)
	leafID  string   // current leaf node ID (navigation pointer)
	titled  bool     // whether the current session title has been set
	pending bool     // true = ephemeral (no file written yet)
}

// OpenStore opens (or creates) the sessions directory and loads a Store.
func OpenStore() (*Store, error) {
	dir := paths.RaijinSessionsDir()
	if dir == "" {
		return nil, errors.New("persist: cannot resolve sessions directory")
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("persist: mkdir sessions: %w", err)
	}

	st := &Store{
		dir:      dir,
		sessions: make(map[string]Session),
		nodes:    make(map[string]*treeNode),
	}

	if err := st.loadSessionIndex(); err != nil {
		return nil, err
	}
	st.loadState()
	return st, nil
}

// Messages returns a libagent.MessageService backed by this store.
func (st *Store) Messages() libagent.MessageService { return &messageService{store: st} }

// ListSessionSummaries returns summaries of all known sessions, newest first.
// Ephemeral (not-yet-flushed) sessions are excluded.
func (st *Store) ListSessionSummaries() []SessionSummary {
	st.mu.Lock()
	defer st.mu.Unlock()

	out := make([]SessionSummary, 0, len(st.sessions))
	for _, sess := range st.sessions {
		if st.current == sess.ID && st.pending {
			continue
		}
		out = append(out, SessionSummary{
			ID:        sess.ID,
			ShortID:   ShortID(sess.ID),
			Title:     sess.Title,
			UpdatedAt: sess.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

// CreateEphemeral registers a fresh session in memory only.
// No journal file is written until the first message is stored.
func (st *Store) CreateEphemeral() (Session, error) {
	now := time.Now().Unix()
	sess := Session{
		ID:        uuid.New().String(),
		CreatedAt: now,
		UpdatedAt: now,
	}

	st.mu.Lock()
	st.sessions[sess.ID] = sess
	st.current = sess.ID
	st.nodes = make(map[string]*treeNode)
	st.order = nil
	st.leafID = ""
	st.titled = false
	st.pending = true
	st.mu.Unlock()

	return sess, nil
}

// RemoveSession permanently deletes a session from memory and from disk.
func (st *Store) RemoveSession(sessionID string) error {
	st.mu.Lock()
	if _, ok := st.sessions[sessionID]; !ok {
		st.mu.Unlock()
		return ErrSessionNotFound
	}
	if st.current == sessionID {
		st.mu.Unlock()
		return errors.New("persist: cannot remove the active session")
	}
	delete(st.sessions, sessionID)
	st.mu.Unlock()

	_ = os.Remove(st.journalPath(sessionID))
	return nil
}

// AppendCompaction appends a compaction checkpoint entry to the current session.
// The entry records a summary and the first message ID that should remain visible.
func (st *Store) AppendCompaction(summary, firstKeptID string, tokensBefore int64) error {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return errors.New("persist: compaction summary is empty")
	}
	if firstKeptID == "" {
		return errors.New("persist: compaction first-kept ID is empty")
	}

	st.mu.Lock()
	sessionID := st.current
	if sessionID == "" {
		st.mu.Unlock()
		return ErrSessionNotFound
	}
	if _, ok := st.nodes[firstKeptID]; !ok {
		st.mu.Unlock()
		return errors.New("persist: compaction first-kept node not found")
	}
	if err := st.flushHeaderLocked(sessionID); err != nil {
		st.mu.Unlock()
		return err
	}

	parentID := st.leafID
	entryID := uuid.New().String()
	entry := jEntry{
		Typ:          jTypeCompaction,
		ID:           entryID,
		ParentID:     parentID,
		Summary:      summary,
		FirstKeptID:  firstKeptID,
		TokensBefore: tokensBefore,
	}
	st.mu.Unlock()

	if err := st.appendEntry(sessionID, entry); err != nil {
		return err
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	n := &treeNode{
		id:           entryID,
		parentID:     parentID,
		typ:          jTypeCompaction,
		summary:      summary,
		firstKeptID:  firstKeptID,
		tokensBefore: tokensBefore,
	}
	st.nodes[entryID] = n
	st.leafID = entryID
	if parent, ok := st.nodes[parentID]; ok {
		parent.children = append(parent.children, entryID)
	}
	if sess, ok := st.sessions[sessionID]; ok {
		sess.UpdatedAt = time.Now().Unix()
		st.sessions[sessionID] = sess
	}
	st.saveStateLocked(sessionID)
	return nil
}

// Navigate moves the leaf pointer to targetID within the current session.
// If target is a user message, the leaf is set to its parent (so the user
// can re-submit the message from the editor) and its text is returned.
// For all other node types the leaf is set to target itself.
func (st *Store) Navigate(targetID string) (editorText string, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	node, ok := st.nodes[targetID]
	if !ok {
		return "", errors.New("persist: navigate: node not found")
	}

	if node.msg != nil {
		if um, ok := node.msg.(*libagent.UserMessage); ok {
			editorText = strings.TrimSpace(um.Content)
			st.leafID = node.parentID
			return editorText, nil
		}
	}

	st.leafID = targetID
	return "", nil
}

// GetTree returns all tree entries for the current session using Pi's
// flattenTree algorithm: active branch first at every branch point, depth
// increases only at branch points, connector/gutter metadata is recomputed
// for the visible set (tool-call-only assistant messages are hidden and their
// children are promoted, mirroring Pi's recalculateVisualStructure).
func (st *Store) GetTree() []TreeEntry {
	st.mu.Lock()
	defer st.mu.Unlock()

	if len(st.nodes) == 0 {
		return nil
	}

	// Only expose nodes that are safe navigation targets: selecting them must
	// not separate tool calls from their tool results in the resulting path.
	navigationSafe := make(map[string]bool, len(st.nodes))
	for id, n := range st.nodes {
		if n == nil || n.msg == nil {
			navigationSafe[id] = true
			continue
		}
		leafID := id
		if _, ok := n.msg.(*libagent.UserMessage); ok {
			leafID = n.parentID
		}
		navigationSafe[id] = hasBijectiveToolCouplingFromLeaf(st.nodes, leafID)
	}

	// isDisplayed: hide compaction checkpoints and assistant messages with no
	// visible text (tool-call-only intermediaries). Hidden nodes have their
	// children promoted to the nearest visible level.
	isDisplayed := func(node *treeNode) bool {
		if node == nil {
			return false
		}
		if node.typ == jTypeCompaction {
			return false
		}
		if !navigationSafe[node.id] {
			return false
		}
		msg := node.msg
		if msg == nil {
			return true
		}
		am, ok := msg.(*libagent.AssistantMessage)
		if !ok {
			return true
		}
		return strings.TrimSpace(am.Text) != ""
	}

	// Build the active path set: leafID → root.
	activePath := make(map[string]struct{}, len(st.nodes))
	cur := st.leafID
	visited := make(map[string]struct{}, len(st.nodes))
	for cur != "" {
		if _, ok := visited[cur]; ok {
			break
		}
		visited[cur] = struct{}{}
		activePath[cur] = struct{}{}
		n, ok := st.nodes[cur]
		if !ok {
			break
		}
		cur = n.parentID
	}

	onActivePath := func(id string) bool {
		_, ok := activePath[id]
		return ok
	}

	// visibleChildren returns the "virtual children" of a set of raw child IDs,
	// flattening through invisible (filtered-out) intermediary nodes.
	// This is the Go equivalent of Pi's recalculateVisualStructure promotion.
	var visibleChildren func(ids []string) []string
	visibleChildren = func(ids []string) []string {
		var out []string
		for _, id := range ids {
			n, ok := st.nodes[id]
			if !ok {
				continue
			}
			if isDisplayed(n) {
				out = append(out, id)
			} else {
				out = append(out, visibleChildren(n.children)...)
			}
		}
		return out
	}

	// Collect physical roots in append order (deterministic).
	var physRoots []string
	inRoots := make(map[string]struct{})
	for _, id := range st.order {
		n := st.nodes[id]
		if _, ok := inRoots[id]; ok {
			continue
		}
		if n.parentID == "" {
			inRoots[id] = struct{}{}
			physRoots = append(physRoots, id)
		} else if _, ok := st.nodes[n.parentID]; !ok {
			inRoots[id] = struct{}{}
			physRoots = append(physRoots, id)
		}
	}

	// Resolve visible roots, promoting through any invisible physical roots.
	visRoots := visibleChildren(physRoots)
	sort.SliceStable(visRoots, func(i, j int) bool {
		return onActivePath(visRoots[i]) && !onActivePath(visRoots[j])
	})

	out := make([]TreeEntry, 0, len(st.nodes))

	// dfs emits one visible entry and recurses into its visible virtual children.
	// siblings is the pre-computed visible sibling list so connector/isLast are correct.
	var dfs func(id string, depth int, siblings []string, myIdx int, gutters []GutterInfo)
	dfs = func(id string, depth int, siblings []string, myIdx int, gutters []GutterInfo) {
		n, ok := st.nodes[id]
		if !ok {
			return
		}

		hasMultiple := len(siblings) > 1
		isLast := myIdx == len(siblings)-1

		// Visible virtual children of this node.
		vChildren := visibleChildren(n.children)
		sort.SliceStable(vChildren, func(i, j int) bool {
			return onActivePath(vChildren[i]) && !onActivePath(vChildren[j])
		})

		out = append(out, TreeEntry{
			ID:             id,
			ParentID:       n.parentID,
			Msg:            n.msg,
			IsLeaf:         id == st.leafID,
			IsOnActivePath: onActivePath(id),
			Depth:          depth,
			ShowConnector:  hasMultiple,
			IsLastSibling:  isLast,
			Gutters:        gutters,
		})

		childDepth := depth
		if len(vChildren) > 1 {
			childDepth = depth + 1
		}

		childGutters := gutters
		if hasMultiple {
			connectorPos := depth - 1
			childGutters = append(append([]GutterInfo(nil), gutters...), GutterInfo{
				Position: connectorPos,
				Show:     !isLast,
			})
		}

		for i, child := range vChildren {
			dfs(child, childDepth, vChildren, i, childGutters)
		}
	}

	for i, root := range visRoots {
		dfs(root, 0, visRoots, i, nil)
	}
	return out
}

func hasBijectiveToolCouplingFromLeaf(nodes map[string]*treeNode, leafID string) bool {
	seen := make(map[string]struct{}, len(nodes))
	path := make([]libagent.Message, 0, len(nodes))

	cur := leafID
	for cur != "" {
		if _, ok := seen[cur]; ok {
			break
		}
		seen[cur] = struct{}{}

		n, ok := nodes[cur]
		if !ok || n == nil {
			break
		}
		if n.msg != nil {
			path = append(path, n.msg)
		}
		cur = n.parentID
	}
	// Build chronological order (root -> leaf) for coupling validation.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return libagent.HasBijectiveToolCoupling(path)
}

// ---------------------------------------------------------------------------
// journal I/O
// ---------------------------------------------------------------------------

func (st *Store) journalPath(sessionID string) string {
	return filepath.Join(st.dir, sessionID+".jsonl")
}

func (st *Store) statePath() string {
	return filepath.Join(st.dir, "state.json")
}

// flushHeader writes the session.create entry if the session is still ephemeral.
// Caller must hold mu or be certain no concurrent writes are happening.
func (st *Store) flushHeaderLocked(sessionID string) error {
	if !st.pending || st.current != sessionID {
		return nil
	}
	sess, ok := st.sessions[sessionID]
	if !ok {
		return errors.New("persist: session not found for header flush")
	}
	entry := jEntry{
		Typ:      jTypeSession,
		SessID:   sessionID,
		Title:    sess.Title,
		SessTime: sess.CreatedAt,
	}
	if err := st.appendEntryLocked(sessionID, entry); err != nil {
		return err
	}
	st.pending = false
	st.saveStateLocked(sessionID)
	return nil
}

// appendEntry appends a single journal entry to the given session's file.
func (st *Store) appendEntry(sessionID string, entry jEntry) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.appendEntryLocked(sessionID, entry)
}

// appendEntryLocked is appendEntry without locking. Caller must hold mu.
func (st *Store) appendEntryLocked(sessionID string, entry jEntry) error {
	entry.V = journalVersion
	entry.T = time.Now().Unix()
	entry.SessID = sessionID

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("persist: marshal entry: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(st.journalPath(sessionID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("persist: open journal: %w", err)
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("persist: write journal: %w", writeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("persist: sync journal: %w", syncErr)
	}
	return closeErr
}

type stateFile struct {
	CurrentSessionID string `json:"current_session_id"`
}

func (st *Store) saveStateLocked(sessionID string) {
	data, _ := json.Marshal(stateFile{CurrentSessionID: sessionID})
	_ = os.WriteFile(st.statePath(), data, filePerm)
}

func (st *Store) saveState(sessionID string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.saveStateLocked(sessionID)
}

func (st *Store) loadState() {
	data, err := os.ReadFile(st.statePath())
	if err != nil {
		return
	}
	var sf stateFile
	if json.Unmarshal(data, &sf) != nil || sf.CurrentSessionID == "" {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.sessions[sf.CurrentSessionID]; ok {
		st.current = sf.CurrentSessionID
	}
}

// loadSessionIndex scans the journal directory and replays only session-level
// entries to build the in-memory session index cheaply at startup.
func (st *Store) loadSessionIndex() error {
	entries, err := os.ReadDir(st.dir)
	if err != nil {
		return fmt.Errorf("persist: read sessions dir: %w", err)
	}
	for _, de := range entries {
		name := de.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".jsonl")
		sess, err := replaySessionMeta(filepath.Join(st.dir, name))
		if err != nil || sess.ID == "" {
			continue
		}
		st.mu.Lock()
		st.sessions[sessionID] = sess
		st.mu.Unlock()
	}
	return nil
}

// loadSessionTree replays a journal file and builds the full in-memory tree
// for the given session. Called on first access to message data.
// Caller must NOT hold mu.
func (st *Store) loadSessionTree(sessionID string) error {
	nodes, order, leafID, titled, err := replayJournal(st.journalPath(sessionID), sessionID)
	if err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.current != sessionID {
		return nil // switched away while loading
	}
	st.nodes = nodes
	st.order = order
	st.leafID = leafID
	st.titled = titled
	return nil
}

// replaySessionMeta reads a journal file and returns the latest session metadata.
func replaySessionMeta(path string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Session{}, nil
		}
		return Session{}, err
	}
	defer f.Close()

	var sess Session
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	// Skip message entries (potentially large payloads); parse everything else.
	msgMarker := []byte(`"typ":"msg`)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if bytes.Contains(line, msgMarker) {
			continue
		}
		var entry jEntry
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		if entry.V != journalVersion {
			continue
		}
		switch entry.Typ {
		case jTypeSession:
			sess = Session{
				ID:        entry.SessID,
				Title:     entry.Title,
				CreatedAt: entry.SessTime,
				UpdatedAt: entry.T,
			}
		case jTypeTitle:
			if entry.SessID != "" {
				sess.ID = entry.SessID
				sess.Title = entry.Title
				sess.UpdatedAt = entry.T
			}
		}
	}
	return sess, scanner.Err()
}

// replayJournal reads a full journal file and reconstructs the tree in memory.
func replayJournal(path string, sessionID string) (
	nodes map[string]*treeNode,
	order []string,
	leafID string,
	titled bool,
	err error,
) {
	nodes = make(map[string]*treeNode)
	order = make([]string, 0)

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nodes, order, "", false, nil
		}
		return nil, nil, "", false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry jEntry
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		if entry.V != journalVersion {
			continue
		}
		switch entry.Typ {
		case jTypeMsg:
			if entry.ID == "" || entry.Msg == nil {
				continue
			}
			m, ok := jMsgToMessage(*entry.Msg)
			if !ok {
				continue
			}
			n := &treeNode{id: entry.ID, parentID: entry.ParentID, typ: jTypeMsg, msg: m}
			nodes[entry.ID] = n
			order = append(order, entry.ID)
			leafID = entry.ID
			// link parent → child
			if entry.ParentID != "" {
				if parent, ok := nodes[entry.ParentID]; ok {
					parent.children = append(parent.children, entry.ID)
				}
			}
			if _, ok := m.(*libagent.UserMessage); ok {
				if !titled {
					if strings.TrimSpace(m.(*libagent.UserMessage).Content) != "" {
						titled = true
					}
				}
			}
		case jTypeMsgUpdate:
			if entry.ID == "" || entry.Msg == nil {
				continue
			}
			m, ok := jMsgToMessage(*entry.Msg)
			if !ok {
				continue
			}
			if n, exists := nodes[entry.ID]; exists {
				n.msg = m
			}
		case jTypeCompaction:
			if entry.ID == "" || entry.FirstKeptID == "" || strings.TrimSpace(entry.Summary) == "" {
				continue
			}
			n := &treeNode{
				id:           entry.ID,
				parentID:     entry.ParentID,
				typ:          jTypeCompaction,
				summary:      strings.TrimSpace(entry.Summary),
				firstKeptID:  entry.FirstKeptID,
				tokensBefore: entry.TokensBefore,
			}
			nodes[entry.ID] = n
			leafID = entry.ID
			if entry.ParentID != "" {
				if parent, ok := nodes[entry.ParentID]; ok {
					parent.children = append(parent.children, entry.ID)
				}
			}
		}
	}

	return nodes, order, leafID, titled, scanner.Err()
}

// ---------------------------------------------------------------------------
// message helpers
// ---------------------------------------------------------------------------

func messageToJMsg(m libagent.Message) jMsg {
	switch msg := m.(type) {
	case *libagent.UserMessage:
		return jMsg{Kind: "user", User: libagent.CloneMessage(msg).(*libagent.UserMessage)}
	case *libagent.AssistantMessage:
		clone := libagent.CloneMessage(msg).(*libagent.AssistantMessage)
		clone.Content = nil
		clone.Error = nil
		return jMsg{Kind: "assistant", Assistant: clone}
	case *libagent.ToolResultMessage:
		return jMsg{Kind: "tool_result", ToolResult: libagent.CloneMessage(msg).(*libagent.ToolResultMessage)}
	default:
		return jMsg{}
	}
}

func jMsgToMessage(jm jMsg) (libagent.Message, bool) {
	switch jm.Kind {
	case "user":
		if jm.User == nil {
			return nil, false
		}
		return libagent.CloneMessage(jm.User), true
	case "assistant":
		if jm.Assistant == nil {
			return nil, false
		}
		return libagent.CloneMessage(jm.Assistant), true
	case "tool_result":
		if jm.ToolResult == nil {
			return nil, false
		}
		return libagent.CloneMessage(jm.ToolResult), true
	default:
		return nil, false
	}
}

// GetSession returns metadata for a persisted session.
func (st *Store) GetSession(id string) (Session, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	sess, ok := st.sessions[id]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return sess, nil
}

// SetCurrent makes sessionID the active session and loads its tree.
func (st *Store) SetCurrent(id string) error {
	st.mu.Lock()
	if _, ok := st.sessions[id]; !ok {
		st.mu.Unlock()
		return ErrSessionNotFound
	}
	prev := st.current
	st.current = id
	needLoad := id != prev
	st.mu.Unlock()

	if needLoad {
		// Reset tree so it gets reloaded on first message access.
		st.mu.Lock()
		st.nodes = make(map[string]*treeNode)
		st.order = nil
		st.leafID = ""
		st.titled = false
		st.pending = false
		st.mu.Unlock()

		_ = st.loadSessionTree(id)
	}
	st.saveState(id)
	return nil
}

// setTitleIfUnset assigns the session title if it has not been set yet.
func (st *Store) setTitleIfUnset(sessionID, title string) {
	st.mu.Lock()
	sess, ok := st.sessions[sessionID]
	if !ok || sess.Title != "" {
		st.mu.Unlock()
		return
	}
	sess.Title = title
	sess.UpdatedAt = time.Now().Unix()
	st.sessions[sessionID] = sess
	st.mu.Unlock()

	_ = st.appendEntry(sessionID, jEntry{
		Typ:   jTypeTitle,
		Title: title,
	})
}

// ---------------------------------------------------------------------------
// libagent.MessageService implementation
// ---------------------------------------------------------------------------

type messageService struct {
	store *Store
}

func (ms *messageService) ensureLoaded(sessionID string) {
	ms.store.mu.Lock()
	isCurrent := ms.store.current == sessionID
	alreadyLoaded := isCurrent && (len(ms.store.nodes) > 0 || ms.store.pending)
	ms.store.mu.Unlock()

	if !isCurrent || alreadyLoaded {
		return
	}
	_ = ms.store.loadSessionTree(sessionID)
}

func (ms *messageService) Create(ctx context.Context, sessionID string, msg libagent.Message) (libagent.Message, error) {
	ms.ensureLoaded(sessionID)

	now := time.Now().Unix()
	meta := libagent.MessageMetaOf(msg)
	if meta.ID == "" {
		meta.ID = uuid.New().String()
	}
	meta.SessionID = sessionID
	if meta.CreatedAt == 0 {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now
	toStore := libagent.CloneMessage(msg)
	libagent.SetMessageMeta(toStore, meta)

	ms.store.mu.Lock()
	// Flush ephemeral header before first message.
	if flushErr := ms.store.flushHeaderLocked(sessionID); flushErr != nil {
		ms.store.mu.Unlock()
		return nil, flushErr
	}
	parentID := ms.store.leafID
	jm := messageToJMsg(toStore)
	ms.store.mu.Unlock()

	entryID := meta.ID
	entry := jEntry{
		Typ:      jTypeMsg,
		ID:       entryID,
		ParentID: parentID,
		Msg:      &jm,
	}
	if err := ms.store.appendEntry(sessionID, entry); err != nil {
		return nil, err
	}

	ms.store.mu.Lock()
	n := &treeNode{id: entryID, parentID: parentID, typ: jTypeMsg, msg: toStore}
	ms.store.nodes[entryID] = n
	ms.store.order = append(ms.store.order, entryID)
	ms.store.leafID = entryID
	if parent, ok := ms.store.nodes[parentID]; ok {
		parent.children = append(parent.children, entryID)
	}
	titled := ms.store.titled
	if _, ok := toStore.(*libagent.UserMessage); ok {
		if strings.TrimSpace(toStore.(*libagent.UserMessage).Content) != "" {
			ms.store.titled = true
		}
	}
	ms.store.mu.Unlock()

	if um, ok := toStore.(*libagent.UserMessage); ok && !titled {
		if t := strings.TrimSpace(um.Content); t != "" {
			ms.store.setTitleIfUnset(sessionID, TitleFromFirstUserMessage(t))
		}
	}

	return libagent.CloneMessage(toStore), nil
}

func (ms *messageService) Update(_ context.Context, msg libagent.Message) error {
	id := libagent.MessageID(msg)
	if id == "" {
		return libagent.ErrMessageNotFound
	}

	ms.store.mu.Lock()
	_, ok := ms.store.nodes[id]
	if !ok {
		ms.store.mu.Unlock()
		return libagent.ErrMessageNotFound
	}
	meta := libagent.MessageMetaOf(msg)
	meta.UpdatedAt = time.Now().Unix()
	toStore := libagent.CloneMessage(msg)
	libagent.SetMessageMeta(toStore, meta)
	ms.store.nodes[id].msg = toStore
	sessionID := ms.store.current
	ms.store.mu.Unlock()

	jm := messageToJMsg(toStore)
	return ms.store.appendEntry(sessionID, jEntry{
		Typ: jTypeMsgUpdate,
		ID:  id,
		Msg: &jm,
	})
}

func (ms *messageService) Get(_ context.Context, id string) (libagent.Message, error) {
	ms.store.mu.Lock()
	defer ms.store.mu.Unlock()
	n, ok := ms.store.nodes[id]
	if !ok || n.msg == nil {
		return nil, libagent.ErrMessageNotFound
	}
	return libagent.CloneMessage(n.msg), nil
}

// List returns messages on the path from the current leaf to the root,
// in chronological order (oldest first).
func (ms *messageService) List(_ context.Context, sessionID string) ([]libagent.Message, error) {
	ms.ensureLoaded(sessionID)

	ms.store.mu.Lock()
	defer ms.store.mu.Unlock()

	if ms.store.current != sessionID {
		return nil, nil
	}

	// Walk from leaf to root.
	pathNodes := make([]*treeNode, 0, len(ms.store.nodes))
	seen := make(map[string]struct{}, len(ms.store.nodes))
	cur := ms.store.leafID
	for cur != "" {
		if _, ok := seen[cur]; ok {
			break
		}
		seen[cur] = struct{}{}
		n, ok := ms.store.nodes[cur]
		if !ok {
			break
		}
		pathNodes = append(pathNodes, n)
		cur = n.parentID
	}

	// Reverse to get chronological order (root -> leaf).
	for i, j := 0, len(pathNodes)-1; i < j; i, j = i+1, j-1 {
		pathNodes[i], pathNodes[j] = pathNodes[j], pathNodes[i]
	}

	type compactionView struct {
		idx         int
		summary     string
		firstKeptID string
	}
	var latest *compactionView
	for i := len(pathNodes) - 1; i >= 0; i-- {
		n := pathNodes[i]
		if n == nil || n.typ != jTypeCompaction {
			continue
		}
		latest = &compactionView{
			idx:         i,
			summary:     n.summary,
			firstKeptID: n.firstKeptID,
		}
		break
	}

	path := make([]libagent.Message, 0, len(pathNodes))
	appendMessage := func(node *treeNode) {
		if node == nil || node.msg == nil {
			return
		}
		path = append(path, libagent.CloneMessage(node.msg))
	}

	if latest == nil {
		for _, n := range pathNodes {
			appendMessage(n)
		}
		return path, nil
	}

	path = append(path, compactionSummaryMessage(latest.summary))

	foundFirstKept := false
	for i := 0; i < latest.idx; i++ {
		n := pathNodes[i]
		if n == nil {
			continue
		}
		if n.id == latest.firstKeptID {
			foundFirstKept = true
		}
		if foundFirstKept {
			appendMessage(n)
		}
	}

	for i := latest.idx + 1; i < len(pathNodes); i++ {
		appendMessage(pathNodes[i])
	}
	return path, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func compactionSummaryMessage(summary string) libagent.Message {
	return &libagent.UserMessage{
		Role:      "user",
		Content:   compactionPref + strings.TrimSpace(summary),
		Timestamp: time.Now(),
	}
}

// ShortID converts a UUID string into a compact uppercase base-36 identifier
// of length 6, derived from the first 64 bits of the UUID.
func ShortID(uuidStr string) string {
	id, err := uuid.Parse(uuidStr)
	if err != nil {
		r := []rune(strings.ToUpper(uuidStr))
		if len(r) > 6 {
			r = r[:6]
		}
		return string(r)
	}
	var val uint64
	for i := range 8 {
		val = (val << 8) | uint64(id[i])
	}
	s := strings.ToUpper(strconv.FormatUint(val, 36))
	for len(s) < 6 {
		s = "0" + s
	}
	if len(s) > 6 {
		s = s[len(s)-6:]
	}
	return s
}

// TitleFromFirstUserMessage normalises whitespace and truncates to titleMaxLen runes.
func TitleFromFirstUserMessage(text string) string {
	normalized := strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
	runes := []rune(normalized)
	if len(runes) <= titleMaxLen {
		return normalized
	}
	return string(runes[:titleMaxLen-1]) + "…"
}
