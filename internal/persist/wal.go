// Package persist provides WAL-backed (write-ahead log) implementations of
// the session.Service and message.Service interfaces. Each session is stored
// as a separate newline-delimited JSON file under
// ~/.config/raijin/sessions/<uuid>.wal. A tiny state.json in the same
// directory tracks the most recently active session.
package persist

import (
	"bufio"
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

	"github.com/francescoalemanno/raijin-mono/internal/message"
	"github.com/francescoalemanno/raijin-mono/internal/paths"
	sessionstore "github.com/francescoalemanno/raijin-mono/internal/session"
)

const (
	walVersion  = 1
	dirPerm     = 0o755
	filePerm    = 0o644
	titleMaxLen = 72 // runes
)

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

// walSession is the serialisable form of session.Session.
type walSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// walMessage is the serialisable form of message.Message.
type walMessage struct {
	ID        string              `json:"id"`
	Role      message.MessageRole `json:"role"`
	SessionID string              `json:"session_id"`
	Parts     []walPart           `json:"parts"`
	Model     string              `json:"model,omitempty"`
	Provider  string              `json:"provider,omitempty"`
	CreatedAt int64               `json:"created_at"`
	UpdatedAt int64               `json:"updated_at"`
}

type walPartType string

const (
	walPartText       walPartType = "text"
	walPartReasoning  walPartType = "reasoning"
	walPartToolCall   walPartType = "tool_call"
	walPartToolResult walPartType = "tool_result"
	walPartFinish     walPartType = "finish"
	walPartBinary     walPartType = "binary"
	walPartSkill      walPartType = "skill"
)

// walPart encodes a single ContentPart as tagged JSON.
type walPart struct {
	T    walPartType     `json:"t"`
	Data json.RawMessage `json:"d"`
}

