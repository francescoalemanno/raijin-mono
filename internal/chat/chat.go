package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/components"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/keys"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/terminal"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"

	"github.com/francescoalemanno/raijin-mono/internal/agent"
	chatsession "github.com/francescoalemanno/raijin-mono/internal/chat/session"
	modelconfig "github.com/francescoalemanno/raijin-mono/internal/config"
	"github.com/francescoalemanno/raijin-mono/internal/core"
	"github.com/francescoalemanno/raijin-mono/internal/input"
	"github.com/francescoalemanno/raijin-mono/internal/message"
	"github.com/francescoalemanno/raijin-mono/internal/prompts"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
	"github.com/francescoalemanno/raijin-mono/internal/substitution"
	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/internal/tools"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/catalog"
	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"
)

type chatState int

const (
	stateIdle chatState = iota
	stateRunning
)

var thinkingLevelCycleOrder = []llm.ThinkingLevel{
	llm.ThinkingLevelOff,
	llm.ThinkingLevelLow,
	llm.ThinkingLevelMedium,
	llm.ThinkingLevelHigh,
	llm.ThinkingLevelMax,
}

// historyEntry tracks a component in the history container.
type historyEntry struct {
	component tui.Component
}

type ChatApp struct {
	ui      *tui.TUI
	session *chatsession.Session
	cfg     *bridgecfg.Config
	store   *modelconfig.ModelStore

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
	state                   chatState
	compacting              bool
	currentThinking         string
	thinkingComponent       *ThinkingComponent
	currentReply            string
	replyComponent          *MessageComponent
	items                   []historyEntry
	pendingTools            map[string]*ToolExecutionComponent // tool call ID -> component
	steeringQueue           promptDeliveryQueue
	steeringInterruptIssued bool
	blocksExpanded          bool
	totalTokens             int64
	contextWindow           int64
	thinkingLevelDirty      bool
	suppressTextEvents      bool
	activeModalDone         func()
	activeModalID           uint64
	nextModalID             uint64

	// Shutdown
	done chan struct{}
}

func RunChat(cfg *bridgecfg.Config) error {
	return RunChatWithPrompt(cfg, "")
}

