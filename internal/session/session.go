package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"

	"github.com/francescoalemanno/raijin-mono/internal/agent"
	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
)

// Session owns chat session lifecycle: agent wiring, tool registration,
// and session reset.
type Session struct {
	paths *tools.PathRegistry

	agent        *agent.SessionAgent
	id           string
	agentTools   []libagent.Tool
	eventCB      func(libagent.AgentEvent)
	persistStore *persist.Store
	binding      *persist.Binding
}

// New creates and initializes a chat session runtime.
func New(runtimeModel libagent.RuntimeModel) (*Session, error) {
	paths := tools.NewPathRegistry()
	s := &Session{
		paths: paths,
	}

	store, err := persist.OpenStore()
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}
	s.persistStore = store

	if runtimeModel.Model != nil {
		if err := s.Reconfigure(runtimeModel); err != nil {
			s.refreshRuntime()
			return s, err
		}
	} else {
		s.refreshRuntime()
	}

	return s, nil
}

func (s *Session) Agent() *agent.SessionAgent { return s.agent }
func (s *Session) ID() string                 { return s.id }
func (s *Session) Tools() []libagent.Tool     { return s.agentTools }
func (s *Session) Paths() *tools.PathRegistry { return s.paths }

// Bind attaches the session to an explicit binding context.
func (s *Session) Bind(ctx context.Context, forceNew, createIfMissing bool) error {
	_ = ctx
	if s.persistStore == nil {
		return errors.New("session store not available")
	}
	key := strings.TrimSpace(osBindingKey())
	if key == "" {
		return errors.New("conversation commands require a bound context; use the REPL or shell integration")
	}

	ownerPID, err := osBindingOwnerPID()
	if err != nil {
		return err
	}

	if forceNew {
		s.binding = &persist.Binding{Key: key, OwnerPID: ownerPID}
		return s.newBackendSession(context.Background())
	}

	binding, err := s.persistStore.LoadBinding(key)
	if err != nil && !errors.Is(err, persist.ErrBindingNotFound) {
		return err
	}
	if errors.Is(err, persist.ErrBindingNotFound) {
		s.binding = &persist.Binding{Key: key, OwnerPID: ownerPID}
		if !createIfMissing {
			return nil
		}
		return s.newBackendSession(context.Background())
	}

	binding.Key = key
	binding.OwnerPID = ownerPID
	s.binding = &binding

	// Check whether the session journal exists on disk. If it does, load it
	// properly so the tree and leaf pointer are recovered. Otherwise treat it
	// as a fresh ephemeral session.
	if _, err := s.persistStore.GetSession(binding.SessionID); err == nil {
		if err := s.persistStore.OpenSession(binding.SessionID); err != nil {
			return err
		}
		s.id = binding.SessionID
		return nil
	}
	sess, err := s.persistStore.CreateEphemeralWithID(binding.SessionID, binding.SessionCreatedAt)
	if err != nil {
		return err
	}
	s.id = sess.ID
	return nil
}

// ListMessages returns all stored messages for the current backend session.
func (s *Session) ListMessages(ctx context.Context) ([]libagent.Message, error) {
	if s.id == "" {
		return nil, nil
	}
	return s.persistStore.Messages().List(ctx, s.id)
}

// SetEventCallback updates event streaming callback.
func (s *Session) SetEventCallback(cb func(libagent.AgentEvent)) {
	s.eventCB = cb
	if s.agent != nil {
		s.agent.SetEventCallback(cb)
	}
}

// Reconfigure rebuilds the agent from a RuntimeModel while preserving service state.
func (s *Session) Reconfigure(runtimeModel libagent.RuntimeModel) error {
	ag, err := agent.NewSessionAgentFromConfig(
		runtimeModel,
		s.persistStore.Messages(),
		s.persistStore,
	)
	if err != nil {
		return err
	}

	s.agent = ag
	s.refreshRuntime()
	return nil
}

// Clear starts a new empty backend session, preserving history of prior sessions.
func (s *Session) Clear(ctx context.Context) error {
	if s.agent == nil {
		return nil
	}
	if err := s.newBackendSession(ctx); err != nil {
		return err
	}
	if err := artifacts.Reload(); err != nil {
		return err
	}
	s.paths = tools.NewPathRegistry()
	s.refreshRuntime()
	return nil
}

// Navigate moves the leaf pointer to targetID within the current session's tree.
// If the target is a user message, it returns the message text for editor pre-population.
func (s *Session) Navigate(targetID string) (editorText string, err error) {
	editorText, err = s.persistStore.Navigate(targetID)
	if err != nil {
		return "", err
	}
	return editorText, s.syncBinding()
}

// SetLeaf moves the active session leaf to a specific stored message without
// applying /tree navigation rewrites for user messages.
func (s *Session) SetLeaf(targetID string) error {
	if err := s.persistStore.SetLeaf(targetID); err != nil {
		return err
	}
	return s.syncBinding()
}