// SessionSummary is the information exposed to the /sessions selector.
type SessionSummary struct {
	ID        string
	ShortID   string
	Title     string
	UpdatedAt int64
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

// Messages returns the message.Service backed by this store.
func (st *Store) Messages() message.Service { return st.msgSvc }

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

// ForkSession creates a new durable session whose WAL is pre-populated with
// the provided messages. It returns the new session so the caller can switch
// to it. Unlike CreateEphemeral, the WAL file is written immediately.
func (st *Store) ForkSession(msgs []message.Message) (sessionstore.Session, error) {
	now := time.Now().Unix()
	sess := sessionstore.Session{
		ID:        uuid.New().String(),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Derive title from the first user message if present.
	for _, m := range msgs {
		if m.Role == message.User {
			if text := m.Content().Text; text != "" {
				sess.Title = TitleFromFirstUserMessage(text)
			}
			break
		}
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

	// Write session header.
	if err := writeEntry(walEntry{
		T:       now,
		Typ:     entrySessionCreate,
		Session: &walSession{ID: sess.ID, Title: sess.Title, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt},
	}); err != nil {
		f.Close()
		os.Remove(tmp)
		return sessionstore.Session{}, fmt.Errorf("persist: fork wal write header: %w", err)
	}

	// Write one msg.create per message, preserving original timestamps.
	for _, m := range msgs {
		m.SessionID = sess.ID
		wm := messageToWalMsg(m)
		if err := writeEntry(walEntry{
			T:   m.CreatedAt,
			Typ: entryMsgCreate,
			Msg: &wm,
		}); err != nil {
			f.Close()
			os.Remove(tmp)
			return sessionstore.Session{}, fmt.Errorf("persist: fork wal write msg: %w", err)
		}
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

	// Register in memory so the session is immediately usable.
	st.sessSvc.mu.Lock()
	st.sessSvc.sessions[sess.ID] = sess
	st.sessSvc.current = sess.ID
	st.sessSvc.mu.Unlock()

	// Pre-populate message cache so ensureLoaded is a no-op.
	st.msgSvc.mu.Lock()
	for _, m := range msgs {
		m.SessionID = sess.ID
		st.msgSvc.messages[m.ID] = m
		st.msgSvc.bySession[sess.ID] = append(st.msgSvc.bySession[sess.ID], m.ID)
		if m.Role == message.User {
			st.msgSvc.titled[sess.ID] = true
		}
	}
	st.msgSvc.loaded[sess.ID] = true
	st.msgSvc.mu.Unlock()

	st.saveState(sess.ID)
	return sess, nil
}

// RemoveSession permanently deletes a session from memory and removes its WAL
// file from disk. If the deleted session is the current one, current is cleared.
func (st *Store) RemoveSession(sessionID string) error {
	st.sessSvc.mu.Lock()
	if _, ok := st.sessSvc.sessions[sessionID]; !ok {
		st.sessSvc.mu.Unlock()
		return sessionstore.ErrSessionNotFound
	}
	delete(st.sessSvc.sessions, sessionID)
	if st.sessSvc.current == sessionID {
		st.sessSvc.current = ""
	}
	st.sessSvc.mu.Unlock()

	st.msgSvc.mu.Lock()
	ids := st.msgSvc.bySession[sessionID]
	for _, id := range ids {
		delete(st.msgSvc.messages, id)
	}
	delete(st.msgSvc.bySession, sessionID)
	delete(st.msgSvc.loaded, sessionID)
	delete(st.msgSvc.titled, sessionID)
	st.msgSvc.mu.Unlock()

	_ = os.Remove(st.walPath(sessionID))
	return nil
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
		case entrySessionCreate, entrySessionTitle:
			if entry.Session != nil {
				sess = sessionstore.Session{
					ID:        entry.Session.ID,
					Title:     entry.Session.Title,
					CreatedAt: entry.Session.CreatedAt,
					UpdatedAt: entry.Session.UpdatedAt,
				}
			}
		}
	}
	return sess, scanner.Err()
}

// compactWAL rewrites the WAL file for a session to contain only the minimal
// entries needed to reconstruct current state: one session header entry and
// one msg.create entry per surviving message. This prevents unbounded growth.
func (st *Store) compactWAL(sessionID string, msgs []message.Message) {
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
		Session:   &walSession{ID: sess.ID, Title: sess.Title, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt},
	}
	headerLine, err := json.Marshal(headerEntry)
	if err != nil {
		return
	}

	lines := make([][]byte, 0, 1+len(msgs))
	lines = append(lines, headerLine)
	for _, m := range msgs {
		wm := messageToWalMsg(m)
		entry := walEntry{
			V:         walVersion,
			T:         m.CreatedAt,
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
func replayMessages(path string) ([]message.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	msgs := make(map[string]message.Message)
	order := make([]string, 0)

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
			m := walMsgToMessage(*entry.Msg)
			if _, seen := msgs[m.ID]; !seen {
				order = append(order, m.ID)
			}
			msgs[m.ID] = m
		case entryMsgUpdate:
			if entry.Msg == nil {
				continue
			}
			m := walMsgToMessage(*entry.Msg)
			msgs[m.ID] = m
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
			msgs = make(map[string]message.Message)
			order = order[:0]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	result := make([]message.Message, 0, len(order))
	for _, id := range order {
		if m, ok := msgs[id]; ok {
			result = append(result, m)
		}
	}
	return result, nil
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

// ---------------------------------------------------------------------------
// Part serialisation helpers
// ---------------------------------------------------------------------------

func messageToWalMsg(m message.Message) walMessage {
	wm := walMessage{
		ID:        m.ID,
		Role:      m.Role,
		SessionID: m.SessionID,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
		Model:     m.Model,
		Provider:  m.Provider,
	}
	for _, part := range m.Parts {
		wp, ok := encodeWalPart(part)
		if ok {
			wm.Parts = append(wm.Parts, wp)
		}
	}
	return wm
}

func walMsgToMessage(wm walMessage) message.Message {
	m := message.Message{
		ID:        wm.ID,
		Role:      wm.Role,
		SessionID: wm.SessionID,
		CreatedAt: wm.CreatedAt,
		UpdatedAt: wm.UpdatedAt,
		Model:     wm.Model,
		Provider:  wm.Provider,
	}
	for _, wp := range wm.Parts {
		part, ok := decodeWalPart(wp)
		if ok {
			m.Parts = append(m.Parts, part)
		}
	}
	return m
}

func encodeWalPart(part message.ContentPart) (walPart, bool) {
	var t walPartType
	switch part.(type) {
	case message.TextContent:
		t = walPartText
	case message.ReasoningContent:
		t = walPartReasoning
	case message.ToolCall:
		t = walPartToolCall
	case message.ToolResult:
		t = walPartToolResult
	case message.Finish:
		t = walPartFinish
	case message.BinaryContent:
		t = walPartBinary
	case message.SkillContent:
		t = walPartSkill
	default:
		return walPart{}, false
	}
	data, err := json.Marshal(part)
	if err != nil {
		return walPart{}, false
	}
	return walPart{T: t, Data: data}, true
}

func decodeWalPart(wp walPart) (message.ContentPart, bool) {
	switch wp.T {
	case walPartText:
		var v message.TextContent
		if json.Unmarshal(wp.Data, &v) != nil {
			return nil, false
		}
		return v, true
	case walPartReasoning:
		var v message.ReasoningContent
		if json.Unmarshal(wp.Data, &v) != nil {
			return nil, false
		}
		return v, true
	case walPartToolCall:
		var v message.ToolCall
		if json.Unmarshal(wp.Data, &v) != nil {
			return nil, false
		}
		return v, true
	case walPartToolResult:
		var v message.ToolResult
		if json.Unmarshal(wp.Data, &v) != nil {
			return nil, false
		}
		return v, true
	case walPartFinish:
		var v message.Finish
		if json.Unmarshal(wp.Data, &v) != nil {
			return nil, false
		}
		return v, true
	case walPartBinary:
		var v message.BinaryContent
		if json.Unmarshal(wp.Data, &v) != nil {
			return nil, false
		}
		return v, true
	case walPartSkill:
		var v message.SkillContent
		if json.Unmarshal(wp.Data, &v) != nil {
			return nil, false
		}
		return v, true
	}
	return nil, false
}
