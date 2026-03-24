package agent

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

const (
	autoCompactReserveTokens      int64 = 16_384
	autoCompactKeepRecentFallback int64 = 20_000
	autoCompactOverheadTokens     int64 = 2_400
	autoCompactFillThreshold            = 60.0
	autoCompactTargetFillFraction       = 0.20
	autoCompactTokenThreshold     int64 = 150_000
	autoCompactSummaryPrefix            = "[Context checkpoint created by /compact]\n\n"
)

const autoCompactSystemPrompt = "You are a context summarization assistant. Do NOT continue the conversation. Output only the requested structured summary."

const autoCompactPromptTemplate = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

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

func (a *SessionAgent) autoCompactTransform(sessionID string, runtimeModel libagent.RuntimeModel, messageIDs *messageIDIndex) libagent.TransformContextFn {
	return func(ctx context.Context, msgs []libagent.Message) ([]libagent.Message, error) {
		if len(msgs) == 0 {
			return msgs, nil
		}

		contextWindow := runtimeModel.EffectiveContextWindow()
		watch := newLoopAutoCompactionWatch(msgs, contextWindow)
		if !watch.shouldCompact() {
			return msgs, nil
		}

		compacted, summary, firstKeptID, tokensBefore, err := autoCompactMessages(ctx, msgs, runtimeModel, messageIDs)
		if err != nil {
			return msgs, nil
		}
		if firstKeptID != "" {
			_ = a.store.AppendCompaction(summary, firstKeptID, tokensBefore)
		}
		return compacted, nil
	}
}

type loopAutoCompactionWatch struct {
	estimatedTokens int64
	contextPercent  float64
	triggerByFill   bool
	triggerByTokens bool
}

func newLoopAutoCompactionWatch(msgs []libagent.Message, contextWindow int64) loopAutoCompactionWatch {
	watch := loopAutoCompactionWatch{
		estimatedTokens: approximateLoopConversationUsageTokens(msgs),
	}
	if contextWindow > 0 {
		watch.contextPercent = float64(watch.estimatedTokens) / float64(contextWindow) * 100
		watch.triggerByFill = watch.contextPercent >= autoCompactFillThreshold
	}
	watch.triggerByTokens = watch.estimatedTokens >= autoCompactTokenThreshold
	return watch
}

func (w loopAutoCompactionWatch) shouldCompact() bool {
	return w.triggerByFill || w.triggerByTokens
}

func autoCompactMessages(ctx context.Context, msgs []libagent.Message, runtimeModel libagent.RuntimeModel, messageIDs *messageIDIndex) ([]libagent.Message, string, string, int64, error) {
	if len(msgs) == 0 {
		return nil, "", "", 0, fmt.Errorf("nothing to compact")
	}

	keepRecentTokens := autoCompactKeepRecentTokens(runtimeModel.EffectiveContextWindow(), autoCompactReserveTokens)
	cutIdx := findLoopCompactionCutIndex(msgs, keepRecentTokens)
	if cutIdx <= 0 || cutIdx >= len(msgs) {
		return nil, "", "", 0, fmt.Errorf("nothing to compact")
	}

	toSummarize := msgs[:cutIdx]
	keptMsgs := msgs[cutIdx:]
	summary, err := generateLoopCompactionSummary(ctx, runtimeModel, toSummarize)
	if err != nil {
		return nil, "", "", 0, err
	}

	compacted := make([]libagent.Message, 0, len(keptMsgs)+1)
	compacted = append(compacted, loopCompactionSummaryMessage(summary))
	for _, msg := range keptMsgs {
		compacted = append(compacted, libagent.CloneMessage(msg))
	}

	return compacted, summary, firstLoopPersistedMessageID(keptMsgs, messageIDs), estimateLoopConversationTokens(msgs), nil
}

