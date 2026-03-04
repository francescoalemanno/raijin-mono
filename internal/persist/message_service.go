package persist

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/francescoalemanno/raijin-mono/internal/message"
)

// MessageService implements message.Service with WAL persistence.
// Messages are lazily loaded from WAL on the first List call for a session.
type MessageService struct {
	store *Store

	mu           sync.RWMutex
	messages     map[string]message.Message // id -> message
	bySession    map[string][]string        // sessionID -> []messageID (ordered)
	loaded       map[string]bool            // sessionID -> messages loaded from WAL
	titled       map[string]bool            // sessionID -> title has been set
	pendingFlush map[string]bool            // sessionID -> session.create not yet written to WAL
}

func newMessageService(st *Store) *MessageService {
	return &MessageService{
		store:        st,
		messages:     make(map[string]message.Message),
		bySession:    make(map[string][]string),
		loaded:       make(map[string]bool),
		titled:       make(map[string]bool),
		pendingFlush: make(map[string]bool),
	}
}

func lineageMessageID(sessionID, originalID string) string {
	return "lin:" + sessionID + ":" + originalID
}

func (ms *MessageService) resolveLineageMessages(sessionID string) ([]message.Message, error) {
	ms.store.sessSvc.mu.RLock()
	sess, ok := ms.store.sessSvc.sessions[sessionID]
	ms.store.sessSvc.mu.RUnlock()
	if !ok || sess.ParentSessionID == "" {
		return nil, nil
	}

	lineage, err := ms.resolveLineageMessages(sess.ParentSessionID)
	if err != nil {
		return nil, err
	}
	parentLocal, hadDeleteAll, err := replayMessages(ms.store.walPath(sess.ParentSessionID))
	if err != nil {
		return nil, err
	}
	parentMsgs := lineage
	if hadDeleteAll {
		parentMsgs = nil
	}
	parentMsgs = append(parentMsgs, parentLocal...)
	parentMsgs = message.SanitizeHistory(parentMsgs)

	if sess.ForkedFromMessageID == "" {
		return parentMsgs, nil
	}
	cut := len(parentMsgs)
	for i, m := range parentMsgs {
		if m.ID == sess.ForkedFromMessageID {
			cut = i
			break
		}
	}
	if cut < 0 {
		cut = 0
	}
	if cut > len(parentMsgs) {
		cut = len(parentMsgs)
	}
	result := make([]message.Message, 0, cut)
	for _, m := range parentMsgs[:cut] {
		clone := m.Clone()
		clone.ID = lineageMessageID(sessionID, m.ID)
		clone.SessionID = sessionID
		result = append(result, clone)
	}
	return result, nil
}

// ensureLoaded replays the WAL for sessionID if not yet loaded.
// For forked sessions, it materializes lineage messages from ancestors up to
// the fork point, then applies local WAL mutations on top.
// Caller must NOT hold ms.mu.
func (ms *MessageService) ensureLoaded(sessionID string) {
	ms.mu.RLock()
	done := ms.loaded[sessionID]
	ms.mu.RUnlock()
	if done {
		return
	}

	lineageMsgs, err := ms.resolveLineageMessages(sessionID)
	if err != nil {
		lineageMsgs = nil
	}

	localMsgs, hadDeleteAll, err := replayMessages(ms.store.walPath(sessionID))
	if err != nil {
		// Best-effort: treat as empty local WAL.
		localMsgs = nil
		hadDeleteAll = false
	}

	msgs := lineageMsgs
	if hadDeleteAll {
		msgs = nil
	}
	localMsgs = message.SanitizeHistory(localMsgs)
	msgs = append(msgs, localMsgs...)
	msgs = message.SanitizeHistory(msgs)

	// Compact only local session messages. For forks, lineage is inherited and
	// must not be copied into the child WAL.
	if len(localMsgs) > 0 {
		ms.store.compactWAL(sessionID, localMsgs)
	}

	ms.store.sessSvc.mu.RLock()
	sess, ok := ms.store.sessSvc.sessions[sessionID]
	hasTitle := ok && strings.TrimSpace(sess.Title) != ""
	ms.store.sessSvc.mu.RUnlock()

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.loaded[sessionID] {
		return // another goroutine beat us
	}
	for _, m := range msgs {
		ms.messages[m.ID] = m
		ms.bySession[sessionID] = append(ms.bySession[sessionID], m.ID)
	}
	ms.titled[sessionID] = hasTitle
	ms.loaded[sessionID] = true
}