// GetTree returns the flat tree entry list for the current session.
func (s *Session) GetTree() []persist.TreeEntry {
	return s.persistStore.GetTree()
}

// SwitchTo loads a previously persisted session and makes it current.
func (s *Session) SwitchTo(ctx context.Context, sessionID string) error {
	_ = ctx
	if err := s.persistStore.OpenSession(sessionID); err != nil {
		return err
	}
	s.id = sessionID
	if s.binding != nil {
		s.binding.SessionID = sessionID
		if persisted, err := s.persistStore.GetSession(sessionID); err == nil {
			s.binding.SessionCreatedAt = persisted.CreatedAt
			s.binding.SessionUpdatedAt = persisted.UpdatedAt
		}
	}
	return s.syncBinding()
}

// ListSessionSummaries returns persisted sessions, newest first.
func (s *Session) ListSessionSummaries() []persist.SessionSummary {
	return s.persistStore.ListSessionSummaries()
}

// RemoveSession permanently deletes a non-active persisted session.
func (s *Session) RemoveSession(sessionID string) error {
	if err := s.persistStore.RemoveSession(sessionID); err != nil {
		return err
	}
	if s.id == sessionID {
		s.id = ""
	}
	return nil
}

// AppendCompaction stores a compaction checkpoint for the active session.
func (s *Session) AppendCompaction(summary, firstKeptID string, tokensBefore int64) error {
	if err := s.persistStore.AppendCompaction(summary, firstKeptID, tokensBefore); err != nil {
		return err
	}
	return s.syncBinding()
}

func (s *Session) AppendCompactionWithEvents(start libagent.ContextCompactionEvent, summary, firstKeptID string, tokensBefore int64, end libagent.ContextCompactionEvent) error {
	if err := s.persistStore.AppendCompactionWithEvents(start, summary, firstKeptID, tokensBefore, end); err != nil {
		return err
	}
	return s.syncBinding()
}

func (s *Session) ListReplayItems() ([]persist.ReplayItem, error) {
	if s.id == "" {
		return nil, nil
	}
	return s.persistStore.ListReplayItems(s.id)
}

// EnsurePersisted flushes the session header if the current session is still
// ephemeral (i.e. has no messages yet).
func (s *Session) EnsurePersisted() error {
	if s.persistStore == nil || s.id == "" {
		return nil
	}
	if err := s.persistStore.EnsureSessionPersisted(s.id); err != nil {
		return err
	}
	return s.syncBinding()
}

// HasHistory returns true when current session has at least one stored message.
func (s *Session) HasHistory(ctx context.Context) (bool, error) {
	if s.id == "" {
		return false, nil
	}
	msgs, err := s.persistStore.Messages().List(ctx, s.id)
	if err != nil {
		return false, err
	}
	return len(msgs) > 0, nil
}

func (s *Session) registerTools() {
	s.agentTools = tools.RegisterDefaultTools(s.paths, s.agent)
	if s.agent != nil {
		s.agent.SetTools(s.agentTools)
	}
}

func (s *Session) newBackendSession(ctx context.Context) error {
	_ = ctx
	sess, err := s.persistStore.CreateEphemeral()
	if err != nil {
		return err
	}
	s.id = sess.ID
	if s.binding != nil {
		s.binding.SessionID = sess.ID
		s.binding.SessionCreatedAt = sess.CreatedAt
		s.binding.SessionUpdatedAt = sess.UpdatedAt
	}
	return s.syncBinding()
}

func (s *Session) refreshRuntime() {
	if s.agent != nil {
		s.agent.SetSystemPrompt(agent.BuildSystemPrompt())
	}
	s.registerTools()
	s.SetEventCallback(s.eventCB)
}

func (s *Session) syncBinding() error {
	if s.persistStore == nil || s.binding == nil || strings.TrimSpace(s.binding.Key) == "" || s.id == "" {
		return nil
	}
	s.binding.SessionID = s.id
	if sess, err := s.persistStore.GetSession(s.id); err == nil {
		s.binding.SessionCreatedAt = sess.CreatedAt
		s.binding.SessionUpdatedAt = sess.UpdatedAt
	} else {
		s.binding.SessionUpdatedAt = max(s.binding.SessionUpdatedAt, s.binding.SessionCreatedAt)
		if s.binding.SessionCreatedAt == 0 {
			s.binding.SessionCreatedAt = s.binding.SessionUpdatedAt
		}
	}
	return s.persistStore.SaveBinding(*s.binding)
}

func osBindingKey() string {
	return strings.TrimSpace(getenv(persist.SessionBindingKeyEnv))
}

func osBindingOwnerPID() (int, error) {
	raw := strings.TrimSpace(getenv(persist.SessionBindingOwnerPIDEnv))
	if raw == "" {
		return 0, errors.New("conversation commands require a bound context; missing binding owner")
	}
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		return 0, errors.New("conversation commands require a bound context; invalid binding owner")
	}
	return pid, nil
}

var getenv = func(key string) string {
	return os.Getenv(key)
}
