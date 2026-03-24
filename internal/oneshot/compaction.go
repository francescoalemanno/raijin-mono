package oneshot

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/session"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

const (
	defaultCompactionReserveTokens    int64 = 16_384
	defaultCompactionKeepRecentTokens int64 = 20_000
	approximateContextOverheadTokens  int64 = 2_400
	autoCompactContextFillThreshold         = 60.0
	compactionTargetFillFraction            = 0.20
	autoCompactTokenThreshold         int64 = 150_000
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

func compactionKeepRecentTokens(contextWindow, reserveTokens int64) int64 {
	keepRecent := defaultCompactionKeepRecentTokens
	if contextWindow <= 0 {
		return keepRecent
	}

	targetUsageTokens := int64(math.Floor(float64(contextWindow) * compactionTargetFillFraction))
	if targetUsageTokens > approximateContextOverheadTokens {
		keepRecent = targetUsageTokens - approximateContextOverheadTokens
	} else {
		keepRecent = max(contextWindow/5, 1)
	}

	reserve := normalizeReserveTokens(contextWindow, reserveTokens)
	maxKeep := contextWindow - reserve
	if maxKeep <= 0 {
		maxKeep = contextWindow / 2
	}
	if maxKeep > 0 {
		keepRecent = min(keepRecent, maxKeep)
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

func compactSession(ctx context.Context, sess *session.Session, runtimeModel libagent.RuntimeModel, customInstructions string) (summarized, kept int, err error) {
	if sess == nil || sess.Agent() == nil || sess.ID() == "" {
		return 0, 0, fmt.Errorf("no active session to compact")
	}

	msgs, err := sess.ListMessages(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list messages: %w", err)
	}
	if len(msgs) == 0 {
		return 0, 0, fmt.Errorf("nothing to compact")
	}

	contextWindow := runtimeModel.EffectiveContextWindow()
	keepRecentTokens := compactionKeepRecentTokens(contextWindow, defaultCompactionReserveTokens)
	cutIdx := findCompactionCutIndex(msgs, keepRecentTokens)
	if cutIdx <= 0 || cutIdx >= len(msgs) {
		return 0, 0, fmt.Errorf("nothing to compact")
	}
	toSummarize := msgs[:cutIdx]
	keptMsgs := msgs[cutIdx:]

	summary, err := generateCompactionSummary(ctx, runtimeModel, toSummarize, customInstructions)
	if err != nil {
		return 0, 0, err
	}

	firstKeptID := firstPersistedMessageID(keptMsgs)
	if firstKeptID == "" {
		return 0, 0, fmt.Errorf("cannot compact: no persisted message ID in kept window")
	}
	if err := sess.AppendCompaction(summary, firstKeptID, estimateConversationTokens(msgs)); err != nil {
		return 0, 0, fmt.Errorf("failed to append compaction entry: %w", err)
	}

	return len(toSummarize), len(keptMsgs), nil
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
	maxOut := max(int64(math.Floor(float64(reserve)*0.8)), 256)

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
	return libagent.HasBijectiveToolCoupling(msgs[:cut]) && libagent.HasBijectiveToolCoupling(msgs[cut:])
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
		chars += int64(len(libagent.AssistantText(m)) + len(libagent.AssistantReasoning(m)) + len(m.CompleteReason) + len(m.CompleteMessage) + len(m.CompleteDetails))
		for _, tc := range libagent.AssistantToolCalls(m) {
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

func approximateConversationUsageTokens(msgs []libagent.Message) int64 {
	return approximateContextOverheadTokens + estimateConversationTokens(msgs)
}

type autoCompactionWatch struct {
	estimatedTokens int64
	contextWindow   int64
	contextPercent  float64
	triggerByFill   bool
	triggerByTokens bool
}

func newAutoCompactionWatch(msgs []libagent.Message, contextWindow int64) autoCompactionWatch {
	watch := autoCompactionWatch{
		estimatedTokens: approximateConversationUsageTokens(msgs),
		contextWindow:   contextWindow,
	}
	if contextWindow > 0 {
		watch.contextPercent = float64(watch.estimatedTokens) / float64(contextWindow) * 100
		watch.triggerByFill = watch.contextPercent >= autoCompactContextFillThreshold
	}
	watch.triggerByTokens = watch.estimatedTokens >= autoCompactTokenThreshold
	return watch
}

func (w autoCompactionWatch) shouldCompact() bool {
	return w.triggerByFill || w.triggerByTokens
}

func (w autoCompactionWatch) triggerLabel() string {
	switch {
	case w.triggerByFill && w.triggerByTokens:
		return fmt.Sprintf("ctx %.1f%%, %s estimated tokens", w.contextPercent, formatStatusTokenCount(w.estimatedTokens))
	case w.triggerByFill:
		return fmt.Sprintf("ctx %.1f%%", w.contextPercent)
	case w.triggerByTokens:
		return fmt.Sprintf("%s estimated tokens", formatStatusTokenCount(w.estimatedTokens))
	default:
		return ""
	}
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
			if thinking := strings.TrimSpace(libagent.AssistantReasoning(m)); thinking != "" {
				parts = append(parts, "[Assistant thinking]: "+thinking)
			}
			if text := strings.TrimSpace(libagent.AssistantText(m)); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
			assistantCalls := libagent.AssistantToolCalls(m)
			if len(assistantCalls) > 0 {
				callParts := make([]string, 0, len(assistantCalls))
				for _, c := range assistantCalls {
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
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
