package libagent

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type InMemoryMessageService struct {
	mu        sync.RWMutex
	messages  map[string]Message
	bySession map[string][]string
}

func NewInMemoryMessageService() *InMemoryMessageService {
	return &InMemoryMessageService{
		messages:  make(map[string]Message),
		bySession: make(map[string][]string),
	}
}

func (s *InMemoryMessageService) Create(_ context.Context, sessionID string, msg Message) (Message, error) {
	now := time.Now().Unix()
	meta := MessageMetaOf(msg)
	if meta.ID == "" {
		meta.ID = uuid.New().String()
	}
	meta.SessionID = sessionID
	if meta.CreatedAt == 0 {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now
	clone := CloneMessage(msg)
	SetMessageMeta(clone, meta)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[meta.ID] = clone
	s.bySession[sessionID] = append(s.bySession[sessionID], meta.ID)
	return CloneMessage(clone), nil
}

func (s *InMemoryMessageService) Update(_ context.Context, msg Message) error {
	id := MessageID(msg)
	if id == "" {
		return ErrMessageNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.messages[id]; !ok {
		return ErrMessageNotFound
	}
	meta := MessageMetaOf(msg)
	meta.UpdatedAt = time.Now().Unix()
	clone := CloneMessage(msg)
	SetMessageMeta(clone, meta)
	s.messages[id] = clone
	return nil
}

func (s *InMemoryMessageService) Get(_ context.Context, id string) (Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msg, ok := s.messages[id]
	if !ok {
		return nil, ErrMessageNotFound
	}
	return CloneMessage(msg), nil
}

func (s *InMemoryMessageService) List(_ context.Context, sessionID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.bySession[sessionID]
	out := make([]Message, 0, len(ids))
	for _, id := range ids {
		if m, ok := s.messages[id]; ok {
			out = append(out, CloneMessage(m))
		}
	}
	return out, nil
}

func (s *InMemoryMessageService) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg, ok := s.messages[id]
	if !ok {
		return ErrMessageNotFound
	}
	meta := MessageMetaOf(msg)
	delete(s.messages, id)
	ids := s.bySession[meta.SessionID]
	for i, mid := range ids {
		if mid == id {
			s.bySession[meta.SessionID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	return nil
}

func (s *InMemoryMessageService) DeleteAll(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.bySession[sessionID]
	for _, id := range ids {
		delete(s.messages, id)
	}
	delete(s.bySession, sessionID)
	return nil
}
