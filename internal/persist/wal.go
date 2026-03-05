// Package persist provides WAL-backed (write-ahead log) implementations of
// the session.Service and message.Service interfaces. Each session is stored
// as a separate newline-delimited JSON file under
// ~/.config/raijin/sessions/<uuid>.wal. A tiny state.json in the same
// directory tracks the most recently active session.
package persist

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/francescoalemanno/raijin-mono/internal/paths"
	sessionstore "github.com/francescoalemanno/raijin-mono/internal/session"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

const (
	walVersion  = 1
	dirPerm     = 0o755
	filePerm    = 0o644
	titleMaxLen = 72 // runes
)

var ErrSessionDeleteActiveLineage = errors.New("session can be deleted only if inactive")

// entryType identifies a WAL entry.
type entryType string

const (
	entrySessionCreate entryType = "session.create"
	entrySessionTitle  entryType = "session.title"
	entryMsgCreate     entryType = "msg.create"
	entryMsgUpdate     entryType = "msg.update"
	entryMsgDelete     entryType = "msg.delete"
	entryMsgDeleteAll  entryType = "msg.delete_all"
)

// walEntry is the NDJSON envelope written to the WAL file.
type walEntry struct {
	V         int         `json:"v"`
	T         int64       `json:"t"`
	Typ       entryType   `json:"typ"`
	SessionID string      `json:"sid,omitempty"`
	Session   *walSession `json:"session,omitempty"`
	Msg       *walMessage `json:"msg,omitempty"`
	MsgID     string      `json:"mid,omitempty"`
}

// walSessionMetaEntry keeps only fields needed for session index replay.
// Using this during startup avoids decoding large message payloads.
type walSessionMetaEntry struct {
	V       int         `json:"v"`
	Typ     entryType   `json:"typ"`
	Session *walSession `json:"session,omitempty"`
}

// walSession is the serialisable form of session.Session.
type walSession struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	ParentSessionID     string `json:"parent_session_id,omitempty"`
	ForkedFromMessageID string `json:"forked_from_message_id,omitempty"`
	CreatedAt           int64  `json:"created_at"`
	UpdatedAt           int64  `json:"updated_at"`
}

// walMessage is the serialisable form of runtime libagent messages.
// It also retains legacy fields for backward-compatible decode.
type walMessage struct {
	Kind       string                      `json:"kind,omitempty"`
	User       *libagent.UserMessage       `json:"user,omitempty"`
	Assistant  *libagent.AssistantMessage  `json:"assistant,omitempty"`
	ToolResult *libagent.ToolResultMessage `json:"tool_result,omitempty"`

	// Legacy v1 fields
	ID         string            `json:"id,omitempty"`
	Role       string            `json:"role,omitempty"`
	SessionID  string            `json:"session_id,omitempty"`
	Parts      []json.RawMessage `json:"parts,omitempty"`
	Completion *walCompletion    `json:"completion,omitempty"`
	Model      string            `json:"model,omitempty"`
	Provider   string            `json:"provider,omitempty"`
	CreatedAt  int64             `json:"created_at,omitempty"`
	UpdatedAt  int64             `json:"updated_at,omitempty"`
}

type walCompletion struct {
	Reason   string `json:"reason"`
	Time     int64  `json:"time"`
	Finished bool   `json:"finished"`
	Message  string `json:"message,omitempty"`
	Details  string `json:"details,omitempty"`
}

type legacyWalPartType string

const (
	legacyWalPartText       legacyWalPartType = "text"
	legacyWalPartReasoning  legacyWalPartType = "reasoning"
	legacyWalPartToolCall   legacyWalPartType = "tool_call"
	legacyWalPartToolResult legacyWalPartType = "tool_result"
	legacyWalPartFinish     legacyWalPartType = "finish"
	legacyWalPartBinary     legacyWalPartType = "binary"
	legacyWalPartSkill      legacyWalPartType = "skill"
)