// flushSession writes the session.create WAL entry for an ephemeral session
// the first time any message is stored for it, making it durable.
func (ms *MessageService) flushSession(sessionID string) {
	ms.mu.Lock()
	pending := ms.pendingFlush[sessionID]
	if pending {
		delete(ms.pendingFlush, sessionID)
	}
	ms.mu.Unlock()

	if !pending {
		return
	}

	ms.store.sessSvc.mu.RLock()
	sess, ok := ms.store.sessSvc.sessions[sessionID]
	ms.store.sessSvc.mu.RUnlock()
	if !ok {
		return
	}

	_ = ms.store.appendEntry(sessionID, walEntry{
		Typ: entrySessionCreate,
		Session: &walSession{
			ID:                  sess.ID,
			Title:               sess.Title,
			ParentSessionID:     sess.ParentSessionID,
			ForkedFromMessageID: sess.ForkedFromMessageID,
			CreatedAt:           sess.CreatedAt,
			UpdatedAt:           sess.UpdatedAt,
		},
	})
	ms.store.saveState(sessionID)
}

// Create adds a new message, appends a msg.create WAL entry, and derives
// the session title from the first user message.
func (ms *MessageService) Create(ctx context.Context, sessionID string, params message.CreateParams) (message.Message, error) {
	ms.ensureLoaded(sessionID)

	// Flush ephemeral session to WAL on first message.
	ms.flushSession(sessionID)

	now := time.Now().Unix()
	msg := message.Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      params.Role,
		Parts:     params.Parts,
		Model:     params.Model,
		Provider:  params.Provider,
		CreatedAt: now,
		UpdatedAt: now,
	}

	wm := messageToWalMsg(msg)
	if err := ms.store.appendEntry(sessionID, walEntry{
		Typ: entryMsgCreate,
		Msg: &wm,
	}); err != nil {
		return message.Message{}, err
	}

	text := strings.TrimSpace(msg.Content().Text)

	ms.mu.Lock()
	ms.messages[msg.ID] = msg
	ms.bySession[sessionID] = append(ms.bySession[sessionID], msg.ID)
	titled := ms.titled[sessionID]
	if params.Role == message.User && !titled && text != "" {
		ms.titled[sessionID] = true
	}
	ms.mu.Unlock()

	// Derive session title from first non-empty user message (outside the lock).
	if params.Role == message.User && !titled && text != "" {
		title := TitleFromFirstUserMessage(text)
		ms.store.sessSvc.setTitle(ctx, sessionID, title)
	}

	return msg.Clone(), nil
}

// Update modifies an existing message and appends a msg.update WAL entry.
func (ms *MessageService) Update(ctx context.Context, msg message.Message) error {
	ms.mu.Lock()
	if _, ok := ms.messages[msg.ID]; !ok {
		ms.mu.Unlock()
		return message.ErrMessageNotFound
	}
	msg.UpdatedAt = time.Now().Unix()
	ms.messages[msg.ID] = msg.Clone()
	sessionID := msg.SessionID
	ms.mu.Unlock()

	wm := messageToWalMsg(msg)
	return ms.store.appendEntry(sessionID, walEntry{
		Typ: entryMsgUpdate,
		Msg: &wm,
	})
}

// Get retrieves a message by ID.
func (ms *MessageService) Get(ctx context.Context, id string) (message.Message, error) {
	ms.mu.RLock()
	msg, ok := ms.messages[id]
	ms.mu.RUnlock()
	if !ok {
		return message.Message{}, message.ErrMessageNotFound
	}
	return msg.Clone(), nil
}

// List returns all messages for a session in creation order, replaying from
// WAL if necessary.
func (ms *MessageService) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	ms.ensureLoaded(sessionID)

	ms.mu.RLock()
	defer ms.mu.RUnlock()

	ids := ms.bySession[sessionID]
	result := make([]message.Message, 0, len(ids))
	for _, id := range ids {
		if m, ok := ms.messages[id]; ok {
			result = append(result, m.Clone())
		}
	}
	return result, nil
}

// Delete removes a message and appends a msg.delete WAL entry.
func (ms *MessageService) Delete(ctx context.Context, id string) error {
	ms.mu.Lock()
	msg, ok := ms.messages[id]
	if !ok {
		ms.mu.Unlock()
		return message.ErrMessageNotFound
	}
	sessionID := msg.SessionID
	delete(ms.messages, id)
	ids := ms.bySession[sessionID]
	for i, mid := range ids {
		if mid == id {
			ms.bySession[sessionID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	ms.mu.Unlock()

	return ms.store.appendEntry(sessionID, walEntry{
		Typ:   entryMsgDelete,
		MsgID: id,
	})
}

// DeleteAll removes all messages for a session and appends a msg.delete_all
// WAL entry.
func (ms *MessageService) DeleteAll(ctx context.Context, sessionID string) error {
	ms.mu.Lock()
	ids := ms.bySession[sessionID]
	for _, id := range ids {
		delete(ms.messages, id)
	}
	delete(ms.bySession, sessionID)
	ms.loaded[sessionID] = true // mark loaded so we don't replay a deleted session
	ms.mu.Unlock()

	return ms.store.appendEntry(sessionID, walEntry{
		Typ: entryMsgDeleteAll,
	})
}
