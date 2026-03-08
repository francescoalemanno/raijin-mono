package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/terminal"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	"github.com/francescoalemanno/raijin-mono/internal/agent"
	chatsession "github.com/francescoalemanno/raijin-mono/internal/chat/session"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/persist"
	"github.com/francescoalemanno/raijin-mono/internal/prompts"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
)

type chatState int

const (
	stateIdle chatState = iota
	stateRunning
)

var thinkingLevelCycleOrder = []libagent.ThinkingLevel{
	libagent.ThinkingLevelLow,
	libagent.ThinkingLevelMedium,
	libagent.ThinkingLevelHigh,
	libagent.ThinkingLevelMax,
}

// historyEntry tracks a component in the history container.
type historyEntry struct {
	component tui.Component
}

type ChatApp struct {
	ui           *tui.TUI
	session      *chatsession.Session
	runtimeModel libagent.RuntimeModel
	modelCfg     libagent.ModelConfig
	store        *modelconfig.ModelStore
	workingDir   string

	// Components
	logo            *components.Text
	header          *infoBar
	history         *tui.Container
	statusContainer *tui.Container
	statusText      *components.Text
	statusLoader    *components.Loader
	editorContainer *tui.Container
	editor          *components.Editor
	footer          *components.Text

	// State (managed on UI goroutine via Dispatch)
	state              chatState
	compacting         bool
	currentThinking    string
	thinkingComponent  *ThinkingComponent
	currentReply       string
	replyComponent     *MessageComponent
	items              []historyEntry
	pendingTools       map[string]*ToolExecutionComponent // tool call ID -> component
	blocksExpanded     bool
	totalTokens        int64
	contextWindow      int64
	thinkingLevelDirty bool
	suppressTextEvents bool
	activeModalDone    func()
	activeModalID      uint64
	nextModalID        uint64

	// Shutdown
	done chan struct{}
}

func RunChat(runtimeModel libagent.RuntimeModel, modelCfg libagent.ModelConfig) error {
	return RunChatWithPrompt(runtimeModel, modelCfg, "")
}

func RunChatWithPrompt(runtimeModel libagent.RuntimeModel, modelCfg libagent.ModelConfig, initialPrompt string) error {
	sess, err := chatsession.New(runtimeModel)
	if err != nil && sess == nil {
		return err
	}
	store, _ := modelconfig.LoadModelStore()

	term := terminal.NewProcessTerminal()
	app := newChatApp(term, sess, runtimeModel, modelCfg, store)

	app.session.SetEventCallback(func(event libagent.AgentEvent) {
		app.handleEvent(event)
	})

	app.ui.Start()
	if initialPrompt = strings.TrimSpace(initialPrompt); initialPrompt != "" {
		app.submitInitialPrompt(initialPrompt)
	}
	<-app.done
	app.ui.Stop()
	term.DrainInput(500, 50)
	return nil
}

func (app *ChatApp) submitInitialPrompt(prompt string) {
	go app.handleSubmit(prompt)
}

func newChatApp(term terminal.Terminal, sess *chatsession.Session, runtimeModel libagent.RuntimeModel, modelCfg libagent.ModelConfig, store *modelconfig.ModelStore) *ChatApp {
	app := &ChatApp{
		session:      sess,
		runtimeModel: runtimeModel,
		modelCfg:     modelCfg,
		store:        store,
		pendingTools: make(map[string]*ToolExecutionComponent),
		done:         make(chan struct{}),
	}
	app.workingDir = renderWorkingDir()

	app.ui = tui.NewTUI(term)

	// Logo at top
	app.logo = components.NewText("", 0, 0, nil)
	app.logo.SetFgColorFn(theme.Default.Foreground.Ansi24)
	app.ui.AddChild(app.logo)

	// History container — messages flow here, natural scrollback
	app.history = &tui.Container{}
	app.ui.AddChild(app.history)

	editorTheme := components.EditorTheme{
		BorderColor:    theme.Default.Accent.Ansi24,
		Foreground:     theme.Default.Foreground.Ansi24,
		ShellLineColor: theme.Default.AccentAlt.Ansi24,
		SelectList: components.SelectListTheme{
			Prefix:         theme.Default.Foreground.Ansi24,
			SelectedPrefix: theme.Default.Accent.Ansi24,
			SelectedText:   theme.Default.Accent.Ansi24,
			Description:    theme.Default.Muted.Ansi24,
			ScrollInfo:     theme.Default.Muted.Ansi24,
			NoMatch:        theme.Default.Muted.Ansi24,
		},
	}
	app.editor = components.NewEditor(app.ui, editorTheme, components.EditorOptions{
		PaddingX:               1,
		AutocompleteMaxVisible: 8,
	})
	app.editor.SetOnSubmit(func(text string) {
		app.editor.AddToHistory(text)
		app.editor.SetText("")
		go app.handleSubmit(text)
	})
	app.setupAutocompleteProvider()

	// Status container — swaps between text and loader
	app.statusContainer = &tui.Container{}
	app.statusText = components.NewText("", 1, 0, nil)
	app.statusText.SetFgColorFn(theme.Default.Foreground.Ansi24)
	app.statusContainer.AddChild(app.statusText)
	app.ui.AddChild(app.statusContainer)
	app.editorContainer = &tui.Container{}
	app.editorContainer.AddChild(app.editor)
	app.ui.AddChild(app.editorContainer)
	app.ui.SetFocus(app.editor)

	// Info bar (below editor)
	app.header = newInfoBar()
	app.ui.AddChild(app.header)

	// Footer
	app.footer = components.NewText("", 1, 0, nil)
	app.footer.SetFgColorFn(theme.Default.Foreground.Ansi24)
	app.ui.AddChild(app.footer)

	// Global key listener — intercepts before editor sees input
	app.ui.AddInputListener(app.globalKeyListener)

	// Set initial content
	app.refreshHeader()
	app.refreshStatus()
	app.refreshFooter()

	// Always start with a fresh welcome screen; prior sessions are accessible
	// via /sessions.
	app.ShowWelcomeMessage()

	return app
}