// legacyWalPart encodes historical part records used in WAL v1.
type legacyWalPart struct {
	T    legacyWalPartType `json:"t"`
	Data json.RawMessage   `json:"d"`
}

// SessionSummary is the information exposed to the /sessions selector.
type SessionSummary struct {
	ID                  string
	ShortID             string
	Title               string
	ParentSessionID     string
	ForkedFromMessageID string
	UpdatedAt           int64
}

// Store is the entry point for persistence. It owns two service
// implementations that write to the WAL, and exposes a SessionSummary list
// for the /sessions UI.
type Store struct {
	dir string
	mu  sync.Mutex

	sessSvc *SessionService
	msgSvc  *MessageService
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

	st := &Store{dir: dir}
	st.sessSvc = newSessionService(st)
	st.msgSvc = newMessageService(st)

	// Load session index from WAL files present on disk.
	if err := st.loadSessionIndex(); err != nil {
		return nil, err
	}

	// Restore last current session from state.json (best-effort).
	st.loadState()

	return st, nil
}

// Sessions returns the session.Service backed by this store.
func (st *Store) Sessions() sessionstore.Service { return st.sessSvc }

// Messages returns the runtime message service backed by this store.
func (st *Store) Messages() libagent.MessageService { return st.msgSvc }

// ListSessionSummaries returns summaries of all known sessions, newest first.
func (st *Store) ListSessionSummaries() []SessionSummary {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sessSvc.summariesLocked()
}

// CreateEphemeral registers a fresh session in memory only — no WAL entry is
// written. The session becomes durable the first time a user message is stored
// for it (MessageService.Create flushes the session.create entry at that
// point). Ephemeral sessions are excluded from ListSessionSummaries.
func (st *Store) CreateEphemeral() (sessionstore.Session, error) {
	now := time.Now().Unix()
	sess := sessionstore.Session{
		ID:        uuid.New().String(),
		CreatedAt: now,
		UpdatedAt: now,
	}

	st.sessSvc.mu.Lock()
	st.sessSvc.sessions[sess.ID] = sess
	st.sessSvc.current = sess.ID
	st.sessSvc.mu.Unlock()

	st.msgSvc.mu.Lock()
	st.msgSvc.pendingFlush[sess.ID] = true
	st.msgSvc.loaded[sess.ID] = true // nothing to replay from disk
	st.msgSvc.mu.Unlock()

	return sess, nil
}

