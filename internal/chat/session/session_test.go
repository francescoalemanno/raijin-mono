package session

import (
	"context"
	"errors"
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/agent"
	"github.com/francescoalemanno/raijin-mono/internal/message"
	sessionstore "github.com/francescoalemanno/raijin-mono/internal/session"
)

type stubMessageService struct {
	deleteAllErr       error
	deleteAllSessionID string
	deleteAllCalls     int
	calls              *[]string
}

func (s *stubMessageService) Create(ctx context.Context, sessionID string, params message.CreateParams) (message.Message, error) {
	return message.Message{}, nil
}

func (s *stubMessageService) Update(ctx context.Context, msg message.Message) error {
	return nil
}

func (s *stubMessageService) Get(ctx context.Context, id string) (message.Message, error) {
	return message.Message{}, nil
}

func (s *stubMessageService) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	return nil, nil
}

func (s *stubMessageService) Delete(ctx context.Context, id string) error {
	return nil
}

func (s *stubMessageService) DeleteAll(ctx context.Context, sessionID string) error {
	s.deleteAllCalls++
	s.deleteAllSessionID = sessionID
	if s.calls != nil {
		*s.calls = append(*s.calls, "delete_all")
	}
	return s.deleteAllErr
}

type stubSessionService struct {
	resetErr     error
	resetSession sessionstore.Session
	resetCalls   int
	calls        *[]string
}

func (s *stubSessionService) Create(ctx context.Context) (sessionstore.Session, error) {
	return sessionstore.Session{}, nil
}

func (s *stubSessionService) Get(ctx context.Context, id string) (sessionstore.Session, error) {
	return sessionstore.Session{}, nil
}

func (s *stubSessionService) Update(ctx context.Context, sess sessionstore.Session) error {
	return nil
}

func (s *stubSessionService) Delete(ctx context.Context, id string) error {
	return nil
}

func (s *stubSessionService) Current(ctx context.Context) (sessionstore.Session, error) {
	return sessionstore.Session{}, nil
}

func (s *stubSessionService) SetCurrent(ctx context.Context, id string) error {
	return nil
}

func (s *stubSessionService) Reset(ctx context.Context) (sessionstore.Session, error) {
	s.resetCalls++
	if s.calls != nil {
		*s.calls = append(*s.calls, "reset")
	}
	if s.resetErr != nil {
		return sessionstore.Session{}, s.resetErr
	}
	return s.resetSession, nil
}

func TestResetBackendSessionStopsOnDeleteAllError(t *testing.T) {
	t.Parallel()

	calls := []string{}
	msgErr := errors.New("delete all failed")
	msgSvc := &stubMessageService{deleteAllErr: msgErr, calls: &calls}
	sessSvc := &stubSessionService{resetSession: sessionstore.Session{ID: "new-id"}, calls: &calls}

	ag := agent.NewSessionAgent(agent.SessionAgentOptions{Messages: msgSvc, Sessions: sessSvc})
	s := &Session{agent: ag, id: "old-id"}

	err := s.resetBackendSession(context.Background())
	if !errors.Is(err, msgErr) {
		t.Fatalf("expected delete-all error, got %v", err)
	}
	if sessSvc.resetCalls != 0 {
		t.Fatalf("expected reset not to be called, got %d calls", sessSvc.resetCalls)
	}
	if s.id != "old-id" {
		t.Fatalf("expected session id to remain unchanged, got %q", s.id)
	}
	if len(calls) != 1 || calls[0] != "delete_all" {
		t.Fatalf("unexpected call order: %v", calls)
	}
}

func TestResetBackendSessionDeletesMessagesBeforeReset(t *testing.T) {
	t.Parallel()

	calls := []string{}
	msgSvc := &stubMessageService{calls: &calls}
	sessSvc := &stubSessionService{resetSession: sessionstore.Session{ID: "new-id"}, calls: &calls}

	ag := agent.NewSessionAgent(agent.SessionAgentOptions{Messages: msgSvc, Sessions: sessSvc})
	s := &Session{agent: ag, id: "old-id"}

	err := s.resetBackendSession(context.Background())
	if err != nil {
		t.Fatalf("resetBackendSession returned error: %v", err)
	}
	if msgSvc.deleteAllCalls != 1 {
		t.Fatalf("expected delete-all to be called once, got %d", msgSvc.deleteAllCalls)
	}
	if msgSvc.deleteAllSessionID != "old-id" {
		t.Fatalf("expected delete-all to use old session id, got %q", msgSvc.deleteAllSessionID)
	}
	if sessSvc.resetCalls != 1 {
		t.Fatalf("expected reset to be called once, got %d", sessSvc.resetCalls)
	}
	if s.id != "new-id" {
		t.Fatalf("expected new session id to be set, got %q", s.id)
	}
	if len(calls) != 2 || calls[0] != "delete_all" || calls[1] != "reset" {
		t.Fatalf("unexpected call order: %v", calls)
	}
}