func generateLoopCompactionSummary(ctx context.Context, model libagent.RuntimeModel, msgs []libagent.Message) (string, error) {
	conversation := serializeLoopConversationForCompaction(msgs)
	if strings.TrimSpace(conversation) == "" {
		return "", fmt.Errorf("no content to summarize")
	}

	prompt := "<conversation>\n" + conversation + "\n</conversation>\n\n" + autoCompactPromptTemplate
	reserve := autoCompactReserveTokens
	if cw := model.EffectiveContextWindow(); cw > 0 {
		reserve = min(reserve, cw/2)
	}
	maxOut := max(int64(math.Floor(float64(reserve)*0.8)), 256)

	var sb strings.Builder
	err := libagent.StreamText(ctx, model.Model, autoCompactSystemPrompt, prompt, maxOut, func(delta string) {
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

func autoCompactKeepRecentTokens(contextWindow, reserveTokens int64) int64 {
	keepRecent := autoCompactKeepRecentFallback
	if contextWindow <= 0 {
		return keepRecent
	}

	targetUsageTokens := int64(math.Floor(float64(contextWindow) * autoCompactTargetFillFraction))
	if targetUsageTokens > autoCompactOverheadTokens {
		keepRecent = targetUsageTokens - autoCompactOverheadTokens
	} else {
		keepRecent = max(contextWindow/5, 1)
	}

	reserve := normalizeLoopReserveTokens(contextWindow, reserveTokens)
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

func normalizeLoopReserveTokens(contextWindow, reserveTokens int64) int64 {
	if reserveTokens <= 0 {
		reserveTokens = autoCompactReserveTokens
	}
	if contextWindow > 0 && reserveTokens >= contextWindow {
		reserveTokens = contextWindow / 2
	}
	if reserveTokens < 0 {
		return 0
	}
	return reserveTokens
}

func findLoopCompactionCutIndex(msgs []libagent.Message, keepRecentTokens int64) int {
	if len(msgs) == 0 || keepRecentTokens <= 0 {
		return 0
	}

	var (
		acc int64
		cut int
	)
	for i := len(msgs) - 1; i >= 0; i-- {
		acc += estimateLoopMessageTokens(msgs[i])
		if acc >= keepRecentTokens {
			cut = i
			break
		}
	}
	if cut <= 0 || cut >= len(msgs) {
		return 0
	}

	for i := cut; i < len(msgs); i++ {
		if isValidLoopCompactionCutIndex(msgs, i) {
			return i
		}
	}
	for i := cut - 1; i > 0; i-- {
		if isValidLoopCompactionCutIndex(msgs, i) {
			return i
		}
	}
	return 0
}

func isValidLoopCompactionCutIndex(msgs []libagent.Message, cut int) bool {
	if cut <= 0 || cut >= len(msgs) {
		return false
	}
	if _, ok := msgs[cut].(*libagent.ToolResultMessage); ok {
		return false
	}
	return libagent.HasBijectiveToolCoupling(msgs[:cut]) && libagent.HasBijectiveToolCoupling(msgs[cut:])
}

func estimateLoopMessageTokens(msg libagent.Message) int64 {
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

func estimateLoopConversationTokens(msgs []libagent.Message) int64 {
	var total int64
	for _, msg := range msgs {
		total += estimateLoopMessageTokens(msg)
	}
	return total
}

func approximateLoopConversationUsageTokens(msgs []libagent.Message) int64 {
	return autoCompactOverheadTokens + estimateLoopConversationTokens(msgs)
}

func firstLoopPersistedMessageID(msgs []libagent.Message, messageIDs *messageIDIndex) string {
	for _, msg := range msgs {
		if id := strings.TrimSpace(libagent.MessageID(msg)); id != "" {
			return id
		}
		if id := strings.TrimSpace(messageIDs.Lookup(msg)); id != "" {
			return id
		}
	}
	return ""
}

func serializeLoopConversationForCompaction(msgs []libagent.Message) string {
	parts := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *libagent.UserMessage:
			if text := strings.TrimSpace(m.Content); text != "" {
				parts = append(parts, "[User]: "+text)
			}
		case *libagent.AssistantMessage:
			if thinking := strings.TrimSpace(libagent.AssistantReasoning(m)); thinking != "" {
				parts = append(parts, "[Assistant thinking]: "+thinking)
			}
			if text := strings.TrimSpace(libagent.AssistantText(m)); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
			if assistantCalls := libagent.AssistantToolCalls(m); len(assistantCalls) > 0 {
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
				if len(m.Data) > 0 {
					text = fmt.Sprintf("[binary attachment %d bytes, mime=%s]", len(m.Data), strings.TrimSpace(m.MIMEType))
				} else {
					text = "(empty)"
				}
			}
			prefix := "[Tool " + strings.TrimSpace(m.ToolName) + " result]"
			if m.IsError {
				prefix = "[Tool " + strings.TrimSpace(m.ToolName) + " error]"
			}
			parts = append(parts, prefix+": "+text)
		}
	}
	return strings.Join(parts, "\n")
}

func loopCompactionSummaryMessage(summary string) libagent.Message {
	return &libagent.UserMessage{
		Role:      "user",
		Content:   autoCompactSummaryPrefix + strings.TrimSpace(summary),
		Timestamp: time.Now(),
	}
}
