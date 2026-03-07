package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"

	"github.com/francescoalemanno/raijin-mono/internal/core"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
)

// SessionAgentCall represents a request to run the agent.
type SessionAgentCall struct {
	SessionID        string
	Prompt           string
	Attachments      []libagent.FilePart
	MaxOutputTokens  int64
	Temperature      *float64
	TopP             *float64
	TopK             *int64
	OnMessageCreated func(messageID string)
}

// SessionAgent orchestrates LLM interactions with proper message and session management.
type SessionAgent struct {
	mu            sync.RWMutex
	model         libagent.RuntimeModel
	systemPrompt  string
	agentTools    []libagent.Tool
	eventCallback func(libagent.AgentEvent)

	activeRequests map[string]context.CancelFunc
	activeAgents   map[string]*libagent.Agent

	messages libagent.MessageService
	store    *persist.Store
}

// SessionAgentOptions configures a new SessionAgent.
type SessionAgentOptions struct {
	Model        libagent.RuntimeModel
	SystemPrompt string
	Tools        []libagent.Tool
	Messages     libagent.MessageService
	Store        *persist.Store
}

// NewSessionAgent creates a new SessionAgent with services.
func NewSessionAgent(opts SessionAgentOptions) *SessionAgent {
	agentTools := make([]libagent.Tool, len(opts.Tools))
	copy(agentTools, opts.Tools)
	return &SessionAgent{
		model:          opts.Model,
		systemPrompt:   opts.SystemPrompt,
		agentTools:     agentTools,
		messages:       opts.Messages,
		store:          opts.Store,
		activeRequests: make(map[string]context.CancelFunc),
		activeAgents:   make(map[string]*libagent.Agent),
	}
}

// SetEventCallback sets the callback for agent events.
func (a *SessionAgent) SetEventCallback(cb func(libagent.AgentEvent)) {
	a.mu.Lock()
	a.eventCallback = cb
	a.mu.Unlock()
}

// EventCallback returns the current event callback.
func (a *SessionAgent) EventCallback() func(libagent.AgentEvent) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.eventCallback
}