// ForkSession creates a new durable child session linked to a parent session
// and to the parent message where the fork starts. The child WAL starts with
// session metadata only; ancestor messages are resolved at read time.
func (st *Store) ForkSession(parentSessionID, forkedFromMessageID string, msgs []libagent.Message) (sessionstore.Session, error) {
	now := time.Now().Unix()
	sess := sessionstore.Session{
		ID:                  uuid.New().String(),
		ParentSessionID:     parentSessionID,
		ForkedFromMessageID: forkedFromMessageID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	path := st.walPath(sess.ID)
	tmp := path + ".fork"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, filePerm)
	if err != nil {
		return sessionstore.Session{}, fmt.Errorf("persist: fork wal create: %w", err)
	}

	writeEntry := func(entry walEntry) error {
		entry.V = walVersion
		entry.SessionID = sess.ID
		line, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		_, err = f.Write(append(line, '\n'))
		return err
	}

	if err := writeEntry(walEntry{
		T:   now,
		Typ: entrySessionCreate,
		Session: &walSession{
			ID:                  sess.ID,
			Title:               sess.Title,
			ParentSessionID:     sess.ParentSessionID,
			ForkedFromMessageID: sess.ForkedFromMessageID,
			CreatedAt:           sess.CreatedAt,
			UpdatedAt:           sess.UpdatedAt,
		},
	}); err != nil {
		f.Close()
		os.Remove(tmp)
		return sessionstore.Session{}, fmt.Errorf("persist: fork wal write header: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return sessionstore.Session{}, fmt.Errorf("persist: fork wal sync: %w", err)
	}
	f.Close()

	st.mu.Lock()
	renameErr := os.Rename(tmp, path)
	st.mu.Unlock()
	if renameErr != nil {
		os.Remove(tmp)
		return sessionstore.Session{}, fmt.Errorf("persist: fork wal rename: %w", renameErr)
	}

	st.sessSvc.mu.Lock()
	st.sessSvc.sessions[sess.ID] = sess
	st.sessSvc.current = sess.ID
	st.sessSvc.mu.Unlock()

	// Child session starts with no local messages. Leave loaded=false so
	// ensureLoaded can materialize inherited lineage on first read.
	st.msgSvc.mu.Lock()
	delete(st.msgSvc.loaded, sess.ID)
	delete(st.msgSvc.bySession, sess.ID)
	st.msgSvc.mu.Unlock()

	st.saveState(sess.ID)
	return sess, nil
}

// RemoveSession permanently deletes a session subtree from memory and removes
// WAL files from disk. Deleting a parent deletes all descendants.
func (st *Store) RemoveSession(sessionID string) error {
	st.sessSvc.mu.Lock()
	if _, ok := st.sessSvc.sessions[sessionID]; !ok {
		st.sessSvc.mu.Unlock()
		return sessionstore.ErrSessionNotFound
	}
	if st.isOnCurrentLineageLocked(sessionID) {
		st.sessSvc.mu.Unlock()
		return ErrSessionDeleteActiveLineage
	}

	toDelete := st.collectSessionSubtreeLocked(sessionID)
	clearCurrent := false
	for _, id := range toDelete {
		delete(st.sessSvc.sessions, id)
		if st.sessSvc.current == id {
			clearCurrent = true
		}
	}
	if clearCurrent {
		st.sessSvc.current = ""
	}
	st.sessSvc.mu.Unlock()

	st.msgSvc.mu.Lock()
	for _, sid := range toDelete {
		ids := st.msgSvc.bySession[sid]
		for _, id := range ids {
			delete(st.msgSvc.messages, id)
		}
		delete(st.msgSvc.bySession, sid)
		delete(st.msgSvc.loaded, sid)
		delete(st.msgSvc.titled, sid)
		delete(st.msgSvc.pendingFlush, sid)
	}
	st.msgSvc.mu.Unlock()

	for _, sid := range toDelete {
		_ = os.Remove(st.walPath(sid))
	}

	if clearCurrent {
		st.saveState("")
	}
	return nil
}

// collectSessionSubtreeLocked returns the root session plus all descendants.
// Caller must hold st.sessSvc.mu.
func (st *Store) collectSessionSubtreeLocked(rootID string) []string {
	queue := []string{rootID}
	out := make([]string, 0, 4)
	seen := map[string]struct{}{rootID: {}}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		out = append(out, node)

		for id, sess := range st.sessSvc.sessions {
			if sess.ParentSessionID != node {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			queue = append(queue, id)
		}
	}
	return out
}

// isOnCurrentLineageLocked reports whether sessionID is on the active lineage,
// defined as current session plus its ancestor chain to the root.
// Caller must hold st.sessSvc.mu.
func (st *Store) isOnCurrentLineageLocked(sessionID string) bool {
	current := st.sessSvc.current
	if current == "" {
		return false
	}

	seen := make(map[string]struct{}, 8)
	for current != "" {
		if current == sessionID {
			return true
		}
		if _, ok := seen[current]; ok {
			break
		}
		seen[current] = struct{}{}
		sess, ok := st.sessSvc.sessions[current]
		if !ok {
			break
		}
		current = sess.ParentSessionID
	}
	return false
}

// walPath returns the WAL file path for a session ID.
func (st *Store) walPath(sessionID string) string {
	return filepath.Join(st.dir, sessionID+".wal")
}

// statePath returns the path of the small state.json file.
func (st *Store) statePath() string {
	return filepath.Join(st.dir, "state.json")
}

// appendEntry appends a single WAL entry to the given session's file.
// Caller must NOT hold st.mu (this method acquires it briefly).
func (st *Store) appendEntry(sessionID string, entry walEntry) error {
	entry.V = walVersion
	entry.T = time.Now().Unix()
	entry.SessionID = sessionID

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("persist: marshal entry: %w", err)
	}
	data = append(data, '\n')

	st.mu.Lock()
	defer st.mu.Unlock()

	f, err := os.OpenFile(st.walPath(sessionID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("persist: open wal: %w", err)
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("persist: write wal: %w", writeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("persist: sync wal: %w", syncErr)
	}
	return closeErr
}

type stateFile struct {
	CurrentSessionID string `json:"current_session_id"`
}

func (st *Store) saveState(sessionID string) {
	data, _ := json.Marshal(stateFile{CurrentSessionID: sessionID})
	_ = os.WriteFile(st.statePath(), data, filePerm)
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
	st.sessSvc.mu.Lock()
	defer st.sessSvc.mu.Unlock()
	if _, ok := st.sessSvc.sessions[sf.CurrentSessionID]; ok {
		st.sessSvc.current = sf.CurrentSessionID
	}
}

// loadSessionIndex scans the WAL directory and replays only the session-level
// entries (not message bodies) to build the in-memory session index cheaply.
func (st *Store) loadSessionIndex() error {
	entries, err := os.ReadDir(st.dir)
	if err != nil {
		return fmt.Errorf("persist: read sessions dir: %w", err)
	}

	for _, de := range entries {
		name := de.Name()
		if !strings.HasSuffix(name, ".wal") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".wal")
		sess, err := replaySessionMeta(filepath.Join(st.dir, name))
		if err != nil || sess.ID == "" {
			continue
		}
		st.sessSvc.mu.Lock()
		st.sessSvc.sessions[sessionID] = sess
		st.sessSvc.mu.Unlock()
	}
	return nil
}

// replaySessionMeta reads a WAL file and returns the latest session metadata,
// ignoring message entries for speed.
func replaySessionMeta(path string) (sessionstore.Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return sessionstore.Session{}, err
	}
	defer f.Close()

	var sess sessionstore.Session
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	sessionTypeMarker := []byte(`"typ":"session.`)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Most WAL entries are msg.* and may carry large payloads. Skip them
		// without JSON decoding to keep startup replay cheap.
		if !bytes.Contains(line, sessionTypeMarker) {
			continue
		}

		var entry walSessionMetaEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // tolerate corrupt/partial lines (crash recovery)
		}
		if entry.V != walVersion {
			continue // skip entries from an incompatible WAL version
		}
		switch entry.Typ {
		case entrySessionCreate, entrySessionTitle:
			if entry.Session != nil {
				sess = sessionstore.Session{
					ID:                  entry.Session.ID,
					Title:               entry.Session.Title,
					ParentSessionID:     entry.Session.ParentSessionID,
					ForkedFromMessageID: entry.Session.ForkedFromMessageID,
					CreatedAt:           entry.Session.CreatedAt,
					UpdatedAt:           entry.Session.UpdatedAt,
				}
			}
		}
	}
	return sess, scanner.Err()
}

