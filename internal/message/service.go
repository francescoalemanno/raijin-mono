package message

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type CreateParams struct {
	Role     MessageRole
	Parts    []ContentPart
	Model    string
	Provider string
}

// Service defines the interface for message storage operations.
type Service interface {
	Create(ctx context.Context, sessionID string, params CreateParams) (Message, error)
	Update(ctx context.Context, msg Message) error
	Get(ctx context.Context, id string) (Message, error)
	List(ctx context.Context, sessionID string) ([]Message, error)
	Delete(ctx context.Context, id string) error
	DeleteAll(ctx context.Context, sessionID string) error
}

type InMemoryService struct {
	mu        sync.RWMutex
	messages  map[string]Message  // id -> message
	bySession map[string][]string // sessionID -> []messageID (ordered)
}

func NewInMemoryService() *InMemoryService {
	return &InMemoryService{
		messages:  make(map[string]Message),
		bySession: make(map[string][]string),
	}
}

func (s *InMemoryService) Create(ctx context.Context, sessionID string, params CreateParams) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	msg := Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      params.Role,
		Parts:     params.Parts,
		Model:     params.Model,
		Provider:  params.Provider,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.messages[msg.ID] = msg
	s.bySession[sessionID] = append(s.bySession[sessionID], msg.ID)

	return msg.Clone(), nil
}

func (s *InMemoryService) Update(ctx context.Context, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.messages[msg.ID]; !exists {
		return ErrMessageNotFound
	}

	msg.UpdatedAt = time.Now().Unix()
	s.messages[msg.ID] = msg.Clone()
	return nil
}

func (s *InMemoryService) Get(ctx context.Context, id string) (Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg, exists := s.messages[id]
	if !exists {
		return Message{}, ErrMessageNotFound
	}
	return msg.Clone(), nil
}

func (s *InMemoryService) List(ctx context.Context, sessionID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.bySession[sessionID]
	result := make([]Message, 0, len(ids))
	for _, id := range ids {
		if msg, exists := s.messages[id]; exists {
			result = append(result, msg.Clone())
		}
	}
	return result, nil
}

func (s *InMemoryService) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg, exists := s.messages[id]
	if !exists {
		return ErrMessageNotFound
	}

	delete(s.messages, id)

	// Remove from session index
	ids := s.bySession[msg.SessionID]
	for i, mid := range ids {
		if mid == id {
			s.bySession[msg.SessionID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}

	return nil
}

func (s *InMemoryService) DeleteAll(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := s.bySession[sessionID]
	for _, id := range ids {
		delete(s.messages, id)
	}
	delete(s.bySession, sessionID)

	return nil
}