// globalKeyListener intercepts global shortcuts before the editor handles input.
func (app *ChatApp) globalKeyListener(data string) *tui.InputListenerResult {
	if keys.IsKeyRelease(data) {
		return nil
	}
	keyID := keys.ParseKey(data)

	switch keyID {
	case "ctrl+d":
		if app.compacting {
			return &tui.InputListenerResult{Consume: true}
		}
		app.quit()
		return &tui.InputListenerResult{Consume: true}
	case "ctrl+t", "shift+tab":
		app.cycleThinkingLevel()
		return &tui.InputListenerResult{Consume: true}
	case "ctrl+c", "escape":
		if app.activeModalDone != nil {
			app.activeModalDone()
			return &tui.InputListenerResult{Consume: true}
		}
		// Let the editor handle it first when autocomplete is open
		if app.editor.IsShowingAutocomplete() {
			return nil
		}
		if app.compacting {
			return &tui.InputListenerResult{Consume: true}
		}
		if app.state == stateRunning {
			app.interruptRun()
			return &tui.InputListenerResult{Consume: true}
		}
		return nil
	case "ctrl+p":
		app.showModelSelector()
		return &tui.InputListenerResult{Consume: true}
	case "ctrl+o":
		app.toggleBlocksExpanded()
		return &tui.InputListenerResult{Consume: true}
	}

	return nil
}

func (app *ChatApp) quit() {
	select {
	case <-app.done:
	default:
		close(app.done)
	}
}

func (app *ChatApp) refreshHeader() {
	// Logo
	app.logo.SetText(theme.Default.RenderGradient("//////// RAIJIN ////////"))

	// Compute estimated tokens from message content
	estimatedTokens := int64(2400)
	if app.session != nil {
		if msgs, err := app.session.ListMessages(context.Background()); err == nil {
			for _, msg := range msgs {
				estimatedTokens += estimateMessageTokens(msg)
			}
		}
	}

	contextWindow := app.contextWindow
	if contextWindow == 0 {
		contextWindow = app.runtimeModel.EffectiveContextWindow()
	}

	// Build parts in priority order: warning, model, ctx%, cwd
	parts := make([]string, 0, 4)

	if contextWindow > 0 {
		tokens := max(float64(estimatedTokens), float64(app.totalTokens))
		pct := float64(tokens) / float64(contextWindow) * 100
		ctxPart := theme.Default.Muted.Ansi24(fmt.Sprintf("%.1f%%/%s", pct, formatTokenCount(contextWindow)))

		if pct >= 80 {
			parts = append(parts, theme.Default.Danger.AnsiBold("LLM about to fail for context exhaustion, run /compact"))
		} else if pct >= 45 {
			parts = append(parts, theme.Default.Accent.AnsiBold("LLM performance reduced, run /compact"))
		}

		if app.modelCfg.Provider != "" {
			thinking := string(libagent.NormalizeThinkingLevel(app.modelCfg.ThinkingLevel))
			parts = append(parts, theme.Default.Muted.Ansi24(fmt.Sprintf("(%s) %s • %s", app.modelCfg.Provider, app.modelCfg.Model, thinking)))
		}

		parts = append(parts, ctxPart)
	} else if app.modelCfg.Provider != "" {
		thinking := string(libagent.NormalizeThinkingLevel(app.modelCfg.ThinkingLevel))
		parts = append(parts, theme.Default.Muted.Ansi24(fmt.Sprintf("(%s) %s • %s", app.modelCfg.Provider, app.modelCfg.Model, thinking)))
	}

	if app.workingDir != "" {
		parts = append(parts, app.workingDir)
	}

	app.header.SetParts(parts)
}

func formatTokenCount(tokens int64) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%dk", tokens/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

func renderWorkingDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err == nil && (cwd == home || strings.HasPrefix(cwd, home+string(os.PathSeparator))) {
		cwd = "~" + cwd[len(home):]
	}
	return theme.Default.Foreground.Ansi24(cwd)
}

func (app *ChatApp) refreshStatus() {
	switch app.state {
	case stateRunning:
		app.showStatusLoader("Working")
	default:
		app.stopLoader()
	}
}

func (app *ChatApp) showStatusLoader(message string) {
	if app.statusLoader != nil {
		app.statusLoader.SetMessage(message)
		return
	}
	app.statusContainer.Clear()
	loader := components.NewLoader(app.ui, theme.Default.Accent.Ansi24, theme.Default.AccentAlt.Ansi24, message)
	app.statusLoader = loader
	app.statusContainer.AddChild(loader)
	go loader.Loop()
}