// Run executes the agent with a user message.
// Messages are stored via the message service; session existence is verified via persist store metadata.
func (a *SessionAgent) Run(ctx context.Context, call SessionAgentCall) error {
	if call.Prompt == "" && len(call.Attachments) == 0 {
		return ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return ErrSessionMissing
	}

	a.mu.RLock()
	agentTools := make([]libagent.Tool, len(a.agentTools))
	copy(agentTools, a.agentTools)
	model := a.model
	systemPrompt := a.systemPrompt
	a.mu.RUnlock()

	if model.Model == nil {
		return errors.New("llm runtime is not configured")
	}

	// Verify session exists
	if _, err := a.store.GetSession(call.SessionID); err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Get existing messages from service
	msgs, err := a.messages.List(ctx, call.SessionID)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	// Set up cancellation
	genCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.activeRequests[call.SessionID] = cancel
	a.mu.Unlock()

	defer func() {
		cancel()
		a.mu.Lock()
		delete(a.activeRequests, call.SessionID)
		delete(a.activeAgents, call.SessionID)
		a.mu.Unlock()
	}()

	// Build history from stored messages.
	history := make([]libagent.Message, 0, len(msgs))
	for _, m := range msgs {
		if isEmptyMessage(m) {
			continue
		}
		history = append(history, libagent.CloneMessage(m))
	}

	// Resolve effective max output tokens.
	contextWindow := model.EffectiveContextWindow()
	if contextWindow == 0 {
		contextWindow = libagent.DefaultContextWindow
	}
	effectiveMaxOut := call.MaxOutputTokens
	if effectiveMaxOut <= 0 {
		effectiveMaxOut = libagent.DefaultMaxTokens
	}
	if contextWindow > 0 && effectiveMaxOut >= contextWindow {
		effectiveMaxOut = max(contextWindow/2, 1)
	}

	// Prepare the user prompt message for the agent.
	promptMsg := toRuntimeUserMessage(call.Prompt, call.Attachments)

	// Build provider options (thinking level, reasoning config, Codex instructions, etc.).
	providerOpts := model.BuildCallProviderOptions(systemPrompt)

	// Codex rejects max_output_tokens; other providers honour it.
	var maxOut int64
	if !libagent.SkipMaxOutputTokens(model.ModelCfg.Provider) {
		maxOut = effectiveMaxOut
	}

	// Build the libagent.Agent for this run.
	ag := libagent.NewAgent(libagent.AgentOptions{
		RuntimeModel:    model,
		SystemPrompt:    systemPrompt,
		Tools:           agentTools,
		Messages:        history,
		ProviderOptions: providerOpts,
		MaxOutputTokens: maxOut,
	})
	ag.SetSteeringMode(libagent.QueueModeAll)
	ag.SetFollowUpMode(libagent.QueueModeAll)
	a.mu.Lock()
	a.activeAgents[call.SessionID] = ag
	a.mu.Unlock()

	// Subscribe to events before starting.
	evCh, unsub := ag.Subscribe()
	defer unsub()

	// Track per-run state.
	rs := &runState{
		agent:               a,
		call:                call,
		model:               model,
		genCtx:              genCtx,
		initialUserNotified: false,
	}

	// Run the agent prompt in a goroutine; we handle events synchronously.
	// libagent closes evCh (via stopAllSubscribers) when the run completes,
	// so the range loop exits naturally without any special-casing.
	promptErrCh := make(chan error, 1)
	go func() {
		promptErrCh <- ag.Prompt(genCtx, promptMsg.Content, promptMsg.Files...)
	}()

	for event := range evCh {
		if err := rs.handleEvent(ctx, event); err != nil {
			// Fatal persistence error: cancel the run and drain evCh so the
			// subscriber goroutine and libagent can unwind cleanly.
			cancel()
			for range evCh {
			}
			<-promptErrCh
			return err
		}
		if cb := a.EventCallback(); cb != nil {
			cb(event)
		}
	}

	// evCh closed: libagent finished. Collect Prompt()'s return value.
	promptErr := <-promptErrCh

	if promptErr != nil {
		if errors.Is(promptErr, context.Canceled) {
			return promptErr
		}
		if rs.currentAssistant != nil {
			rs.currentAssistant.Completed = true
			rs.currentAssistant.CompleteReason = "error"
			rs.currentAssistant.CompleteMessage = promptErr.Error()
			if err := rs.persistAssistant(ctx); err != nil {
				return err
			}
		}
		return promptErr
	}
	if rs.loopErr != nil {
		if errors.Is(rs.loopErr, context.Canceled) {
			return rs.loopErr
		}
		if rs.currentAssistant != nil {
			rs.currentAssistant.Completed = true
			rs.currentAssistant.CompleteReason = "error"
			rs.currentAssistant.CompleteMessage = rs.loopErr.Error()
			if err := rs.persistAssistant(ctx); err != nil {
				return err
			}
		}
		return rs.loopErr
	}

	return nil
}

// runState holds mutable per-run state updated as agent events arrive.
type runState struct {
	agent  *SessionAgent
	call   SessionAgentCall
	model  libagent.RuntimeModel
	genCtx context.Context

	currentAssistant    *libagent.AssistantMessage
	textStarted         bool
	initialUserNotified bool
	loopErr             error
}