// compactWAL rewrites the WAL file for a session to contain only the minimal
// entries needed to reconstruct current state: one session header entry and
// one msg.create entry per surviving message. This prevents unbounded growth.
func (st *Store) compactWAL(sessionID string, msgs []libagent.Message) {
	path := st.walPath(sessionID)

	// Read the latest session metadata from the existing WAL.
	sess, err := replaySessionMeta(path)
	if err != nil || sess.ID == "" {
		return
	}

	headerEntry := walEntry{
		V:         walVersion,
		T:         time.Now().Unix(),
		Typ:       entrySessionCreate,
		SessionID: sessionID,
		Session: &walSession{
			ID:                  sess.ID,
			Title:               sess.Title,
			ParentSessionID:     sess.ParentSessionID,
			ForkedFromMessageID: sess.ForkedFromMessageID,
			CreatedAt:           sess.CreatedAt,
			UpdatedAt:           sess.UpdatedAt,
		},
	}
	headerLine, err := json.Marshal(headerEntry)
	if err != nil {
		return
	}

	lines := make([][]byte, 0, 1+len(msgs))
	lines = append(lines, headerLine)
	for _, m := range msgs {
		wm := messageToWalMsg(m)
		meta := libagent.MessageMetaOf(m)
		entry := walEntry{
			V:         walVersion,
			T:         meta.CreatedAt,
			Typ:       entryMsgCreate,
			SessionID: sessionID,
			Msg:       &wm,
		}
		line, err := json.Marshal(entry)
		if err != nil {
			return
		}
		lines = append(lines, line)
	}

	tmp := path + ".compact"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, filePerm)
	if err != nil {
		return
	}
	for _, line := range lines {
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			os.Remove(tmp)
			return
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return
	}
	f.Close()

	st.mu.Lock()
	defer st.mu.Unlock()
	os.Rename(tmp, path)
}