func (app *ChatApp) stopLoader() {
	if app.statusLoader != nil {
		app.statusLoader.Stop()
		app.statusLoader = nil
		app.statusContainer.Clear()
	}
}

func (app *ChatApp) refreshFooter() {
	shortcuts := theme.Default.AccentAlt.AnsiBold("esc") + " " + theme.Default.Muted.Ansi24("cancel") +
		"  " + theme.Default.AccentAlt.AnsiBold("ctrl+t") + " " + theme.Default.Muted.Ansi24("thinking") +
		"  " + theme.Default.AccentAlt.AnsiBold("ctrl+o") + " " + theme.Default.Muted.Ansi24("expand") +
		"  " + theme.Default.AccentAlt.AnsiBold("ctrl+p") + " " + theme.Default.Muted.Ansi24("models") +
		"  " + theme.Default.AccentAlt.AnsiBold("/") + " " + theme.Default.Muted.Ansi24("commands") +
		"  " + theme.Default.AccentAlt.AnsiBold("ctrl+d") + " " + theme.Default.Muted.Ansi24("quit")
	app.footer.SetText(shortcuts)
}

func (app *ChatApp) cycleThinkingLevel() {
	if app.modelCfg.Provider == "" {
		return
	}

	nextLevel := nextThinkingLevel(app.modelCfg.ThinkingLevel)
	app.modelCfg.ThinkingLevel = nextLevel

	if app.store != nil {
		if defaultName := app.store.DefaultName(); defaultName != "" {
			if modelCfg, found := app.store.Get(defaultName); found {
				modelCfg.ThinkingLevel = nextLevel
				if err := app.store.Add(modelCfg); err != nil {
					app.appendMessage("failed to persist thinking level: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
					return
				}
			}
		}
	}

	if app.state == stateRunning {
		app.thinkingLevelDirty = true
	} else if app.session != nil {
		newModel, err := rebuildRuntimeModel(app.modelCfg)
		if err != nil {
			app.appendMessage("failed to apply thinking level: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			return
		}
		app.runtimeModel = newModel
		if err := app.session.Reconfigure(app.runtimeModel); err != nil {
			app.appendMessage("failed to apply thinking level: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			return
		}
	}

	app.refreshHeader()
}

func nextThinkingLevel(current libagent.ThinkingLevel) libagent.ThinkingLevel {
	normalized := libagent.NormalizeThinkingLevel(current)
	for i, level := range thinkingLevelCycleOrder {
		if level == normalized {
			return thinkingLevelCycleOrder[(i+1)%len(thinkingLevelCycleOrder)]
		}
	}
	return thinkingLevelCycleOrder[0]
}

func (app *ChatApp) resetConversationView(showWelcome bool) {
	app.history.Clear()
	app.items = nil
	app.pendingTools = make(map[string]*ToolExecutionComponent)
	app.currentReply = ""
	app.replyComponent = nil
	app.currentThinking = ""
	app.thinkingComponent = nil
	app.activeModalDone = nil
	app.activeModalID = 0
	app.totalTokens = 0
	app.contextWindow = 0
	app.state = stateIdle
	if showWelcome {
		welcome := app.newWelcomeComponent()
		app.history.AddChild(welcome)
		app.items = append(app.items, historyEntry{component: welcome})
	}
}

func (app *ChatApp) newWelcomeComponent() *WelcomeComponent {
	var toolNames []string
	for _, plugin := range tools.LoadPluginInfos() {
		toolNames = append(toolNames, plugin.Name)
	}

	var skillNames []string
	for _, skill := range skills.GetExternalSkills() {
		skillNames = append(skillNames, skill.Name)
	}

	return NewWelcomeComponent(skillNames, toolNames)
}

func (app *ChatApp) restoreHistoryFromSession(ctx context.Context) bool {
	if app.session == nil {
		return false
	}
	msgs, err := app.session.ListMessages(ctx)
	if err != nil || len(msgs) == 0 {
		return false
	}

	for _, msg := range msgs {
		app.appendStoredMessage(msg)
	}
	app.finalizeReplayedToolStates()
	return len(app.items) > 0
}

func (app *ChatApp) finalizeReplayedToolStates() {
	for id, comp := range app.pendingTools {
		comp.MarkCancelled()
		delete(app.pendingTools, id)
	}
}

func (app *ChatApp) appendStoredMessage(msg libagent.Message) {
	switch m := msg.(type) {
	case *libagent.UserMessage:
		text := strings.TrimSpace(m.Content)
		if text != "" {
			app.appendSpacer()
			app.appendMessage(text, theme.BorderThin, theme.Default.Accent.Ansi24, theme.Default.Foreground.Ansi24, false)
		}

	case *libagent.AssistantMessage:
		reasoning := strings.TrimSpace(libagent.AssistantReasoning(m))
		if reasoning != "" {
			app.appendSpacer()
			comp := NewThinking(app.ui)
			comp.SetExpanded(app.blocksExpanded)
			comp.SetText(reasoning)
			comp.Finish()
			app.history.AddChild(comp)
			app.items = append(app.items, historyEntry{component: comp})
		}

		for _, tc := range libagent.AssistantToolCalls(m) {
			var args json.RawMessage
			if tc.Input != "" {
				args = json.RawMessage(tc.Input)
			}
			app.appendToolExecution(tc.ID, tc.Name, args)
		}
		text := strings.TrimSpace(libagent.AssistantText(m))
		if text != "" {
			app.appendSpacer()
			app.appendMessage(text, theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, true)
		}
	case *libagent.ToolResultMessage:
		result := m
		data := libagent.EncodeDataString(result.Data)
		if comp, ok := app.pendingTools[result.ToolCallID]; ok {
			comp.UpdateResultWithMedia(result.Content, result.IsError, result.MIMEType, data)
			delete(app.pendingTools, result.ToolCallID)
		} else {
			comp := app.appendToolExecution(result.ToolCallID, result.ToolName, nil)
			comp.UpdateResultWithMedia(result.Content, result.IsError, result.MIMEType, data)
			delete(app.pendingTools, result.ToolCallID)
		}
	}
}

// ---------------------------------------------------------------------------
// History management
// ---------------------------------------------------------------------------

// appendMessage appends a text message to the conversation history.
func (app *ChatApp) appendMessage(content string, borderChar string, borderColor, bodyColor func(string) string, markdown bool) *MessageComponent {
	comp := NewMessage(content, borderChar, borderColor, bodyColor, markdown)
	app.history.AddChild(comp)
	app.items = append(app.items, historyEntry{component: comp})
	return comp
}

// appendToolExecution appends a tool-execution component to the conversation history.
func (app *ChatApp) appendToolExecution(toolID, toolName string, args json.RawMessage) *ToolExecutionComponent {
	var tool libagent.Tool
	if app.session != nil {
		tool = tools.FindTool(app.session.Tools(), toolName)
		if tool == nil && app.session.Agent() != nil {
			tool = tools.FindTool(app.session.Agent().Tools(), toolName)
		}
	}
	app.history.AddChild(components.NewSpacer(1))
	comp := NewToolExecution(toolName, args, tool, app.ui)
	comp.SetExpanded(app.blocksExpanded)
	app.history.AddChild(comp)
	app.items = append(app.items, historyEntry{component: comp})
	if toolID != "" {
		app.pendingTools[toolID] = comp
	}
	return comp
}

// appendSpacer appends a blank spacer row to the conversation history.
func (app *ChatApp) appendSpacer() {
	sp := components.NewSpacer(1)
	app.history.AddChild(sp)
	app.items = append(app.items, historyEntry{component: sp})
}

func (app *ChatApp) flushThinking() {
	if app.thinkingComponent == nil {
		return
	}
	app.thinkingComponent.SetText(app.currentThinking)
	app.thinkingComponent.Finish()
	app.currentThinking = ""
	app.thinkingComponent = nil
}

func (app *ChatApp) flushReply() {
	app.flushThinking()
	if app.currentReply == "" {
		return
	}
	if app.replyComponent != nil {
		app.replyComponent.SetContent(app.currentReply)
	} else {
		app.appendSpacer()
		app.appendMessage(app.currentReply, theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, true)
	}
	app.currentReply = ""
	app.replyComponent = nil
}

// ---------------------------------------------------------------------------
// Agent event handling (called from agent goroutine)
// ---------------------------------------------------------------------------

func (app *ChatApp) handleEvent(event libagent.AgentEvent) {
	app.ui.DispatchOrdered(func() {
		if app.suppressTextEvents {
			if event.Type == libagent.AgentEventTypeMessageUpdate && event.Delta != nil {
				switch event.Delta.Type {
				case "text_delta", "reasoning_delta":
					return
				}
			}
		}
		switch event.Type {
		case libagent.AgentEventTypeAgentStart:
			app.onStreaming()
		case libagent.AgentEventTypeMessageUpdate:
			app.onMessageUpdate(event)
		case libagent.AgentEventTypeToolExecutionStart:
			app.onToolExecutionStart(event)
		case libagent.AgentEventTypeMessageEnd:
			app.onMessageEnd(event)
		}
		app.refreshStatus()
		app.refreshHeader()
	})
}

func (app *ChatApp) onMessageUpdate(event libagent.AgentEvent) {
	delta := event.Delta
	if delta == nil {
		return
	}
	switch delta.Type {
	case "text_delta":
		app.onTextDelta(delta.Delta)
	case "reasoning_delta":
		app.onThinkingDelta(delta.Delta)
	case "tool_input_start":
		app.onToolCall(delta.ID, delta.ToolName, "")
	case "tool_input_delta":
		app.onToolInputDelta(delta.ID, delta.Delta)
	}
}

func (app *ChatApp) onMessageEnd(event libagent.AgentEvent) {
	switch m := event.Message.(type) {
	case *libagent.ToolResultMessage:
		mediaData := ""
		if len(m.Data) > 0 {
			mediaData = base64.StdEncoding.EncodeToString(m.Data)
		}
		app.onToolResult(m.ToolCallID, m.Content, m.IsError, m.MIMEType, mediaData)
	case *libagent.AssistantMessage:
		app.totalTokens = m.Usage.InputTokens + m.Usage.CacheReadTokens + m.Usage.OutputTokens
		if cw := app.runtimeModel.EffectiveContextWindow(); cw > 0 {
			app.contextWindow = cw
		}
	}
}

func (app *ChatApp) onToolExecutionStart(event libagent.AgentEvent) {
	app.onToolCall(event.ToolCallID, event.ToolName, event.ToolArgs)
}

// onTextDelta handles an incremental text chunk from the assistant.
func (app *ChatApp) onTextDelta(text string) {
	app.flushThinking()
	app.currentReply += text
	if app.replyComponent == nil {
		app.appendSpacer()
		app.replyComponent = app.appendMessage("", theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, true)
	}
	app.replyComponent.SetContent(app.currentReply)
}

// onThinkingDelta handles an incremental thinking/reasoning chunk.
func (app *ChatApp) onThinkingDelta(text string) {
	app.currentThinking += text
	if app.thinkingComponent == nil {
		app.appendSpacer()
		comp := NewThinking(app.ui)
		app.history.AddChild(comp)
		app.items = append(app.items, historyEntry{component: comp})
		app.thinkingComponent = comp
	}
	app.thinkingComponent.SetText(app.currentThinking)
}

// onToolCall handles a tool-call event (new tool or args update for existing one).
func (app *ChatApp) onToolCall(id, name, input string) {
	app.flushReply()
	if comp, ok := app.pendingTools[id]; ok {
		if input != "" {
			comp.UpdateArgs(json.RawMessage(input))
		}
	} else {
		app.appendToolExecution(id, name, json.RawMessage(input))
	}
}

// onToolInputDelta handles an incremental input delta for a pending tool call.
func (app *ChatApp) onToolInputDelta(id, input string) {
	if comp, ok := app.pendingTools[id]; ok {
		comp.AppendInputDelta(input)
	}
}

// onToolResult handles the result of a completed tool call.
func (app *ChatApp) onToolResult(id, output string, isError bool, mediaType, mediaDataBase64 string) {
	if comp, ok := app.pendingTools[id]; ok {
		comp.UpdateResultWithMedia(output, isError, mediaType, mediaDataBase64)
		delete(app.pendingTools, id)
	}
}

// onStreaming handles the start of a new streaming response.
func (app *ChatApp) onStreaming() {
	app.flushReply()
	app.state = stateRunning
}

// ---------------------------------------------------------------------------
// Input handling
// ---------------------------------------------------------------------------

func (app *ChatApp) handleSubmit(text string) {
	text = strings.TrimSpace(text)

	if text == "" {
		return
	}

	var compacting bool
	app.dispatchSync(func(_ tui.UIToken) {
		compacting = app.compacting
	})
	if compacting {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage("compaction in progress; wait until it completes", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
		return
	}

	resolved, err := resolvePromptSubmission(context.Background(), text, promptModeInteractive)
	if err != nil {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
		return
	}
	if resolved.builtin != nil {
		app.handleBuiltinCommand(*resolved.builtin)
		return
	}

	app.runPromptWithOptions(resolved.promptText, resolved.opts)
}

type queuedPrompt struct {
	Input string
	Opts  promptRunOptions
}

func (app *ChatApp) dispatchSync(fn func(tui.UIToken)) bool {
	return app.ui.DispatchSync(context.WithoutCancel(context.Background()), fn)
}

func (app *ChatApp) runPromptWithOptions(input string, opts promptRunOptions) {
	current := queuedPrompt{Input: strings.TrimSpace(input), Opts: opts}
	if app.trySteer(current) {
		return
	}
	app.runOnce(current)
}

// runOnce executes a single prompt turn: renders the user message, calls the agent, then finalises the UI.
func (app *ChatApp) runOnce(current queuedPrompt) {
	// Render user message and transition to running state (on UI goroutine).
	app.dispatchSync(func(_ tui.UIToken) {
		app.appendSpacer()
		if current.Opts.TemplateName != "" {
			userInput := current.Input
			if userInput == "" {
				userInput = "/" + current.Opts.TemplateName
			}
			app.appendMessage(userInput, theme.BorderThick, theme.Default.Accent.Ansi24, theme.Default.Foreground.Ansi24, false)
			notice := "↪ /" + current.Opts.TemplateName + " template expanded"
			app.appendMessage(notice, theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		} else {
			app.appendMessage(current.Input, theme.BorderThick, theme.Default.Accent.Ansi24, theme.Default.Foreground.Ansi24, false)
		}
		app.state = stateRunning
		app.currentReply = ""
		app.replyComponent = nil
		app.refreshStatus()
		app.refreshHeader()
	})

	// Check session availability (on UI goroutine).
	var sessionOK bool
	app.dispatchSync(func(_ tui.UIToken) {
		sessionOK = app.session != nil && app.session.Agent() != nil && app.session.ID() != ""
	})
	if !sessionOK {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage("No model configured. Use /models add to set up.", theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			app.state = stateIdle
			app.stopLoader()
			app.refreshStatus()
		})
		return
	}

	prepared, err := preparePromptInput(current.Input, app.session.Paths())
	if err != nil {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			app.state = stateIdle
			app.stopLoader()
			app.refreshStatus()
		})
		return
	}
	// Run agent (blocking, off UI goroutine).
	sessionID := app.session.ID()
	maxTokens := app.modelCfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = libagent.DefaultMaxTokens
	}
	err = app.session.Agent().Run(context.Background(), agent.SessionAgentCall{
		SessionID:       sessionID,
		Prompt:          prepared.text,
		Attachments:     prepared.attachments,
		MaxOutputTokens: maxTokens,
	})

	// Finalize (on UI goroutine).
	var applyThinkingLevel bool
	app.dispatchSync(func(_ tui.UIToken) {
		app.flushReply()
		app.cancelPendingTools()
		if err != nil && err != tools.ErrCancelled && !strings.Contains(err.Error(), "context canceled") {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		}
		app.state = stateIdle
		applyThinkingLevel = app.thinkingLevelDirty
		app.thinkingLevelDirty = false
		app.stopLoader()
		app.refreshStatus()
		app.refreshHeader()
	})
	if applyThinkingLevel && app.session != nil {
		newModel, rebuildErr := rebuildRuntimeModel(app.modelCfg)
		if rebuildErr == nil {
			app.runtimeModel = newModel
			if reconfigureErr := app.session.Reconfigure(app.runtimeModel); reconfigureErr != nil {
				app.dispatchSync(func(_ tui.UIToken) {
					app.appendMessage("failed to apply thinking level: "+reconfigureErr.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
				})
			}
		}
	}
}

func (app *ChatApp) trySteer(current queuedPrompt) bool {
	var running bool
	var sessionID string
	var sessionOK bool
	app.dispatchSync(func(_ tui.UIToken) {
		running = app.state == stateRunning
		sessionOK = app.session != nil && app.session.Agent() != nil && app.session.ID() != ""
		if sessionOK {
			sessionID = app.session.ID()
		}
	})
	if !running {
		return false
	}
	if !sessionOK {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage("cannot steer: no active session", theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
		return true
	}

	prepared, err := preparePromptInput(current.Input, app.session.Paths())
	if err != nil {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
		return true
	}

	if err := app.session.Agent().Steer(context.Background(), agent.SessionAgentCall{
		SessionID:   sessionID,
		Prompt:      prepared.text,
		Attachments: prepared.attachments,
	}); err != nil {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage("failed to queue steering: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
		return true
	}

	preview := current.Input
	if current.Opts.TemplateName != "" {
		if preview == "" {
			preview = "/" + current.Opts.TemplateName
		}
		preview += " (template)"
	}
	preview = strings.TrimSpace(preview)
	if preview == "" {
		preview = "(no text)"
	}
	runes := []rune(preview)
	if len(runes) > 80 {
		preview = string(runes[:79]) + "…"
	}
	app.dispatchSync(func(_ tui.UIToken) {
		app.appendMessage("↪ steering queued: "+preview, theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Muted.Ansi24, false)
	})
	return true
}

func (app *ChatApp) handleBuiltinCommand(cmd builtinCommandCall) {
	switch {
	case cmd.name == "exit":
		app.dispatchSync(func(_ tui.UIToken) { app.quit() })
	case cmd.name == "new":
		if err := app.reloadFromScratch(""); err != nil {
			app.dispatchSync(func(_ tui.UIToken) {
				app.appendMessage("failed to reset session: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			})
		}
	case cmd.name == "sessions":
		app.dispatchSync(func(_ tui.UIToken) { app.showSessionSelector() })
	case cmd.name == "tree":
		app.dispatchSync(func(_ tui.UIToken) { app.showTreeSelector() })
	case cmd.name == "compact":
		instructions := strings.TrimSpace(cmd.args)
		go func() {
			if err := app.compactConversation(instructions); err != nil {
				app.dispatchSync(func(_ tui.UIToken) {
					app.appendMessage("/compact failed: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
				})
			}
		}()
	case cmd.name == "help":
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendSpacer()
			app.appendMessage(helpText(), theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
	case cmd.name == "templates":
		app.dispatchSync(func(_ tui.UIToken) { app.showTemplates() })
	case cmd.name == "models" && len(cmd.fields) == 1:
		app.dispatchSync(func(_ tui.UIToken) { app.showModelSelector() })
	case cmd.name == "models" && len(cmd.fields) == 2 && cmd.fields[1] == "add":
		app.dispatchSync(func(_ tui.UIToken) { app.showModelAdd() })
	default:
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage("unknown command: "+cmd.name, theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
	}
}

// reloadFromScratch is the single full-reset path for chat runtime + UI.
// It should be used by all commands/features that need a fresh Raijin state.
func (app *ChatApp) reloadFromScratch(editorText string) error {
	if app.session != nil {
		if err := app.session.Clear(context.Background()); err != nil {
			return err
		}
	}
	app.dispatchSync(func(_ tui.UIToken) {
		// Rebuild autocompletion and recreate welcome block from refreshed plugin/skill catalogs.
		app.setupAutocompleteProvider()
		app.resetConversationView(true)
		app.editor.SetText(editorText)
		app.stopLoader()
		app.refreshHeader()
		app.refreshStatus()
	})
	app.ui.RequestRender(true)
	return nil
}

func (app *ChatApp) showTemplates() {
	templates := prompts.GetTemplates()
	if len(templates) == 0 {
		app.appendSpacer()
		app.appendMessage("no prompt templates loaded", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	reserved := builtinSlashCommands()
	var b strings.Builder
	b.WriteString("Prompt templates:\n")
	for _, tmpl := range templates {
		desc := strings.TrimSpace(tmpl.Description)
		if desc == "" {
			desc = "(no description)"
		}
		b.WriteString("- /")
		b.WriteString(tmpl.Name)
		b.WriteString(" — ")
		b.WriteString(desc)
		b.WriteString(" [")
		b.WriteString(string(tmpl.Source))
		b.WriteString("]")
		if _, blocked := reserved[tmpl.Name]; blocked {
			b.WriteString(" (reserved command, not invokable)")
		}
		b.WriteString("\n")
	}

	app.appendSpacer()
	app.appendMessage(strings.TrimSpace(b.String()), theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
}

func (app *ChatApp) showTreeSelector() {
	if app.state == stateRunning {
		app.appendMessage("cannot navigate tree while a response is running; interrupt first", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	if app.session == nil || app.session.Agent() == nil || app.session.ID() == "" {
		app.appendMessage("no active conversation", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	entries := app.session.GetTree()
	if len(entries) == 0 {
		app.appendMessage("no history to navigate", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	app.showSelector(func(done func()) tui.Component {
		return NewTreeSelector(entries,
			func(entry persist.TreeEntry) {
				done()
				go func() {
					if err := app.applyTreeNavigation(context.Background(), entry); err != nil {
						app.dispatchSync(func(_ tui.UIToken) {
							app.appendMessage("/tree failed: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
						})
					}
				}()
			},
			func() {
				done()
			},
		)
	})
}

func (app *ChatApp) applyTreeNavigation(ctx context.Context, entry persist.TreeEntry) error {
	if app.session == nil || app.session.Agent() == nil || app.session.ID() == "" {
		return errors.New("no active session")
	}

	editorText, err := app.session.Navigate(entry.ID)
	if err != nil {
		return err
	}

	app.dispatchSync(func(_ tui.UIToken) {
		app.resetConversationView(false)
		app.restoreHistoryFromSession(ctx)
		app.editor.SetText(editorText)
		app.ui.SetFocus(app.editor)
		app.refreshHeader()
		app.refreshStatus()
	})
	app.ui.RequestRender(true)
	return nil
}

// ---------------------------------------------------------------------------
// Selector (editor swap pattern)
// ---------------------------------------------------------------------------

// showSelector replaces the editor area with a custom component.
// The done callback restores the editor when the selector is closed.
func (app *ChatApp) showSelector(create func(done func()) tui.Component) {
	app.nextModalID++
	modalID := app.nextModalID

	done := func() {
		if app.activeModalID != modalID {
			return
		}
		app.activeModalDone = nil
		app.activeModalID = 0
		app.editorContainer.Clear()
		app.editorContainer.AddChild(app.editor)
		app.ui.SetFocus(app.editor)
		app.ui.RequestRender()
	}
	comp := create(done)
	app.activeModalDone = done
	app.activeModalID = modalID
	app.editorContainer.Clear()
	app.editorContainer.AddChild(comp)
	app.ui.SetFocus(comp)
	app.ui.RequestRender()
}

func (app *ChatApp) modelSwitchBlocked() bool {
	if app.state != stateRunning {
		return false
	}
	app.appendMessage("cannot switch models while a response is running; interrupt first", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
	return true
}

func (app *ChatApp) showModelSelector() {
	if app.modelSwitchBlocked() {
		return
	}
	if app.store == nil {
		app.appendMessage("no model store available", theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}
	if len(app.store.List()) == 0 {
		app.appendMessage("no models configured", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	currentModel := app.store.DefaultName()
	app.showSelector(func(done func()) tui.Component {
		return NewModelSelector(app.store, currentModel, "SELECT MODEL",
			func(name string) {
				app.applyModelChoice(name)
				done()
			},
			func(name string) {
				if err := app.store.Delete(name); err != nil {
					app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
				} else {
					app.appendSpacer()
					app.appendMessage("deleted model: "+name, theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, false)
					app.refreshHeader()
				}
			},
			func() {
				done()
			},
		)
	})
}

func (app *ChatApp) applyModelChoice(name string) {
	if app.modelSwitchBlocked() {
		return
	}
	if app.store == nil {
		return
	}
	modelCfg, ok := app.store.Get(name)
	if !ok {
		app.appendMessage("model not found: "+name, theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}
	if err := app.store.SetDefault(name); err != nil {
		app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}
	newModel, err := rebuildRuntimeModel(modelCfg)
	if err != nil {
		app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}
	app.runtimeModel = newModel
	app.modelCfg = modelCfg
	if app.session != nil {
		if err := app.session.Reconfigure(app.runtimeModel); err != nil {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			return
		}
	}
	app.appendSpacer()
	app.appendMessage("switched to "+name, theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, false)
	app.refreshHeader()
}

func (app *ChatApp) showModelAdd() {
	if app.modelSwitchBlocked() {
		return
	}
	providerKeys := make(map[string]string)
	if app.store != nil {
		for _, name := range app.store.List() {
			if mc, ok := app.store.Get(name); ok && mc.APIKey != "" {
				if providerKeys[mc.Provider] == "" {
					providerKeys[mc.Provider] = mc.APIKey
				}
			}
		}
	}

	app.showSelector(func(done func()) tui.Component {
		return NewModelAdd(providerKeys,
			func(result ModelAddResult) {
				app.applyModelAdd(result)
				done()
			},
			func() {
				done()
			},
		)
	})
}

func (app *ChatApp) applyModelAdd(result ModelAddResult) {
	if app.modelSwitchBlocked() {
		return
	}
	if app.store == nil {
		app.appendMessage("no model store available", theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	if strings.EqualFold(result.ProviderID, libagent.CodexProviderID) && strings.TrimSpace(result.APIKey) == "" {
		app.appendSpacer()
		app.appendMessage("starting OpenAI Codex OAuth login...", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		cat := libagent.DefaultCatalog()
		cat.SetLoginCallbacks(libagent.LoginCallbacksWithPrinter(func(msg string) {
			app.appendMessage(msg, theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		}))
		loginModelID := strings.TrimSpace(result.ModelID)
		if loginModelID == "" {
			loginModelID = "gpt-5.3-codex"
		}
		_, err := cat.NewModel(context.Background(), libagent.CodexProviderID, loginModelID, "")
		if err != nil {
			app.appendMessage("openai-codex login failed: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			return
		}
		app.appendMessage("openai-codex login successful", theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, false)
	}

	maxTokens := result.MaxTokens
	if maxTokens <= 0 {
		maxTokens = libagent.DefaultMaxTokens
	}
	if result.ContextWindow > 0 && maxTokens >= result.ContextWindow {
		maxTokens = max(result.ContextWindow/2, 1)
	}

	thinkingLevel := libagent.ThinkingLevelMedium
	modelCfg := libagent.ModelConfig{
		Name:          result.ProviderID + "/" + result.ModelID,
		Provider:      result.ProviderID,
		Model:         result.ModelID,
		APIKey:        result.APIKey,
		MaxTokens:     maxTokens,
		ContextWindow: result.ContextWindow,
		ThinkingLevel: thinkingLevel,
	}
	if result.BaseURL != "" {
		modelCfg.BaseURL = &result.BaseURL
	}

	if err := app.store.Add(modelCfg); err != nil {
		app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}
	_ = app.store.SetDefault(modelCfg.Name)

	newModel, err := rebuildRuntimeModel(modelCfg)
	if err != nil {
		app.appendMessage("failed to configure model: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}
	app.runtimeModel = newModel
	app.modelCfg = modelCfg
	if app.session != nil {
		if err := app.session.Reconfigure(app.runtimeModel); err != nil {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			return
		}
	}
	app.appendSpacer()
	app.appendMessage("model configured: "+result.ProviderID+"/"+result.ModelID, theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, false)
	app.refreshHeader()
}

// rebuildRuntimeModel creates a new RuntimeModel from a ModelConfig using the catalog.
func rebuildRuntimeModel(cfg libagent.ModelConfig) (libagent.RuntimeModel, error) {
	cfg = cfg.Normalize()
	cat := libagent.DefaultCatalog()
	apiKey := cfg.APIKey
	if after, ok := strings.CutPrefix(apiKey, "$"); ok {
		apiKey = os.Getenv(after)
	}
	model, err := cat.NewModel(context.Background(), cfg.Provider, cfg.Model, apiKey)
	if err != nil {
		return libagent.RuntimeModel{}, fmt.Errorf("building model %s/%s: %w", cfg.Provider, cfg.Model, err)
	}
	info, _, _ := cat.FindModel(cfg.Provider, cfg.Model)
	providerType, catalogOpts := cat.FindModelOptions(cfg.Provider, cfg.Model)
	return libagent.RuntimeModel{
		Model:                  model,
		ModelInfo:              info,
		ModelCfg:               cfg,
		ProviderType:           providerType,
		CatalogProviderOptions: catalogOpts,
	}, nil
}

// ---------------------------------------------------------------------------
// Run interruption
// ---------------------------------------------------------------------------

func (app *ChatApp) interruptRun() {
	if app.compacting || app.state != stateRunning {
		return
	}
	if app.session != nil && app.session.Agent() != nil && app.session.ID() != "" {
		app.session.Agent().Cancel(app.session.ID())
	}
	app.flushReply()
	app.cancelPendingTools()
	app.appendMessage("(interrupted)", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Muted.Ansi24, false)
	// Keep state as stateRunning and the loader visible (changed to "Interrupting…")
	// until runOnce() finalizes. This prevents new submits from being mis-routed
	// as steering while the agent goroutine is still unwinding.
	app.showStatusLoader("Interrupting…")
}

// cancelPendingTools stops loaders and marks all pending tool calls as cancelled.
func (app *ChatApp) cancelPendingTools() {
	for id, comp := range app.pendingTools {
		comp.MarkCancelled()
		delete(app.pendingTools, id)
	}
}

// ---------------------------------------------------------------------------
// Block expand/collapse
// ---------------------------------------------------------------------------

func (app *ChatApp) toggleBlocksExpanded() {
	type expandable interface{ SetExpanded(bool) }
	app.blocksExpanded = !app.blocksExpanded
	expanded := app.blocksExpanded
	for i := range app.items {
		if e, ok := app.items[i].component.(expandable); ok {
			e.SetExpanded(expanded)
		}
	}
}
