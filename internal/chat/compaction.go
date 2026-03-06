package chat

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/theme"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
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

	// Persist backend is append-only: store a compaction checkpoint marker and
	// reconstruct context from it instead of rewriting message history.
	firstKeptID := firstPersistedMessageID(kept)
	if firstKeptID == "" {
		return fmt.Errorf("cannot compact: no persisted message ID in kept window")
	}
	if err := app.session.AppendCompaction(summary, firstKeptID, estimateConversationTokens(msgs)); err != nil {
		return fmt.Errorf("failed to append compaction entry: %w", err)
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

func generateCompactionSummary(ctx context.Context, model libagent.RuntimeModel, msgs []libagent.Message, customInstructions string) (string, error) {
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

func findCompactionCutIndex(msgs []libagent.Message, keepRecentTokens int64) int {
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

func findTokenBudgetCutIndex(msgs []libagent.Message, keepRecentTokens int64) int {
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

func isValidCompactionCutIndex(msgs []libagent.Message, cut int) bool {
	if cut <= 0 || cut >= len(msgs) {
		return false
	}
	if _, ok := msgs[cut].(*libagent.ToolResultMessage); ok {
		return false
	}
	return hasBijectiveToolCoupling(msgs[:cut]) && hasBijectiveToolCoupling(msgs[cut:])
}

func hasBijectiveToolCoupling(msgs []libagent.Message) bool {
	callCounts := make(map[string]int)
	resultCounts := make(map[string]int)

	for _, msg := range msgs {
		switch m := msg.(type) {
		case *libagent.AssistantMessage:
			for _, call := range m.ToolCalls {
				id := strings.TrimSpace(call.ID)
				if id == "" {
					return false
				}
				callCounts[id]++
			}
		case *libagent.ToolResultMessage:
			id := strings.TrimSpace(m.ToolCallID)
			if id == "" {
				return false
			}
			resultCounts[id]++
		}
	}

	if len(callCounts) != len(resultCounts) {
		return false
	}
	for id, count := range callCounts {
		if count != 1 || resultCounts[id] != 1 {
			return false
		}
	}
	for id, count := range resultCounts {
		if count != 1 || callCounts[id] != 1 {
			return false
		}
	}
	return true
}

func estimateMessageTokens(msg libagent.Message) int64 {
	var chars int64
	switch m := msg.(type) {
	case *libagent.UserMessage:
		chars += int64(len(m.Content))
		for _, f := range m.Files {
			if strings.HasPrefix(f.MediaType, "text/") {
				chars += int64(len(f.Data))
			} else {
				chars += 4_800
			}
		}
	case *libagent.AssistantMessage:
		chars += int64(len(m.Text) + len(m.Reasoning) + len(m.CompleteReason) + len(m.CompleteMessage) + len(m.CompleteDetails))
		for _, tc := range m.ToolCalls {
			chars += int64(len(tc.ID) + len(tc.Name) + len(tc.Input))
		}
	case *libagent.ToolResultMessage:
		chars += int64(len(m.Content) + len(m.ToolName) + len(m.Metadata))
		if len(m.Data) > 0 {
			chars += 4_800
		}
	}
	if chars <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(chars) / 4.0))
}

func estimateConversationTokens(msgs []libagent.Message) int64 {
	var total int64
	for _, msg := range msgs {
		total += estimateMessageTokens(msg)
	}
	return total
}

func firstPersistedMessageID(msgs []libagent.Message) string {
	for _, msg := range msgs {
		if id := strings.TrimSpace(libagent.MessageID(msg)); id != "" {
			return id
		}
	}
	return ""
}

func serializeConversationForCompaction(msgs []libagent.Message) string {
	parts := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *libagent.UserMessage:
			text := strings.TrimSpace(m.Content)
			if text != "" {
				parts = append(parts, "[User]: "+text)
			}
		case *libagent.AssistantMessage:
			if thinking := strings.TrimSpace(m.Reasoning); thinking != "" {
				parts = append(parts, "[Assistant thinking]: "+thinking)
			}
			if text := strings.TrimSpace(m.Text); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
			if len(m.ToolCalls) > 0 {
				callParts := make([]string, 0, len(m.ToolCalls))
				for _, c := range m.ToolCalls {
					input := strings.TrimSpace(c.Input)
					if input == "" {
						input = "{}"
					}
					callParts = append(callParts, fmt.Sprintf("%s(input=%s)", c.Name, input))
				}
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(callParts, "; "))
			}
		case *libagent.ToolResultMessage:
			text := strings.TrimSpace(m.Content)
			if text == "" {
				continue
			}
			parts = append(parts, "[Tool result]: "+text)
		}
	}
	return strings.Join(parts, "\n\n")
}