func RunChatWithPrompt(cfg *bridgecfg.Config, initialPrompt string) error {
	sess, err := chatsession.New(cfg)
	if err != nil && sess == nil {
		return err
	}
	store, _ := modelconfig.LoadModelStore()

	term := terminal.NewProcessTerminal()
	app := newChatApp(term, sess, cfg, store)

	app.session.SetEventCallback(func(event core.AgentEvent) {
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

func newChatApp(term terminal.Terminal, sess *chatsession.Session, cfg *bridgecfg.Config, store *modelconfig.ModelStore) *ChatApp {
	app := &ChatApp{
		session:       sess,
		cfg:           cfg,
		store:         store,
		pendingTools:  make(map[string]*ToolExecutionComponent),
		steeringQueue: newPromptDeliveryQueue(),
		done:          make(chan struct{}),
	}

	app.ui = tui.NewTUI(term)

	// Logo at top
	app.logo = components.NewText("", 0, 0, nil)
	app.ui.AddChild(app.logo)

	// History container — messages flow here, natural scrollback
	app.history = &tui.Container{}
	app.ui.AddChild(app.history)

	editorTheme := components.EditorTheme{
		BorderColor:    theme.Default.Accent.Ansi24,
		ShellLineColor: theme.Default.AccentAlt.Ansi24,
		SelectList: components.SelectListTheme{
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

	sm, _, ok := app.cfg.ActiveModel()

	// Left side: cwd + context stats
	var leftParts []string
	if cwd := renderWorkingDir(); cwd != "" {
		leftParts = append(leftParts, cwd)
	}
	if app.contextWindow > 0 {
		pct := float64(app.totalTokens) / float64(app.contextWindow) * 100
		leftParts = append(leftParts, theme.Default.Muted.Ansi24(fmt.Sprintf("%.1f%%/%s", pct, formatTokenCount(app.contextWindow))))
	}
	left := strings.Join(leftParts, "  ")

	// Right side: (provider) model • thinking
	var right string
	if ok {
		modelName := sm.Model
		thinking := string(llm.NormalizeThinkingLevel(sm.ThinkingLevel))
		right = theme.Default.Muted.Ansi24(fmt.Sprintf("(%s) %s • %s", sm.Provider, modelName, thinking))
	}

	app.header.SetInfo(left, right)
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
	sm, _, ok := app.cfg.ActiveModel()
	if !ok {
		return
	}

	nextLevel := nextThinkingLevel(sm.ThinkingLevel)
	sm.ThinkingLevel = nextLevel
	app.cfg.Model = sm

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
		if err := app.session.Reconfigure(app.cfg); err != nil {
			app.appendMessage("failed to apply thinking level: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			return
		}
	}

	app.refreshHeader()
}

func nextThinkingLevel(current llm.ThinkingLevel) llm.ThinkingLevel {
	normalized := llm.NormalizeThinkingLevel(current)
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
	app.steeringQueue = newPromptDeliveryQueue()
	app.steeringInterruptIssued = false
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

func (app *ChatApp) appendStoredMessage(msg message.Message) {
	switch msg.Role {
	case message.User:
		text := strings.TrimSpace(msg.Content().Text)
		if text != "" {
			app.appendSpacer()
			app.appendMessage(text, theme.BorderThin, theme.Default.Accent.Ansi24, theme.Default.Foreground.Ansi24, false)
		}
		for _, att := range msg.BinaryContent() {
			if !IsImageMIME(att.MIMEType) || len(att.Data) == 0 {
				continue
			}
			app.appendSpacer()
			app.appendImageFromBytes(att.Data, att.MIMEType, att.Path)
		}
	case message.Assistant:
		reasoning := strings.TrimSpace(msg.ReasoningContent().Thinking)
		if reasoning != "" {
			app.appendSpacer()
			comp := NewThinking(app.ui)
			comp.SetExpanded(app.blocksExpanded)
			comp.SetText(reasoning)
			comp.Finish()
			app.history.AddChild(comp)
			app.items = append(app.items, historyEntry{component: comp})
		}

		// Render tool calls first (they precede the text in the turn).
		for _, tc := range msg.ToolCalls() {
			var args json.RawMessage
			if tc.Input != "" {
				args = json.RawMessage(tc.Input)
			}
			app.appendToolExecution(tc.ID, tc.Name, args)
		}
		text := strings.TrimSpace(msg.Content().Text)
		if text != "" {
			app.appendSpacer()
			app.appendMessage(text, theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, true)
		}
	case message.Tool:
		for _, result := range msg.ToolResults() {
			if comp, ok := app.pendingTools[result.ToolCallID]; ok {
				// Component already created from the assistant message's ToolCall.
				comp.UpdateResultWithMedia(result.Content, result.IsError, result.MIMEType, result.Data)
				delete(app.pendingTools, result.ToolCallID)
			} else {
				// Fallback: no prior ToolCall found (e.g. older WAL without ToolCall parts).
				comp := app.appendToolExecution(result.ToolCallID, result.Name, nil)
				comp.UpdateResultWithMedia(result.Content, result.IsError, result.MIMEType, result.Data)
				delete(app.pendingTools, result.ToolCallID)
			}
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
	var tool llm.Tool
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

func (app *ChatApp) appendImageComponent(base64Data, mimeType, filename string) {
	_ = base64Data
	label := "↪ image attached"
	if filename != "" {
		label += ": " + filename
	}
	if mimeType != "" {
		label += ", " + mimeType
	}
	app.appendMessage(label, theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Muted.Ansi24, false)
}

func (app *ChatApp) appendImageFromBytes(data []byte, mimeType, filename string) {
	if !IsImageMIME(mimeType) || len(data) == 0 {
		return
	}
	app.appendImageComponent(base64.StdEncoding.EncodeToString(data), mimeType, filename)
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

func (app *ChatApp) handleEvent(event core.AgentEvent) {
	app.ui.Dispatch(func() {
		if app.suppressTextEvents {
			switch event.Kind {
			case core.EventTextDelta, core.EventThinking:
				return
			}
		}
		switch event.Kind {
		case core.EventTextDelta:
			app.onTextDelta(event)
		case core.EventThinking:
			app.onThinkingDelta(event)
		case core.EventToolCall:
			app.onToolCall(event)
		case core.EventToolInputDelta:
			app.onToolInputDelta(event)
		case core.EventToolResult:
			app.onToolResult(event)
		case core.EventStreaming:
			app.onStreaming()
		case core.EventTotalTokens:
			app.onTotalTokens(event)
		}
		app.refreshStatus()
		app.refreshHeader()
	})
}

// onTextDelta handles an incremental text chunk from the assistant.
func (app *ChatApp) onTextDelta(event core.AgentEvent) {
	app.flushThinking()
	app.currentReply += event.Text
	if app.replyComponent == nil {
		app.appendSpacer()
		app.replyComponent = app.appendMessage("", theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, true)
	}
	app.replyComponent.SetContent(app.currentReply)
}

// onThinkingDelta handles an incremental thinking/reasoning chunk.
func (app *ChatApp) onThinkingDelta(event core.AgentEvent) {
	app.currentThinking += event.Text
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
func (app *ChatApp) onToolCall(event core.AgentEvent) {
	app.flushReply()
	if comp, ok := app.pendingTools[event.ID]; ok {
		if event.Input != "" {
			comp.UpdateArgs(json.RawMessage(event.Input))
		}
	} else {
		app.appendToolExecution(event.ID, event.Name, json.RawMessage(event.Input))
	}
}

// onToolInputDelta handles an incremental input delta for a pending tool call.
func (app *ChatApp) onToolInputDelta(event core.AgentEvent) {
	if comp, ok := app.pendingTools[event.ID]; ok {
		comp.AppendInputDelta(event.Input)
	}
}

// onToolResult handles the result of a completed tool call.
func (app *ChatApp) onToolResult(event core.AgentEvent) {
	if comp, ok := app.pendingTools[event.ID]; ok {
		comp.UpdateResultWithMedia(event.Output, event.IsError, event.MediaType, event.MediaDataBase64)
		delete(app.pendingTools, event.ID)
	}
	app.maybeIssueSteeringInterrupt()
}

// onStreaming handles the start of a new streaming response.
func (app *ChatApp) onStreaming() {
	app.flushReply()
	app.state = stateRunning
}

// onTotalTokens handles a token-count update.
func (app *ChatApp) onTotalTokens(event core.AgentEvent) {
	app.totalTokens = event.TotalTokens
	if event.ContextWindow > 0 {
		app.contextWindow = event.ContextWindow
	}
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

	// Commands
	if strings.HasPrefix(text, "/") {
		app.handleCommand(text)
		return
	}

	text, _ = substitution.ExpandShellSubstitutions(context.Background(), text)
	app.runPromptWithOptions(text, promptRunOptions{})
}

type promptRunOptions struct {
	AllowedTools []string
	TemplateName string
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
	for {
		var enqueued bool
		app.dispatchSync(func(_ tui.UIToken) {
			if app.state == stateRunning {
				app.enqueueSteering(current.Input, current.Opts)
				app.maybeIssueSteeringInterrupt()
				app.refreshStatus()
				enqueued = true
			}
		})
		if enqueued {
			return
		}
		next, hasNext := app.runOnce(current)
		if !hasNext {
			return
		}
		current = next
	}
}

// runOnce executes a single prompt turn: renders the user message, calls the
// agent, then finalises the UI and returns any queued follow-up prompt.
func (app *ChatApp) runOnce(current queuedPrompt) (next queuedPrompt, hasNext bool) {
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
			if len(current.Opts.AllowedTools) > 0 {
				notice += " (tools limited: " + strings.Join(current.Opts.AllowedTools, ", ") + ")"
			}
			app.appendMessage(notice, theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		} else {
			app.appendMessage(current.Input, theme.BorderThick, theme.Default.Accent.Ansi24, theme.Default.Foreground.Ansi24, false)
		}
		app.state = stateRunning
		app.steeringInterruptIssued = false
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
			app.steeringInterruptIssued = false
			app.stopLoader()
			app.refreshStatus()
		})
		return queuedPrompt{}, false
	}

	// Parse resources (can happen off UI goroutine).
	text, files, skills, err := input.ParseAndLoadResources(current.Input)
	if err != nil {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			app.state = stateIdle
			app.steeringInterruptIssued = false
			app.stopLoader()
			app.refreshStatus()
		})
		return queuedPrompt{}, false
	}

	var attachments []message.BinaryContent
	for _, f := range files {
		attachments = append(attachments, message.BinaryContent{
			Path:     f.Path,
			MIMEType: f.MediaType,
			Data:     f.Data,
		})
	}

	if len(attachments) > 0 {
		app.dispatchSync(func(_ tui.UIToken) {
			for _, att := range attachments {
				if !IsImageMIME(att.MIMEType) || len(att.Data) == 0 {
					continue
				}
				app.appendImageFromBytes(att.Data, att.MIMEType, att.Path)
			}
		})
	}

	paths := app.session.Paths()
	var skillAttachments []message.SkillContent
	for _, loaded := range skills {
		skillAttachments = append(skillAttachments, message.SkillContent{
			Name:    loaded.Name,
			Content: loaded.Content,
		})
		tools.RegisterSkillScriptsPath(paths, loaded.ScriptsDir)
	}
	if len(skills) > 0 {
		app.dispatchSync(func(_ tui.UIToken) {
			for _, loaded := range skills {
				app.appendMessage("↪ "+loaded.Name+" skill loaded", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Muted.Ansi24, false)
			}
		})
	}

	// Run agent (blocking, off UI goroutine).
	sessionID := app.session.ID()
	_, err = app.session.Agent().Run(context.Background(), agent.SessionAgentCall{
		SessionID:       sessionID,
		Prompt:          text,
		Attachments:     attachments,
		Skills:          skillAttachments,
		AllowedTools:    current.Opts.AllowedTools,
		MaxOutputTokens: int64(app.cfg.MaxTokens()),
	})

	// Handle context overflow: run compaction and auto-retry
	if isContextOverflow(err) {
		app.dispatchSync(func(_ tui.UIToken) {
			app.flushReply()
			app.cancelPendingTools()
			app.state = stateIdle
			app.steeringInterruptIssued = false
			app.refreshStatus()
		})

		compactErr := app.compactConversation("")
		if compactErr != nil {
			app.dispatchSync(func(_ tui.UIToken) {
				app.stopLoader()
				app.state = stateIdle
				app.appendMessage("Compaction failed, retry aborted: "+compactErr.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
				app.refreshStatus()
			})
			return queuedPrompt{}, false
		}

		// Compaction succeeded: set up for retry
		app.dispatchSync(func(_ tui.UIToken) {
			app.stopLoader()
			app.refreshHeader()
			// Reset state for retry
			app.currentReply = ""
			app.replyComponent = nil
			app.currentThinking = ""
			app.thinkingComponent = nil
		})

		// Return same prompt for retry
		return current, true
	}

	// Finalize (on UI goroutine).
	var nextPrompt queuedPrompt
	var hasNextPrompt bool
	var applyThinkingLevel bool
	app.dispatchSync(func(_ tui.UIToken) {
		app.flushReply()
		app.cancelPendingTools()
		if err != nil && err != tools.ErrCancelled && !strings.Contains(err.Error(), "context canceled") {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		}
		app.state = stateIdle
		app.steeringInterruptIssued = false
		applyThinkingLevel = app.thinkingLevelDirty
		app.thinkingLevelDirty = false
		nextPrompt, hasNextPrompt = app.dequeueSteering()
		app.stopLoader()
		app.refreshStatus()
		app.refreshHeader()
	})
	if applyThinkingLevel && app.session != nil {
		if reconfigureErr := app.session.Reconfigure(app.cfg); reconfigureErr != nil {
			app.dispatchSync(func(_ tui.UIToken) {
				app.appendMessage("failed to apply thinking level: "+reconfigureErr.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			})
		}
	}
	if err == nil {
		app.maybeAutoCompact()
	}
	return nextPrompt, hasNextPrompt
}

func (app *ChatApp) enqueueSteering(input string, opts promptRunOptions) {
	app.steeringQueue.Enqueue(queuedPrompt{Input: input, Opts: opts})

	preview := input
	if opts.TemplateName != "" {
		if preview == "" {
			preview = "/" + opts.TemplateName
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

	app.appendMessage("↪ steering queued: "+preview, theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Muted.Ansi24, false)
}

func (app *ChatApp) maybeIssueSteeringInterrupt() {
	if app.state != stateRunning || app.steeringInterruptIssued || app.steeringQueue.Len() == 0 {
		return
	}

	ag := app.session
	if ag == nil || ag.Agent() == nil || ag.ID() == "" {
		app.steeringInterruptIssued = true
		return
	}

	if len(app.pendingTools) > 0 {
		ag.Agent().RequestStop(ag.ID())
	} else {
		ag.Agent().Cancel(ag.ID())
	}
	app.steeringInterruptIssued = true
}

func (app *ChatApp) dequeueSteering() (queuedPrompt, bool) {
	return app.steeringQueue.Dequeue()
}

func (app *ChatApp) handleCommand(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
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

	// Extract the first token to identify the command
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return
	}
	cmd := fields[0]
	if !strings.HasPrefix(cmd, "/") {
		return
	}
	args := input[len(cmd):] // preserve original spacing for args
	cmd = cmd[1:]
	switch {
	case cmd == "exit":
		app.dispatchSync(func(_ tui.UIToken) { app.quit() })
	case cmd == "new":
		if err := app.reloadFromScratch(""); err != nil {
			app.dispatchSync(func(_ tui.UIToken) {
				app.appendMessage("failed to reset session: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			})
		}
	case cmd == "sessions":
		app.dispatchSync(func(_ tui.UIToken) { app.showSessionSelector() })
	case cmd == "fork":
		app.dispatchSync(func(_ tui.UIToken) { app.showForkSelector() })
	case cmd == "compact":
		instructions := strings.TrimSpace(args)
		go func() {
			if err := app.compactConversation(instructions); err != nil {
				app.dispatchSync(func(_ tui.UIToken) {
					app.appendMessage("/compact failed: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
				})
			}
		}()
	case cmd == "help":
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendSpacer()
			app.appendMessage(helpText(), theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
	case cmd == "templates":
		app.dispatchSync(func(_ tui.UIToken) { app.showTemplates() })
	case cmd == "models" && len(fields) == 1:
		app.dispatchSync(func(_ tui.UIToken) { app.showModelSelector() })
	case cmd == "models" && len(fields) == 2 && fields[1] == "add":
		app.dispatchSync(func(_ tui.UIToken) { app.showModelAdd() })
	default:
		if app.tryRunTemplateCommand(cmd, args) {
			return
		}
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage("unknown command: "+cmd, theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
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
	result := prompts.Load()
	if len(result.Templates) == 0 {
		app.appendSpacer()
		app.appendMessage("no prompt templates loaded", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	reserved := builtinSlashCommands()
	var b strings.Builder
	b.WriteString("Prompt templates:\n")
	for _, tmpl := range result.Templates {
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
		if len(tmpl.AllowedTools) > 0 {
			b.WriteString(" {tools: ")
			b.WriteString(strings.Join(tmpl.AllowedTools, ", "))
			b.WriteString("}")
		}
		b.WriteString("\n")
	}
	if len(result.Diagnostics) > 0 {
		b.WriteString("\nTemplate collisions:\n")
		for _, d := range result.Diagnostics {
			b.WriteString("- /")
			b.WriteString(d.Name)
			b.WriteString(": ")
			b.WriteString(d.Message)
			b.WriteString("\n")
		}
	}

	app.appendSpacer()
	app.appendMessage(strings.TrimSpace(b.String()), theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
}

func (app *ChatApp) tryRunTemplateCommand(name, argsString string) bool {
	result := prompts.Load()
	tmpl, found := result.Find(name)
	if !found {
		return false
	}

	if _, reserved := builtinSlashCommands()[tmpl.Name]; reserved {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage("template /"+tmpl.Name+" is reserved by a built-in command", theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
		return true
	}
	argsString = strings.TrimSpace(argsString)
	if argsString == "" && templateNeedsArguments(tmpl.Content) {
		app.dispatchSync(func(_ tui.UIToken) {
			app.appendMessage("template /"+tmpl.Name+" requires arguments", theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		})
		return true
	}

	allowedTools := core.DedupeSorted(tmpl.AllowedTools)
	if len(allowedTools) > 0 {
		var sessionTools []llm.Tool
		app.dispatchSync(func(_ tui.UIToken) {
			if app.session != nil {
				sessionTools = app.session.Tools()
			}
		})
		unknown := core.FilterUnknown(allowedTools, sessionTools)
		if len(unknown) > 0 {
			app.dispatchSync(func(_ tui.UIToken) {
				app.appendMessage(
					"template /"+tmpl.Name+" has unknown allowed-tools: "+strings.Join(unknown, ", "),
					theme.BorderThin,
					theme.Default.Danger.Ansi24,
					theme.Default.Foreground.Ansi24,
					false,
				)
			})
			return true
		}
	}

	expanded := substitution.ExpandAll(context.Background(), strings.TrimSpace(tmpl.Content), argsString, substitution.ArgModeList)
	app.runPromptWithOptions(expanded, promptRunOptions{
		AllowedTools: allowedTools,
		TemplateName: tmpl.Name,
	})
	return true
}

func templateNeedsArguments(content string) bool {
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\\':
			i++
		case '$':
			if strings.HasPrefix(content[i:], "$@") || strings.HasPrefix(content[i:], "$ARGUMENTS") || strings.HasPrefix(content[i:], "${@:") {
				return true
			}
			if i+1 < len(content) && content[i+1] >= '1' && content[i+1] <= '9' {
				return true
			}
		}
	}
	return unescapedToken(content, "{{ARGUMENTS}}")
}

func unescapedToken(content, token string) bool {
	for i := 0; i < len(content); i++ {
		if content[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(content[i:], token) {
			return true
		}
	}
	return false
}

func (app *ChatApp) showForkSelector() {
	if app.state == stateRunning {
		app.appendMessage("cannot fork while a response is running; interrupt first", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	if app.session == nil || app.session.Agent() == nil || app.session.ID() == "" {
		app.appendMessage("no active conversation to fork", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}

	// loadForkCandidates does blocking I/O — run it off the UI goroutine.
	go func() {
		candidates, err := app.loadForkCandidates(context.Background())
		app.dispatchSync(func(_ tui.UIToken) {
			if err != nil {
				app.appendMessage("failed to load messages for /fork: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
				return
			}
			if len(candidates) == 0 {
				app.appendMessage("no previous user messages to fork", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
				return
			}
			app.showSelector(func(done func()) tui.Component {
				return NewForkSelector(candidates,
					func(candidate forkCandidate) {
						done()
						go func() {
							if err := app.applyForkCandidate(context.Background(), candidate); err != nil {
								app.dispatchSync(func(_ tui.UIToken) {
									app.appendMessage("/fork failed: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
								})
							}
						}()
					},
					func() {
						done()
					},
				)
			})
		})
	}()
}

func (app *ChatApp) loadForkCandidates(ctx context.Context) ([]forkCandidate, error) {
	msgs, err := app.session.ListMessages(ctx)
	if err != nil {
		return nil, err
	}
	return collectForkCandidates(msgs), nil
}

func collectForkCandidates(msgs []message.Message) []forkCandidate {
	candidates := make([]forkCandidate, 0)
	ordinal := 0
	for _, msg := range msgs {
		if msg.Role != message.User {
			continue
		}
		prompt := strings.TrimSpace(msg.Content().Text)
		if prompt == "" {
			continue
		}
		ordinal++
		candidates = append(candidates, forkCandidate{
			MessageID: msg.ID,
			Prompt:    prompt,
			Preview:   buildForkPreview(prompt, 90),
			Ordinal:   ordinal,
		})
	}

	for i, j := 0, len(candidates)-1; i < j; i, j = i+1, j-1 {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}
	return candidates
}

func buildForkPreview(prompt string, maxRunes int) string {
	if maxRunes <= 1 {
		maxRunes = 1
	}
	normalized := strings.Join(strings.Fields(prompt), " ")
	runes := []rune(normalized)
	if len(runes) <= maxRunes {
		return normalized
	}
	return string(runes[:maxRunes-1]) + "…"
}

func (app *ChatApp) applyForkCandidate(ctx context.Context, candidate forkCandidate) error {
	if app.session == nil || app.session.Agent() == nil || app.session.ID() == "" {
		return errors.New("no active session")
	}

	msgs, err := app.session.ListMessages(ctx)
	if err != nil {
		return err
	}

	// Collect only the messages that precede the fork point (exclude the
	// selected message and everything after it).
	var keep []message.Message
	for _, msg := range msgs {
		if msg.ID == candidate.MessageID {
			break
		}
		keep = append(keep, msg)
	}

	// Create a new durable session pre-populated with those messages.
	if err := app.session.ForkTo(ctx, keep); err != nil {
		return err
	}

	app.dispatchSync(func(_ tui.UIToken) {
		app.resetConversationView(false)
		app.restoreHistoryFromSession(ctx)
		app.editor.SetText(candidate.Prompt)
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
	if app.store == nil || app.cfg == nil {
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
	applyModelConfig(app.cfg, modelCfg)
	if app.session != nil {
		if err := app.session.Reconfigure(app.cfg); err != nil {
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
	if app.cfg != nil && app.cfg.Providers != nil {
		for id, pc := range app.cfg.Providers {
			if pc.APIKey != "" {
				providerKeys[id] = pc.APIKey
			}
		}
	}
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

	if strings.EqualFold(result.ProviderID, catalog.OpenAICodexProviderID) && strings.TrimSpace(result.APIKey) == "" {
		app.appendSpacer()
		app.appendMessage("starting OpenAI Codex OAuth login...", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		if _, err := bridgecfg.EnsureOpenAICodexOAuth(context.Background(), func(msg string) {
			app.appendMessage(msg, theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Foreground.Ansi24, false)
		}); err != nil {
			app.appendMessage("openai-codex login failed: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			return
		}
		app.appendMessage("openai-codex login successful", theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, false)
	}

	maxTokens := int(result.MaxTokens)
	if maxTokens == 0 {
		maxTokens = bridgecfg.DefaultMaxTokens
	}
	if result.ContextWindow > 0 && int64(maxTokens) >= result.ContextWindow {
		maxTokens = int(result.ContextWindow / 2)
		if maxTokens < 1 {
			maxTokens = 1
		}
	}

	thinkingLevel := llm.ThinkingLevelOff
	if result.CanReason {
		thinkingLevel = llm.ThinkingLevelMedium
	}
	modelCfg := bridgecfg.SelectedModel{
		Name:          result.ProviderID + "/" + result.ModelID,
		Provider:      result.ProviderID,
		Model:         result.ModelID,
		APIKey:        result.APIKey,
		MaxTokens:     int64(maxTokens),
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

	applyModelConfig(app.cfg, modelCfg)
	if err := app.cfg.ConfigureProviders(); err != nil {
		app.appendMessage("failed to configure providers: "+err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
		return
	}
	if app.session != nil {
		if err := app.session.Reconfigure(app.cfg); err != nil {
			app.appendMessage(err.Error(), theme.BorderThin, theme.Default.Danger.Ansi24, theme.Default.Foreground.Ansi24, false)
			return
		}
	}
	app.appendSpacer()
	app.appendMessage("model configured: "+result.ProviderID+"/"+result.ModelID, theme.BorderThin, theme.Default.Success.Ansi24, theme.Default.Foreground.Ansi24, false)
	app.refreshHeader()
}

func applyModelConfig(current *bridgecfg.Config, modelCfg bridgecfg.SelectedModel) {
	providerCfg := modelCfg.ToProviderConfig()
	current.Providers[providerCfg.ID] = providerCfg
	current.Model = modelCfg.Normalize()
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
	app.steeringQueue.Clear()
	app.steeringInterruptIssued = false
	app.appendMessage("(interrupted)", theme.BorderThin, theme.Default.Muted.Ansi24, theme.Default.Muted.Ansi24, false)
	app.stopLoader()
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