func (rs *runState) handleEvent(ctx context.Context, event libagent.AgentEvent) error {
	switch event.Type {
	case libagent.AgentEventTypeMessageStart:
		if _, ok := event.Message.(*libagent.AssistantMessage); ok {
			rs.currentAssistant = libagent.NewAssistantMessage("", "", nil, time.Now())
			rs.currentAssistant.Meta = libagent.MessageMeta{
				SessionID: rs.call.SessionID,
				Model:     rs.model.ModelCfg.Model,
				Provider:  rs.model.ModelCfg.Provider,
			}
			rs.textStarted = false
		}

	case libagent.AgentEventTypeMessageUpdate:
		if rs.currentAssistant == nil {
			return nil
		}
		delta := event.Delta
		if delta == nil {
			return nil
		}
		switch delta.Type {
		case "reasoning_delta":
			rs.currentAssistant.Reasoning += delta.Delta
		case "text_delta":
			text := delta.Delta
			if !rs.textStarted {
				text = strings.TrimPrefix(text, "\n")
				rs.textStarted = true
			}
			rs.currentAssistant.Text += text
		}

	case libagent.AgentEventTypeMessageEnd:
		if am, ok := event.Message.(*libagent.AssistantMessage); ok {
			if rs.currentAssistant == nil {
				return nil
			}
			if len(am.Content) > 0 {
				rs.currentAssistant.Content = append(rs.currentAssistant.Content[:0], am.Content...)
			}
			rs.currentAssistant.Completed = true
			rs.currentAssistant.CompleteReason = string(am.FinishReason)
			if am.Error != nil {
				rs.currentAssistant.CompleteMessage = am.Error.Error()
				if rs.loopErr == nil {
					rs.loopErr = am.Error
				}
			}
			if err := rs.persistAssistant(rs.genCtx); err != nil {
				return err
			}
		}
		if trm, ok := event.Message.(*libagent.ToolResultMessage); ok {
			sessionID := rs.call.SessionID
			if strings.TrimSpace(trm.Meta.SessionID) != "" {
				sessionID = trm.Meta.SessionID
			}
			stored := libagent.CloneMessage(trm)
			meta := libagent.MessageMetaOf(stored)
			if meta.SessionID == "" {
				meta.SessionID = sessionID
			}
			libagent.SetMessageMeta(stored, meta)
			if _, err := rs.agent.messages.Create(rs.genCtx, sessionID, stored); err != nil {
				return err
			}
		}

		if um, ok := event.Message.(*libagent.UserMessage); ok {
			if strings.TrimSpace(um.Content) != "" || len(um.Files) > 0 {
				sessionID := rs.call.SessionID
				if strings.TrimSpace(um.Meta.SessionID) != "" {
					sessionID = um.Meta.SessionID
				}
				stored := libagent.CloneMessage(um)
				meta := libagent.MessageMetaOf(stored)
				if meta.SessionID == "" {
					meta.SessionID = sessionID
				}
				libagent.SetMessageMeta(stored, meta)
				created, err := rs.agent.messages.Create(rs.genCtx, sessionID, stored)
				if err != nil {
					return err
				}
				if !rs.initialUserNotified && rs.call.OnMessageCreated != nil {
					rs.call.OnMessageCreated(libagent.MessageID(created))
					rs.initialUserNotified = true
				}
			}
		}

	case libagent.AgentEventTypeAgentEnd:
		// nothing extra needed
	}
	return nil
}

func isEmptyMessage(m libagent.Message) bool {
	switch msg := m.(type) {
	case *libagent.UserMessage:
		return strings.TrimSpace(msg.Content) == "" && len(msg.Files) == 0
	case *libagent.AssistantMessage:
		return !msg.Completed || (strings.TrimSpace(libagent.AssistantText(msg)) == "" && strings.TrimSpace(libagent.AssistantReasoning(msg)) == "" && len(libagent.AssistantToolCalls(msg)) == 0)
	case *libagent.ToolResultMessage:
		return strings.TrimSpace(msg.ToolCallID) == "" || strings.TrimSpace(msg.ToolName) == ""
	default:
		return true
	}
}

func toRuntimeUserMessage(text string, attachments []libagent.FilePart) *libagent.UserMessage {
	return &libagent.UserMessage{
		Role:      "user",
		Content:   libagent.PromptWithUserAttachments(strings.TrimSpace(text), attachments),
		Files:     libagent.NonTextFiles(attachments),
		Timestamp: time.Now(),
	}
}

func (rs *runState) persistAssistant(ctx context.Context) error {
	if rs.currentAssistant == nil {
		return nil
	}
	toStore := libagent.CloneMessage(rs.currentAssistant)
	if isEmptyMessage(toStore) {
		rs.currentAssistant = nil
		return nil
	}
	if _, err := rs.agent.messages.Create(ctx, rs.call.SessionID, toStore); err != nil {
		return err
	}
	rs.currentAssistant = nil
	return nil
}

// Cancel cancels a running session.
func (a *SessionAgent) Cancel(sessionID string) {
	a.mu.RLock()
	cancel := a.activeRequests[sessionID]
	a.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// Steer queues a user message into the active run for this session.
func (a *SessionAgent) Steer(_ context.Context, call SessionAgentCall) error {
	if call.Prompt == "" && len(call.Attachments) == 0 {
		return ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return ErrSessionMissing
	}

	a.mu.RLock()
	ag := a.activeAgents[call.SessionID]
	a.mu.RUnlock()
	if ag == nil {
		return errors.New("session is not running")
	}

	ag.Steer(toRuntimeUserMessage(call.Prompt, call.Attachments))
	return nil
}

// CancelAll cancels all running sessions.
func (a *SessionAgent) CancelAll() {
	a.mu.RLock()
	ids := make([]string, 0, len(a.activeRequests))
	for id := range a.activeRequests {
		ids = append(ids, id)
	}
	a.mu.RUnlock()
	for _, id := range ids {
		a.Cancel(id)
	}
}

// IsBusy returns true if any session is running.
func (a *SessionAgent) IsBusy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.activeRequests) > 0
}

