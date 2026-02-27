package session

import (
	"context"
	"errors"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"

	"github.com/francescoalemanno/raijin-mono/internal/agent"
	"github.com/francescoalemanno/raijin-mono/internal/artifacts"
	"github.com/francescoalemanno/raijin-mono/internal/message"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	sessionstore "github.com/francescoalemanno/raijin-mono/internal/session"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
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
}

// New creates and initializes a chat session runtime.
func New(runtimeModel libagent.RuntimeModel) (*Session, error) {
	paths := tools.NewPathRegistry()
	s := &Session{
		paths: paths,
	}

	for _, p := range skills.GetProjectScriptsPaths() {
		s.paths.Add(p)
	}

	if store, err := persist.OpenStore(); err == nil {
		s.persistStore = store
	}

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

// ListMessages returns all stored messages for the current backend session.
func (s *Session) ListMessages(ctx context.Context) ([]message.Message, error) {
	if s.agent == nil || s.id == "" {
		return nil, nil
	}
	return s.agent.Messages().List(ctx, s.id)
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
	var sessSvc sessionstore.Service
	var msgSvc message.Service
	if s.agent != nil {
		sessSvc = s.agent.Sessions()
		msgSvc = s.agent.Messages()
	} else if s.persistStore != nil {
		sessSvc = s.persistStore.Sessions()
		msgSvc = s.persistStore.Messages()
	}

	ag, err := agent.NewSessionAgentFromConfig(runtimeModel, msgSvc, sessSvc)
	if err != nil {
		return err
	}

	s.agent = ag
	s.ensureSessionID(context.Background())
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
	for _, p := range skills.GetProjectScriptsPaths() {
		s.paths.Add(p)
	}
	s.refreshRuntime()
	return nil
}

// ForkTo creates a new durable child session pre-populated with msgs and switches to it.
func (s *Session) ForkTo(ctx context.Context, parentSessionID, forkedFromMessageID string, msgs []message.Message) error {
	if s.agent == nil {
		return nil
	}
	if s.persistStore == nil {
		return errors.New("session persistence is not available")
	}
	forked, err := s.persistStore.ForkSession(parentSessionID, forkedFromMessageID, msgs)
	if err != nil {
		return err
	}
	if err := s.agent.Sessions().SetCurrent(ctx, forked.ID); err != nil {
		return err
	}
	s.id = forked.ID
	return nil
}

// SwitchTo loads a previously persisted session and makes it current.
func (s *Session) SwitchTo(ctx context.Context, sessionID string) error {
	if s.agent == nil {
		return nil
	}
	if err := s.agent.Sessions().SetCurrent(ctx, sessionID); err != nil {
		return err
	}
	s.id = sessionID
	return nil
}

// PersistStore returns the underlying persist.Store, or nil if using in-memory only.
func (s *Session) PersistStore() *persist.Store {
	return s.persistStore
}

// HasHistory returns true when current session has at least one stored message.
func (s *Session) HasHistory(ctx context.Context) (bool, error) {
	if s.agent == nil || s.id == "" {
		return false, nil
	}
	msgs, err := s.agent.Messages().List(ctx, s.id)
	if err != nil {
		return false, err
	}
	return len(msgs) > 0, nil
}

func (s *Session) registerTools() {
	s.agentTools = tools.RegisterDefaultTools(s.paths)
	if s.agent != nil {
		s.agent.SetTools(s.agentTools)
	}
}

func (s *Session) newBackendSession(ctx context.Context) error {
	if s.agent == nil {
		return nil
	}
	if s.persistStore != nil {
		sess, err := s.persistStore.CreateEphemeral()
		if err != nil {
			return err
		}
		s.id = sess.ID
		return nil
	}
	sess, err := s.agent.Sessions().Create(ctx)
	if err != nil {
		return err
	}
	if err := s.agent.Sessions().SetCurrent(ctx, sess.ID); err != nil {
		return err
	}
	s.id = sess.ID
	return nil
}

func (s *Session) refreshRuntime() {
	if s.agent != nil {
		s.agent.SetSystemPrompt(agent.BuildSystemPrompt())
	}
	s.registerTools()
	s.SetEventCallback(s.eventCB)
}

func (s *Session) ensureSessionID(ctx context.Context) {
	if s.agent == nil || s.id != "" {
		return
	}
	if s.persistStore != nil {
		if sess, err := s.persistStore.CreateEphemeral(); err == nil {
			s.id = sess.ID
			return
		}
	}
	if sess, err := s.agent.Sessions().Create(ctx); err == nil {
		s.id = sess.ID
	}
}
