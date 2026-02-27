package chat

import (
	"context"
	"fmt"
	"math"
	"strings"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
	"github.com/francescoalemanno/raijin-mono/internal/message"
	"github.com/francescoalemanno/raijin-mono/internal/theme"
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/tui"
)

const (
	defaultCompactionReserveTokens    int64 = 16_384
	defaultCompactionKeepRecentTokens int64 = 20_000
)

func normalizeReserveTokens(contextWindow, reserveTokens int64) int64 {
	if reserveTokens <= 0 {
		reserveTokens = defaultCompactionReserveTokens
	}
	if contextWindow > 0 && reserveTokens >= contextWindow {
		reserveTokens = contextWindow / 2
	}
	if reserveTokens < 0 {
		return 0
	}
	return reserveTokens
}

func modelContextWindow(model libagent.RuntimeModel) int64 {
	return model.EffectiveContextWindow()
}

func compactionKeepRecentTokens(contextWindow, reserveTokens int64) int64 {
	keepRecent := defaultCompactionKeepRecentTokens
	if contextWindow <= 0 {
		return keepRecent
	}
	reserve := normalizeReserveTokens(contextWindow, reserveTokens)
	targetKeep := contextWindow - reserve
	if targetKeep <= 0 {
		targetKeep = contextWindow / 2
	}
	if targetKeep > 0 {
		keepRecent = min(keepRecent, targetKeep)
	}
	if keepRecent <= 0 {
		return 1
	}
	return keepRecent
}

const compactionSystemPrompt = "You are a context summarization assistant. Do NOT continue the conversation. Output only the requested structured summary."

const compactionPromptTemplate = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// compactConversation rewrites the current session history to:
// 1) a summary message
// 2) the most recent kept tail
func (app *ChatApp) compactConversation(customInstructions string) error {
	var canRun bool
	app.dispatchSync(func(_ tui.UIToken) {
		canRun = !app.compacting && app.state != stateRunning && app.session != nil && app.session.Agent() != nil && app.session.ID() != ""
		if canRun {
			app.compacting = true
			app.state = stateRunning
			app.showStatusLoader("Compacting context")
			app.refreshStatus()
		}
	})
	if !canRun {
		return fmt.Errorf("cannot compact while another operation is running")
	}

	defer app.dispatchSync(func(_ tui.UIToken) {
		app.compacting = false
		app.state = stateIdle
		app.stopLoader()
		app.refreshStatus()
		app.refreshHeader()
	})

	sessionID := app.session.ID()
	msgSvc := app.session.Agent().Messages()
	msgs, err := msgSvc.List(context.Background(), sessionID)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	model := app.session.Agent().Model()
	contextWindow := modelContextWindow(model)
	keepRecentTokens := compactionKeepRecentTokens(contextWindow, defaultCompactionReserveTokens)
	cutIdx := findCompactionCutIndex(msgs, keepRecentTokens)
	if cutIdx <= 0 || cutIdx >= len(msgs) {
		return fmt.Errorf("nothing to compact")
	}
	toSummarize := msgs[:cutIdx]
	kept := msgs[cutIdx:]

	summary, err := generateCompactionSummary(context.Background(), model, toSummarize, customInstructions)
	if err != nil {
		return err
	}

	if err := msgSvc.DeleteAll(context.Background(), sessionID); err != nil {
		return fmt.Errorf("failed to rewrite history: %w", err)
	}

	summaryText := "[Context checkpoint created by /compact]\n\n" + strings.TrimSpace(summary)
	if _, err := msgSvc.Create(context.Background(), sessionID, message.CreateParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: summaryText}},
	}); err != nil {
		if rollbackErr := restoreMessages(context.Background(), msgSvc, sessionID, msgs); rollbackErr != nil {
			return fmt.Errorf("failed to store compaction summary: %w (rollback failed: %v)", err, rollbackErr)
		}
		return fmt.Errorf("failed to store compaction summary: %w", err)
	}

	for _, msg := range kept {
		clone := msg.Clone()
		if _, err := msgSvc.Create(context.Background(), sessionID, message.CreateParams{
			Role:     clone.Role,
			Parts:    clone.Parts,
			Model:    clone.Model,
			Provider: clone.Provider,
		}); err != nil {
			if rollbackErr := restoreMessages(context.Background(), msgSvc, sessionID, msgs); rollbackErr != nil {
				return fmt.Errorf("failed to restore kept history: %w (rollback failed: %v)", err, rollbackErr)
			}
			return fmt.Errorf("failed to restore kept history: %w", err)
		}
	}

	app.dispatchSync(func(_ tui.UIToken) {
		app.resetConversationView(false)
		app.restoreHistoryFromSession(context.Background())
		app.appendSpacer()
		app.appendMessage(
			fmt.Sprintf("context compacted: summarized %d messages, kept %d", len(toSummarize), len(kept)),
			theme.BorderThin,
			theme.Default.Muted.Ansi24,
			theme.Default.Foreground.Ansi24,
			false,
		)
	})
	return nil
}