// replayMessages reads a WAL file and reconstructs the ordered message list.
// The returned bool is true when at least one msg.delete_all entry was seen.
func replayMessages(path string) ([]libagent.Message, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	msgs := make(map[string]libagent.Message)
	order := make([]string, 0)
	hadDeleteAll := false

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry walEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // tolerate corrupt/partial lines (crash recovery)
		}
		if entry.V != walVersion {
			continue // skip entries from an incompatible WAL version
		}
		switch entry.Typ {
		case entryMsgCreate:
			if entry.Msg == nil {
				continue
			}
			m, ok := walMsgToMessage(*entry.Msg)
			if !ok {
				continue
			}
			id := libagent.MessageID(m)
			if id == "" {
				continue
			}
			if _, seen := msgs[id]; !seen {
				order = append(order, id)
			}
			msgs[id] = m
		case entryMsgUpdate:
			if entry.Msg == nil {
				continue
			}
			m, ok := walMsgToMessage(*entry.Msg)
			if !ok {
				continue
			}
			id := libagent.MessageID(m)
			if id == "" {
				continue
			}
			msgs[id] = m
		case entryMsgDelete:
			if entry.MsgID == "" {
				continue
			}
			delete(msgs, entry.MsgID)
			for i, id := range order {
				if id == entry.MsgID {
					order = slices.Delete(order, i, i+1)
					break
				}
			}
		case entryMsgDeleteAll:
			hadDeleteAll = true
			msgs = make(map[string]libagent.Message)
			order = order[:0]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, hadDeleteAll, err
	}

	result := make([]libagent.Message, 0, len(order))
	for _, id := range order {
		if m, ok := msgs[id]; ok {
			result = append(result, m)
		}
	}
	return result, hadDeleteAll, nil
}

// ---------------------------------------------------------------------------
// Short ID
// ---------------------------------------------------------------------------

// ShortID converts a UUID string into a compact uppercase base-36 identifier
// of length 6, derived from the first 64 bits of the UUID.
func ShortID(uuidStr string) string {
	id, err := uuid.Parse(uuidStr)
	if err != nil {
		// Fall back to first 6 chars of raw string, uppercased.
		r := []rune(strings.ToUpper(uuidStr))
		if len(r) > 6 {
			r = r[:6]
		}
		return string(r)
	}
	// Use the first 8 bytes as a uint64 → base36.
	var val uint64
	for i := 0; i < 8; i++ {
		val = (val << 8) | uint64(id[i])
	}
	s := strings.ToUpper(strconv.FormatUint(val, 36))
	// Pad or trim to 6 characters.
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
