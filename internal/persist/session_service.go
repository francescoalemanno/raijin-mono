package persist

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	sessionstore "github.com/francescoalemanno/raijin-mono/internal/session"
)

// SessionService implements session.Service with WAL persistence.
type SessionService struct {
	store    *Store
	mu       sync.RWMutex
	sessions map[string]sessionstore.Session
	current  string
}

func newSessionService(st *Store) *SessionService {
	return &SessionService{
		store:    st,
		sessions: make(map[string]sessionstore.Session),
	}
}

// Create creates a new session and appends a session.create WAL entry.
func (s *SessionService) Create(ctx context.Context) (sessionstore.Session, error) {
	now := time.Now().Unix()
	sess := sessionstore.Session{
		ID:        uuid.New().String(),
		Title:     "",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.appendEntry(sess.ID, walEntry{
		Typ:     entrySessionCreate,
		Session: &walSession{ID: sess.ID, Title: sess.Title, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt},
	}); err != nil {
		return sessionstore.Session{}, err
	}

	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.current = sess.ID
	s.mu.Unlock()

	s.store.saveState(sess.ID)
	return sess, nil
}

// Get retrieves a session by ID.
func (s *SessionService) Get(ctx context.Context, id string) (sessionstore.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return sessionstore.Session{}, sessionstore.ErrSessionNotFound
	}
	return sess, nil
}

// Update updates session metadata and appends a session.title WAL entry.
func (s *SessionService) Update(ctx context.Context, sess sessionstore.Session) error {
	s.mu.Lock()
	if _, ok := s.sessions[sess.ID]; !ok {
		s.mu.Unlock()
		return sessionstore.ErrSessionNotFound
	}
	sess.UpdatedAt = time.Now().Unix()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()

	return s.store.appendEntry(sess.ID, walEntry{
		Typ:     entrySessionTitle,
		Session: &walSession{ID: sess.ID, Title: sess.Title, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt},
	})
}

// Delete removes a session (in-memory only; the WAL file is left on disk).
func (s *SessionService) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return sessionstore.ErrSessionNotFound
	}
	delete(s.sessions, id)
	if s.current == id {
		s.current = ""
	}
	return nil
}

// Current returns the active session.
func (s *SessionService) Current(ctx context.Context) (sessionstore.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == "" {
		return sessionstore.Session{}, sessionstore.ErrSessionNotFound
	}
	sess, ok := s.sessions[s.current]
	if !ok {
		return sessionstore.Session{}, sessionstore.ErrSessionNotFound
	}
	return sess, nil
}

// SetCurrent changes the active session and persists the choice.
func (s *SessionService) SetCurrent(ctx context.Context, id string) error {
	s.mu.Lock()
	if _, ok := s.sessions[id]; !ok {
		s.mu.Unlock()
		return sessionstore.ErrSessionNotFound
	}
	s.current = id
	s.mu.Unlock()
	s.store.saveState(id)
	return nil
}

// Reset clears all sessions and creates a fresh one (satisfies interface;
// the TUI should prefer Create+SetCurrent to preserve history).
func (s *SessionService) Reset(ctx context.Context) (sessionstore.Session, error) {
	return s.Create(ctx)
}

// setTitle updates a session's title both in memory and in the WAL.
// This is an internal helper called by MessageService on first user message.
func (s *SessionService) setTitle(ctx context.Context, sessionID, title string) {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return
	}
	if sess.Title != "" {
		s.mu.Unlock()
		return // already titled
	}
	sess.Title = title
	sess.UpdatedAt = time.Now().Unix()
	s.sessions[sessionID] = sess
	s.mu.Unlock()

	_ = s.store.appendEntry(sessionID, walEntry{
		Typ:     entrySessionTitle,
		Session: &walSession{ID: sess.ID, Title: sess.Title, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt},
	})
}

// summariesLocked returns all session summaries sorted by UpdatedAt desc.
// Caller must hold st.mu. Ephemeral (not-yet-flushed) sessions are excluded.
func (s *SessionService) summariesLocked() []SessionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.store.msgSvc.mu.RLock()
	pending := s.store.msgSvc.pendingFlush
	s.store.msgSvc.mu.RUnlock()

	out := make([]SessionSummary, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if pending[sess.ID] {
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