func generateCompactionSummary(ctx context.Context, model libagent.RuntimeModel, msgs []message.Message, customInstructions string) (string, error) {
	conversation := serializeConversationForCompaction(msgs)
	if strings.TrimSpace(conversation) == "" {
		return "", fmt.Errorf("no content to summarize")
	}

	prompt := "<conversation>\n" + conversation + "\n</conversation>\n\n" + compactionPromptTemplate
	customInstructions = strings.TrimSpace(customInstructions)
	if customInstructions != "" {
		prompt += "\n\nAdditional focus: " + customInstructions
	}

	reserve := defaultCompactionReserveTokens
	if cw := model.EffectiveContextWindow(); cw > 0 {
		reserve = min(reserve, cw/2)
	}
	maxOut := int64(math.Floor(float64(reserve) * 0.8))
	if maxOut < 256 {
		maxOut = 256
	}

	var sb strings.Builder
	err := libagent.StreamText(ctx, model.Model, compactionSystemPrompt, prompt, maxOut, func(delta string) {
		sb.WriteString(delta)
	})
	if err != nil {
		return "", fmt.Errorf("compaction summarization failed: %w", err)
	}

	summary := strings.TrimSpace(sb.String())
	if summary == "" {
		return "", fmt.Errorf("compaction summarization returned empty output")
	}
	return summary, nil
}

func restoreMessages(ctx context.Context, msgSvc message.Service, sessionID string, msgs []message.Message) error {
	if err := msgSvc.DeleteAll(ctx, sessionID); err != nil {
		return err
	}
	for _, msg := range msgs {
		clone := msg.Clone()
		if _, err := msgSvc.Create(ctx, sessionID, message.CreateParams{
			Role:     clone.Role,
			Parts:    clone.Parts,
			Model:    clone.Model,
			Provider: clone.Provider,
		}); err != nil {
			return err
		}
	}
	return nil
}

func findCompactionCutIndex(msgs []message.Message, keepRecentTokens int64) int {
	base := findTokenBudgetCutIndex(msgs, keepRecentTokens)
	if base <= 0 || base >= len(msgs) {
		return 0
	}

	for i := base; i < len(msgs); i++ {
		if isValidCompactionCutIndex(msgs, i) {
			return i
		}
	}
	for i := base - 1; i > 0; i-- {
		if isValidCompactionCutIndex(msgs, i) {
			return i
		}
	}
	return 0
}

func findTokenBudgetCutIndex(msgs []message.Message, keepRecentTokens int64) int {
	if len(msgs) == 0 || keepRecentTokens <= 0 {
		return 0
	}
	var acc int64
	cut := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		acc += estimateMessageTokens(msgs[i])
		if acc >= keepRecentTokens {
			cut = i
			break
		}
	}
	return cut
}

func isValidCompactionCutIndex(msgs []message.Message, cut int) bool {
	if cut <= 0 || cut >= len(msgs) {
		return false
	}
	if msgs[cut].Role == message.Tool {
		return false
	}

	keptCallIDs := make(map[string]struct{})
	for i := cut; i < len(msgs); i++ {
		if msgs[i].Role != message.Assistant {
			continue
		}
		for _, call := range msgs[i].ToolCalls() {
			id := strings.TrimSpace(call.ID)
			if id != "" {
				keptCallIDs[id] = struct{}{}
			}
		}
	}

	for i := cut; i < len(msgs); i++ {
		if msgs[i].Role != message.Tool {
			continue
		}
		for _, result := range msgs[i].ToolResults() {
			id := strings.TrimSpace(result.ToolCallID)
			if id == "" {
				return false
			}
			if _, ok := keptCallIDs[id]; !ok {
				return false
			}
		}
	}
	return true
}

func estimateMessageTokens(msg message.Message) int64 {
	var chars int64
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case message.TextContent:
			chars += int64(len(p.Text))
		case message.ReasoningContent:
			chars += int64(len(p.Thinking))
		case message.ToolCall:
			chars += int64(len(p.Name) + len(p.Input) + len(p.ID))
		case message.ToolResult:
			chars += int64(len(p.Content) + len(p.Metadata) + len(p.Name))
			if p.Data != "" {
				chars += 4_800
			}
		case message.BinaryContent:
			chars += 4_800
		case message.SkillContent:
			chars += int64(len(p.Name) + len(p.Content))
		case message.Finish:
			chars += int64(len(p.Message) + len(p.Details) + len(p.Reason))
		}
	}
	if chars <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(chars) / 4.0))
}

func serializeConversationForCompaction(msgs []message.Message) string {
	parts := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		switch msg.Role {
		case message.User:
			text := strings.TrimSpace(msg.Content().Text)
			if text != "" {
				parts = append(parts, "[User]: "+text)
			}
		case message.Assistant:
			if thinking := strings.TrimSpace(msg.ReasoningContent().Thinking); thinking != "" {
				parts = append(parts, "[Assistant thinking]: "+thinking)
			}
			if text := strings.TrimSpace(msg.Content().Text); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
			calls := msg.ToolCalls()
			if len(calls) > 0 {
				callParts := make([]string, 0, len(calls))
				for _, c := range calls {
					input := strings.TrimSpace(c.Input)
					if input == "" {
						input = "{}"
					}
					callParts = append(callParts, fmt.Sprintf("%s(input=%s)", c.Name, input))
				}
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(callParts, "; "))
			}
		case message.Tool:
			results := msg.ToolResults()
			for _, r := range results {
				text := strings.TrimSpace(r.Content)
				if text == "" {
					continue
				}
				parts = append(parts, "[Tool result]: "+text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
