package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"

	"github.com/francescoalemanno/raijin-mono/internal/core"
	"github.com/francescoalemanno/raijin-mono/internal/message"
	"github.com/francescoalemanno/raijin-mono/internal/session"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
)

// SessionAgentCall represents a request to run the agent.
type SessionAgentCall struct {
	SessionID        string
	Prompt           string
	Attachments      []message.BinaryContent
	Skills           []message.SkillContent
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

	messages message.Service
	sessions session.Service
}

// SessionAgentOptions configures a new SessionAgent.
type SessionAgentOptions struct {
	Model        libagent.RuntimeModel
	SystemPrompt string
	Tools        []libagent.Tool
	Messages     message.Service
	Sessions     session.Service
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
		sessions:       opts.Sessions,
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
// Messages are stored via the message service; session usage is tracked via session service.
func (a *SessionAgent) Run(ctx context.Context, call SessionAgentCall) error {
	if call.Prompt == "" && len(call.Attachments) == 0 && len(call.Skills) == 0 {
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
	if _, err := a.sessions.Get(ctx, call.SessionID); err != nil {
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
		history = append(history, message.ToAgentMessages(m)...)
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
		effectiveMaxOut = contextWindow / 2
		if effectiveMaxOut < 1 {
			effectiveMaxOut = 1
		}
	}

	// Prepare the user prompt message for the agent.
	prepared := message.PrepareUserRequest(message.UserRequest{
		Prompt:      call.Prompt,
		Attachments: call.Attachments,
		Skills:      call.Skills,
	})

	promptMsg := &libagent.UserMessage{
		Role:      "user",
		Content:   prepared.Prompt,
		Files:     prepared.Files,
		Timestamp: time.Now(),
	}

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
		transientIDs:        make([]string, 0, 4),
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
			for i := len(rs.transientIDs) - 1; i >= 0; i-- {
				_ = a.messages.Delete(ctx, rs.transientIDs[i])
			}
		} else if rs.currentAssistant != nil {
			if persisted, err := a.messages.Get(ctx, rs.currentAssistant.ID); err == nil {
				rs.currentAssistant = &persisted
				rs.currentAssistant.FinishThinking()
				rs.currentAssistant.AddFinish(message.FinishReasonError, promptErr.Error(), "")
				_ = a.messages.Update(ctx, *rs.currentAssistant)
			}
		}
		return promptErr
	}
	if rs.loopErr != nil {
		if errors.Is(rs.loopErr, context.Canceled) {
			for i := len(rs.transientIDs) - 1; i >= 0; i-- {
				_ = a.messages.Delete(ctx, rs.transientIDs[i])
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

	currentAssistant    *message.Message
	transientIDs        []string
	textStarted         bool
	initialUserNotified bool
	loopErr             error
}

func (rs *runState) handleEvent(ctx context.Context, event libagent.AgentEvent) error {
	switch event.Type {
	case libagent.AgentEventTypeMessageStart:
		if am, ok := event.Message.(*libagent.AssistantMessage); ok {
			_ = am
			assistantMsg, err := rs.agent.messages.Create(ctx, rs.call.SessionID, message.CreateParams{
				Role:     message.Assistant,
				Parts:    []message.ContentPart{},
				Model:    rs.model.ModelCfg.Model,
				Provider: rs.model.ModelCfg.Provider,
			})
			if err != nil {
				return err
			}
			rs.currentAssistant = &assistantMsg
			rs.transientIDs = append(rs.transientIDs, assistantMsg.ID)
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
			rs.currentAssistant.AppendReasoningContent(delta.Delta)
			return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)

		case "reasoning_end":
			rs.currentAssistant.FinishThinking()
			return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)

		case "text_delta":
			text := delta.Delta
			if !rs.textStarted {
				text = strings.TrimPrefix(text, "\n")
				rs.textStarted = true
			}
			rs.currentAssistant.AppendContent(text)
			return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)

		case "tool_input_start":
			rs.currentAssistant.AddToolCall(message.ToolCall{ID: delta.ID, Name: delta.ToolName, Finished: false})
			return rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant)

		case "tool_input_delta":
			rs.currentAssistant.AppendToolCallInput(delta.ID, delta.Delta)
		}

	case libagent.AgentEventTypeMessageEnd:
		if am, ok := event.Message.(*libagent.AssistantMessage); ok {
			if rs.currentAssistant == nil {
				return nil
			}
			// Mark tool calls as finished and update usage.
			for _, c := range am.Content {
				if tc, ok2 := c.(interface {
					GetToolCallID() string
					GetToolName() string
					GetInput() string
				}); ok2 {
					_ = tc
				}
			}
			finishReason := message.FinishReasonUnknown
			finishMessage := ""
			switch am.FinishReason {
			case "length":
				finishReason = message.FinishReasonMaxTokens
			case "stop":
				finishReason = message.FinishReasonEndTurn
			case "tool-calls":
				finishReason = message.FinishReasonToolUse
			case "error":
				finishReason = message.FinishReasonError
				if am.Error != nil {
					finishMessage = am.Error.Error()
					if rs.loopErr == nil {
						rs.loopErr = am.Error
					}
				}
			}
			rs.currentAssistant.AddFinish(finishReason, finishMessage, "")

			if err := rs.agent.messages.Update(rs.genCtx, *rs.currentAssistant); err != nil {
				return err
			}
			rs.transientIDs = rs.transientIDs[:0]
		}
		if trm, ok := event.Message.(*libagent.ToolResultMessage); ok {
			toolResult := message.FromAgentToolResult(trm)
			created, err := rs.agent.messages.Create(rs.genCtx, rs.call.SessionID, message.CreateParams{
				Role:  message.Tool,
				Parts: []message.ContentPart{toolResult},
			})
			if err != nil {
				return err
			}
			rs.transientIDs = append(rs.transientIDs, created.ID)
		}

		if um, ok := event.Message.(*libagent.UserMessage); ok {
			parts := make([]message.ContentPart, 0, 1+len(um.Files))
			if um.Content != "" {
				parts = append(parts, message.TextContent{Text: um.Content})
			}
			for _, f := range um.Files {
				parts = append(parts, message.BinaryContent{
					Path:     f.Filename,
					MIMEType: f.MediaType,
					Data:     append([]byte(nil), f.Data...),
				})
			}
			if len(parts) > 0 {
				created, err := rs.agent.messages.Create(rs.genCtx, rs.call.SessionID, message.CreateParams{
					Role:  message.User,
					Parts: parts,
				})
				if err != nil {
					return err
				}
				if !rs.initialUserNotified && rs.call.OnMessageCreated != nil {
					rs.call.OnMessageCreated(created.ID)
					rs.initialUserNotified = true
				}
			}
		}

	case libagent.AgentEventTypeAgentEnd:
		// nothing extra needed
	}
	return nil
}