// IsSessionBusy returns true if the specified session is running.
func (a *SessionAgent) IsSessionBusy(sessionID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, busy := a.activeRequests[sessionID]
	return busy
}

// Model returns the current model.
func (a *SessionAgent) Model() libagent.RuntimeModel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.model
}

// SetModel updates the model.
func (a *SessionAgent) SetModel(m libagent.RuntimeModel) {
	a.mu.Lock()
	a.model = m
	a.mu.Unlock()
}

// SetSystemPrompt updates the system prompt.
func (a *SessionAgent) SetSystemPrompt(prompt string) {
	a.mu.Lock()
	a.systemPrompt = prompt
	a.mu.Unlock()
}

// Tools returns a copy of the current agent tools.
func (a *SessionAgent) Tools() []libagent.Tool {
	a.mu.RLock()
	out := make([]libagent.Tool, len(a.agentTools))
	copy(out, a.agentTools)
	a.mu.RUnlock()
	return out
}

// SetTools updates the agent tools.
func (a *SessionAgent) SetTools(t []libagent.Tool) {
	cp := make([]libagent.Tool, len(t))
	copy(cp, t)
	a.mu.Lock()
	a.agentTools = cp
	a.mu.Unlock()
}

// Messages returns the message service.
func (a *SessionAgent) Messages() libagent.MessageService {
	return a.messages
}

// NewSessionAgentFromConfig creates a SessionAgent from a RuntimeModel.
func NewSessionAgentFromConfig(runtimeModel libagent.RuntimeModel, msgService libagent.MessageService, store *persist.Store) (*SessionAgent, error) {
	if runtimeModel.Model == nil {
		return nil, ErrNoModelConfigured
	}

	systemPrompt := BuildSystemPrompt()

	if msgService == nil {
		return nil, ErrMessageServiceMissing
	}
	if store == nil {
		return nil, ErrSessionStoreMissing
	}

	return NewSessionAgent(SessionAgentOptions{
		Model:        runtimeModel,
		SystemPrompt: systemPrompt,
		Messages:     msgService,
		Store:        store,
	}), nil
}

// BuildSystemPrompt constructs the system prompt with skills, tools,
// AGENTS.md content, and environment info using plain string concatenation.
func BuildSystemPrompt() string {
	var sp strings.Builder
	sp.WriteString(`<identity>
You are an expert coding agent, operating inside Raijin a coding-agent harness.
</identity>`)

	// Append available skills.
	allSkills := skills.GetSkills()
	if len(allSkills) > 0 {
		sp.WriteString("\n\n<skills>\n")
		sp.WriteString("- Load a skill via the \"read\" tool when the user's request matches one listed above.\n")
		sp.WriteString("- The user is requesting skill loading when either $skillname syntax is used or there is wording that closely matches a skill name or purpose.\n")
		for _, s := range allSkills {
			sp.WriteString("  <skill name=\"" + s.Name + "\" path=\"" + s.FilePath + "\">" + s.PromptDescription() + "</skill>\n")
		}
		sp.WriteString("</skills>")
	}

	// Append available tools.
	allTools := tools.RegisterDefaultTools(tools.NewPathRegistry())
	if len(allTools) > 0 {
		sp.WriteString("\n\n<tools>\nThe following tools are at your disposal\n")
		for _, t := range allTools {
			sp.WriteString(renderToolForSystemPrompt(t.Info()))
		}
		sp.WriteString("</tools>")
		sp.WriteString(buildToolPreferencesSection(allTools))

		// Append subprocess pattern guidance only if bash tool is available and we're not already in a subprocess.
		if hasTool(allTools, "bash") && os.Getenv("RAIJIN_ENV") != "true" {
			sp.WriteString(`
<subprocess>
The $RAIJIN_BINARY environment variable is set to the path of the raijin executable. 
When the user prefixes their request with 'subprocess:', they want the query executed in a sub-process.
The subprocess is STATELESS with no access to session history or files. 
You MUST enhance the query to be self-contained: include minimal necessary context (file paths, code snippets), 
avoid pronouns without referents, and make it explicit.
Invoke: $RAIJIN_BINARY -p "<enhanced self-contained query>"
Example: User asks "subprocess: How would you optimize this function?" → 
You invoke $RAIJIN_BINARY -p "How would you optimize the processData function in internal/processor.go lines 45-62?"
</subprocess>
`)
		}
	}

	// Append AGENTS.md content.
	if file, ok := GetAgentsFile(); ok {
		cwd, _ := filepath.Abs(".")
		header := ""
		if !SameDir(file.Dir, cwd) {
			header = fmt.Sprintf("Note: this AGENTS.md was loaded from %q. Any relative paths in it are relative to that directory, not the current working directory.\n\n", file.Dir)
		}
		sp.WriteString("\n\n<memory>\n" + header + file.Content + "\n</memory>")
	}

	// Append environment section.
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "unknown"
	}
	gitStatus := "no"
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
		gitStatus = "yes"
	}
	sp.WriteString("\n\n<env>\nWorking directory: " + cwd +
		"\nPlatform: " + runtime.GOOS + " (" + runtime.GOARCH + ")" +
		"\nToday's date: " + time.Now().Format("2006-01-02") +
		"\nIs git repo: " + gitStatus +
		"\n</env>")

	return sp.String()
}

