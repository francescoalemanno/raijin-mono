package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	ID        string
	Title     string
	CreatedAt int64
	UpdatedAt int64
}

type Service interface {
	Create(ctx context.Context) (Session, error)
	Get(ctx context.Context, id string) (Session, error)
	Update(ctx context.Context, sess Session) error
	Delete(ctx context.Context, id string) error
	Current(ctx context.Context) (Session, error)
	SetCurrent(ctx context.Context, id string) error
	Reset(ctx context.Context) (Session, error)
}

type InMemoryService struct {
	mu       sync.RWMutex
	sessions map[string]Session
	current  string // current active session ID
}

func NewInMemoryService() *InMemoryService {
	return &InMemoryService{
		sessions: make(map[string]Session),
	}
}

func (s *InMemoryService) Create(ctx context.Context) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	sess := Session{
		ID:        uuid.New().String(),
		Title:     "Untitled Session",
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.sessions[sess.ID] = sess
	s.current = sess.ID

	return sess, nil
}

func (s *InMemoryService) Get(ctx context.Context, id string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, exists := s.sessions[id]
	if !exists {
		return Session{}, ErrSessionNotFound
	}
	return sess, nil
}

func (s *InMemoryService) Update(ctx context.Context, sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[sess.ID]; !exists {
		return ErrSessionNotFound
	}

	sess.UpdatedAt = time.Now().Unix()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *InMemoryService) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[id]; !exists {
		return ErrSessionNotFound
	}

	delete(s.sessions, id)
	if s.current == id {
		s.current = ""
	}
	return nil
}

func (s *InMemoryService) Current(ctx context.Context) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.current == "" {
		return Session{}, ErrSessionNotFound
	}

	sess, exists := s.sessions[s.current]
	if !exists {
		return Session{}, ErrSessionNotFound
	}
	return sess, nil
}

func (s *InMemoryService) SetCurrent(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[id]; !exists {
		return ErrSessionNotFound
	}

	s.current = id
	return nil
}

func (s *InMemoryService) Reset(ctx context.Context) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = make(map[string]Session)
	s.current = ""

	now := time.Now().Unix()
	sess := Session{
		ID:        uuid.New().String(),
		Title:     "Untitled Session",
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.sessions[sess.ID] = sess
	s.current = sess.ID

	return sess, nil
}