func isEmptyMessage(m message.Message) bool {
	if len(m.Parts) == 0 {
		return true
	}
	if m.Role == message.Assistant && len(m.ToolCalls()) == 0 && m.Content().Text == "" && m.ReasoningContent().Thinking == "" {
		return true
	}
	return false
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
	if call.Prompt == "" && len(call.Attachments) == 0 && len(call.Skills) == 0 {
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

	prepared := message.PrepareUserRequest(message.UserRequest{
		Prompt:      call.Prompt,
		Attachments: call.Attachments,
		Skills:      call.Skills,
	})
	ag.Steer(&libagent.UserMessage{
		Role:      "user",
		Content:   prepared.Prompt,
		Files:     prepared.Files,
		Timestamp: time.Now(),
	})
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
func (a *SessionAgent) Messages() message.Service {
	return a.messages
}

// Sessions returns the session service.
func (a *SessionAgent) Sessions() session.Service {
	return a.sessions
}

// NewSessionAgentFromConfig creates a SessionAgent from a RuntimeModel, optionally reusing existing services.
func NewSessionAgentFromConfig(runtimeModel libagent.RuntimeModel, msgService message.Service, sessService session.Service) (*SessionAgent, error) {
	if runtimeModel.Model == nil {
		return nil, ErrNoModelConfigured
	}

	systemPrompt := BuildSystemPrompt()

	if msgService == nil {
		msgService = message.NewInMemoryService()
	}
	if sessService == nil {
		sessService = session.NewInMemoryService()
	}

	return NewSessionAgent(SessionAgentOptions{
		Model:        runtimeModel,
		SystemPrompt: systemPrompt,
		Messages:     msgService,
		Sessions:     sessService,
	}), nil
}

// BuildSystemPrompt constructs the system prompt with skills, tools,
// AGENTS.md content, and environment info using plain string concatenation.
func BuildSystemPrompt() string {
	sp := `<identity>
You are an expert coding agent, operating inside Raijin a coding-agent harness.
</identity>`

	// Append available skills.
	allSkills := skills.GetSkills()
	hasVisible := false
	for _, s := range allSkills {
		if s.ShouldAdvertiseToLLM() {
			hasVisible = true
			break
		}
	}
	if hasVisible {
		sp += "\n\n<skills>\n"
		sp += "- Load a skill via the \"skill\" tool when the user's request matches one listed above.\n"
		sp += "- Wording that closely matches a skill name should be treated as an implicit request to load it.\n"
		for _, s := range allSkills {
			if s.ShouldAdvertiseToLLM() {
				sp += "  <skill name=\"" + s.Name + "\">" + s.PromptDescription() + "</skill>\n"
			}
		}
		sp += "</skills>"
	}

	// Append available tools.
	allTools := tools.RegisterDefaultTools(tools.NewPathRegistry())
	if len(allTools) > 0 {
		sp += "\n\n<tools>\nThe following tools are at your disposal\n"
		for _, t := range allTools {
			info := t.Info()
			sp += "  <tool name=\"" + info.Name + "\">" + info.Description + "</tool>\n"
		}
		sp += "</tools>"
		sp += buildToolPreferencesSection(allTools)

		// Append subprocess pattern guidance only if bash tool is available and we're not already in a subprocess.
		if hasTool(allTools, "bash") && os.Getenv("RAIJIN_ENV") != "true" {
			sp += "\n\n<subprocess>\n" +
				"The $RAIJIN_BINARY environment variable is set to the path of the raijin executable. " +
				"When the user prefixes their request with 'subprocess:', they want the query executed in a sub-process.\n\n" +
				"The subprocess is STATELESS with no access to session history or files. " +
				"You MUST enhance the query to be self-contained: include minimal necessary context (file paths, code snippets), " +
				"avoid pronouns without referents, and make it explicit.\n\n" +
				"Invoke: $RAIJIN_BINARY -p \"<enhanced self-contained query>\"\n\n" +
				"Example: User asks \"subprocess: How would you optimize this function?\" → " +
				"You invoke $RAIJIN_BINARY -p \"How would you optimize the processData function in internal/processor.go lines 45-62?\"" +
				"\n</subprocess>"
		}
	}

	// Append AGENTS.md content.
	if file, ok := GetAgentsFile(); ok {
		cwd, _ := filepath.Abs(".")
		header := ""
		if !SameDir(file.Dir, cwd) {
			header = fmt.Sprintf("Note: this AGENTS.md was loaded from %q. Any relative paths in it are relative to that directory, not the current working directory.\n\n", file.Dir)
		}
		sp += "\n\n<memory>\n" + header + file.Content + "\n</memory>"
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
	sp += "\n\n<env>\nWorking directory: " + cwd +
		"\nPlatform: " + runtime.GOOS + " (" + runtime.GOARCH + ")" +
		"\nToday's date: " + time.Now().Format("2006-01-02") +
		"\nIs git repo: " + gitStatus +
		"\n</env>"

	return sp
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
	sp := "\n\n<tool-preferences>\n"
	for _, pref := range preferences {
		sp += "- " + pref + "\n"
	}
	sp += "</tool-preferences>"
	return sp
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
		return "Use the bash tool only when needed for commands that have no dedicated built-in tool equivalent."
	case "skill":
		return "Use the skill tool to load reusable workflows instead of manually reproducing those steps in bash."
	case "webfetch":
		return "Use the webfetch tool instead of curl/wget in bash for retrieving web content."
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