func renderToolForSystemPrompt(info libagent.ToolInfo) string {
	line := "  <tool name=\"" + info.Name + "\">" + info.Description
	if params := renderToolParametersForSystemPrompt(info.Parameters, info.Required); params != "" {
		line += "\n" + params + "\n"
	}
	line += "</tool>\n"
	return line
}

func renderToolParametersForSystemPrompt(parameters map[string]any, required []string) string {
	if len(parameters) == 0 {
		return ""
	}

	requiredSet := make(map[string]struct{}, len(required))
	for _, name := range required {
		requiredSet[name] = struct{}{}
	}

	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names)+1)
	lines = append(lines, "    Parameters:")
	for _, name := range names {
		desc := ""
		typeName := "any"

		if schema, ok := parameters[name].(map[string]any); ok {
			if d, ok := schema["description"].(string); ok && strings.TrimSpace(d) != "" {
				desc = d
			}
			if t, ok := schema["type"].(string); ok && strings.TrimSpace(t) != "" {
				typeName = t
			}
		}
		if desc == "" {
			desc = "(no description)"
		}

		req := "optional"
		if _, ok := requiredSet[name]; ok {
			req = "required"
		}
		lines = append(lines, "    - `"+name+"` ("+typeName+", "+req+"): "+desc)
	}
	return strings.Join(lines, "\n")
}

func buildToolPreferencesSection(allTools []libagent.Tool) string {
	if len(allTools) == 0 {
		return ""
	}
	preferences := make([]string, 0, len(allTools))
	for _, t := range allTools {
		name := core.Normalize(t.Info().Name)
		if name == "" {
			continue
		}
		preferences = append(preferences, toolPreferenceFor(name))
	}
	if len(preferences) == 0 {
		return ""
	}
	var sp strings.Builder
	sp.WriteString("\n\n<tool-preferences>\n")
	for _, pref := range preferences {
		sp.WriteString("- " + pref + "\n")
	}
	sp.WriteString("</tool-preferences>")
	return sp.String()
}

func toolPreferenceFor(name string) string {
	switch name {
	case "read":
		return "Use the read tool instead of shelling out with cat/sed/head/tail/ls for inspecting files and directories."
	case "glob":
		return "Use the glob tool instead of find/ls pipelines when locating files by pattern."
	case "grep":
		return "Use the grep tool instead of running grep/ripgrep in bash for content search."
	case "edit":
		return "Use the edit tool instead of perl/sed/awk/python one-liners for surgical in-place edits."
	case "write":
		return "Use the write tool instead of shell redirection (>, >>, cat <<EOF) when creating or overwriting files."
	case "bash":
		return "Use the bash tool only when needed for commands that have no dedicated built-in tool equivalent. Never respond to the user via cat or similar shell tools — respond directly."
	default:
		return "Use the " + name + " tool instead of using bash or shell scripts as equivalents for that task."
	}
}

// hasTool checks if a tool with the given normalized name exists in the list.
func hasTool(tools []libagent.Tool, name string) bool {
	for _, t := range tools {
		if core.Normalize(t.Info().Name) == name {
			return true
		}
	}
	return false
}
