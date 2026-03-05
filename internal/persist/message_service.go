package persist

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

// MessageService implements libagent.MessageService with WAL persistence.
// Messages are lazily loaded from WAL on the first List call for a session.
type MessageService struct {
	store *Store

	mu           sync.RWMutex
	messages     map[string]libagent.Message // id -> message
	bySession    map[string][]string         // sessionID -> []messageID (ordered)
	loaded       map[string]bool             // sessionID -> messages loaded from WAL
	titled       map[string]bool             // sessionID -> title has been set
	pendingFlush map[string]bool             // sessionID -> session.create not yet written to WAL
}

func newMessageService(st *Store) *MessageService {
	return &MessageService{
		store:        st,
		messages:     make(map[string]libagent.Message),
		bySession:    make(map[string][]string),
		loaded:       make(map[string]bool),
		titled:       make(map[string]bool),
		pendingFlush: make(map[string]bool),
	}
}

func lineageMessageID(sessionID, originalID string) string {
	return "lin:" + sessionID + ":" + originalID
}

func (ms *MessageService) resolveLineageMessages(sessionID string) ([]libagent.Message, error) {
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
	parentMsgs = libagent.SanitizeHistory(parentMsgs)

	if sess.ForkedFromMessageID == "" {
		return parentMsgs, nil
	}
	cut := len(parentMsgs)
	for i, m := range parentMsgs {
		if libagent.MessageID(m) == sess.ForkedFromMessageID {
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
	result := make([]libagent.Message, 0, cut)
	for _, m := range parentMsgs[:cut] {
		clone := libagent.CloneMessage(m)
		meta := libagent.MessageMetaOf(clone)
		meta.ID = lineageMessageID(sessionID, meta.ID)
		meta.SessionID = sessionID
		libagent.SetMessageMeta(clone, meta)
		result = append(result, clone)
	}
	return result, nil
}

// ensureLoaded replays the WAL for sessionID if not yet loaded.
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
		localMsgs = nil
		hadDeleteAll = false
	}

	msgs := lineageMsgs
	if hadDeleteAll {
		msgs = nil
	}
	localMsgs = libagent.SanitizeHistory(localMsgs)
	msgs = append(msgs, localMsgs...)
	msgs = libagent.SanitizeHistory(msgs)

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
		return
	}
	for _, m := range msgs {
		id := libagent.MessageID(m)
		if id == "" {
			continue
		}
		ms.messages[id] = m
		ms.bySession[sessionID] = append(ms.bySession[sessionID], id)
	}
	ms.titled[sessionID] = hasTitle
	ms.loaded[sessionID] = true
}

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

func (ms *MessageService) Create(ctx context.Context, sessionID string, msg libagent.Message) (libagent.Message, error) {
	ms.ensureLoaded(sessionID)
	ms.flushSession(sessionID)

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

	wm := messageToWalMsg(toStore)
	if err := ms.store.appendEntry(sessionID, walEntry{Typ: entryMsgCreate, Msg: &wm}); err != nil {
		return nil, err
	}

	firstUserText := ""
	if um, ok := toStore.(*libagent.UserMessage); ok {
		firstUserText = strings.TrimSpace(um.Content)
	}

	ms.mu.Lock()
	ms.messages[meta.ID] = toStore
	ms.bySession[sessionID] = append(ms.bySession[sessionID], meta.ID)
	titled := ms.titled[sessionID]
	if _, ok := toStore.(*libagent.UserMessage); ok && !titled && firstUserText != "" {
		ms.titled[sessionID] = true
	}
	ms.mu.Unlock()

	if _, ok := toStore.(*libagent.UserMessage); ok && !titled && firstUserText != "" {
		title := TitleFromFirstUserMessage(firstUserText)
		ms.store.sessSvc.setTitle(ctx, sessionID, title)
	}

	return libagent.CloneMessage(toStore), nil
}

func (ms *MessageService) Update(_ context.Context, msg libagent.Message) error {
	id := libagent.MessageID(msg)
	if id == "" {
		return libagent.ErrMessageNotFound
	}

	ms.mu.Lock()
	if _, ok := ms.messages[id]; !ok {
		ms.mu.Unlock()
		return libagent.ErrMessageNotFound
	}
	meta := libagent.MessageMetaOf(msg)
	meta.UpdatedAt = time.Now().Unix()
	toStore := libagent.CloneMessage(msg)
	libagent.SetMessageMeta(toStore, meta)
	ms.messages[id] = toStore
	sessionID := meta.SessionID
	ms.mu.Unlock()

	wm := messageToWalMsg(toStore)
	return ms.store.appendEntry(sessionID, walEntry{Typ: entryMsgUpdate, Msg: &wm})
}

func (ms *MessageService) Get(_ context.Context, id string) (libagent.Message, error) {
	ms.mu.RLock()
	msg, ok := ms.messages[id]
	ms.mu.RUnlock()
	if !ok {
		return nil, libagent.ErrMessageNotFound
	}
	return libagent.CloneMessage(msg), nil
}

func (ms *MessageService) List(_ context.Context, sessionID string) ([]libagent.Message, error) {
	ms.ensureLoaded(sessionID)

	ms.mu.RLock()
	defer ms.mu.RUnlock()

	ids := ms.bySession[sessionID]
	result := make([]libagent.Message, 0, len(ids))
	for _, id := range ids {
		if m, ok := ms.messages[id]; ok {
			result = append(result, libagent.CloneMessage(m))
		}
	}
	return result, nil
}

func (ms *MessageService) Delete(_ context.Context, id string) error {
	ms.mu.Lock()
	msg, ok := ms.messages[id]
	if !ok {
		ms.mu.Unlock()
		return libagent.ErrMessageNotFound
	}
	meta := libagent.MessageMetaOf(msg)
	sessionID := meta.SessionID
	delete(ms.messages, id)
	ids := ms.bySession[sessionID]
	for i, mid := range ids {
		if mid == id {
			ms.bySession[sessionID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	ms.mu.Unlock()

	return ms.store.appendEntry(sessionID, walEntry{Typ: entryMsgDelete, MsgID: id})
}

func (ms *MessageService) DeleteAll(_ context.Context, sessionID string) error {
	ms.mu.Lock()
	ids := ms.bySession[sessionID]
	for _, id := range ids {
		delete(ms.messages, id)
	}
	delete(ms.bySession, sessionID)
	ms.loaded[sessionID] = true
	ms.mu.Unlock()

	return ms.store.appendEntry(sessionID, walEntry{Typ: entryMsgDeleteAll})
}
