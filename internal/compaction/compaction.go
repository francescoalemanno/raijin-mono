package compaction

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

const (
	DefaultReserveTokens      int64 = 16_384
	DefaultKeepRecentFallback int64 = 20_000
	ApproximateOverheadTokens int64 = 2_400
	AutoFillThreshold               = 60.0
	TargetFillFraction              = 0.20
	AutoTokenThreshold        int64 = 150_000
	CheckpointPrefix                = "[Context checkpoint created by /compact]\n\n"
)

type Mode string

const (
	ModeAuto   Mode = "auto"
	ModeManual Mode = "manual"
)

type Phase string

const (
	PhaseStart  Phase = "start"
	PhaseEnd    Phase = "end"
	PhaseFailed Phase = "failed"
)

type Watch struct {
	EstimatedTokens int64
	ContextWindow   int64
	ContextPercent  float64
	TriggerByFill   bool
	TriggerByTokens bool
}

func NewWatch(msgs []libagent.Message, contextWindow int64) Watch {
	watch := Watch{
		EstimatedTokens: ApproximateConversationUsageTokens(msgs),
		ContextWindow:   contextWindow,
	}
	if contextWindow > 0 {
		watch.ContextPercent = float64(watch.EstimatedTokens) / float64(contextWindow) * 100
		watch.TriggerByFill = watch.ContextPercent >= AutoFillThreshold
	}
	watch.TriggerByTokens = watch.EstimatedTokens >= AutoTokenThreshold
	return watch
}

func (w Watch) ShouldCompact() bool {
	return w.TriggerByFill || w.TriggerByTokens
}

func (w Watch) TriggerLabel() string {
	switch {
	case w.TriggerByFill && w.TriggerByTokens:
		return fmt.Sprintf("ctx %.1f%%, %s estimated tokens", w.ContextPercent, FormatTokenCount(w.EstimatedTokens))
	case w.TriggerByFill:
		return fmt.Sprintf("ctx %.1f%%", w.ContextPercent)
	case w.TriggerByTokens:
		return fmt.Sprintf("%s estimated tokens", FormatTokenCount(w.EstimatedTokens))
	default:
		return ""
	}
}

type Options struct {
	CustomInstructions string
	PersistedIDLookup  func(libagent.Message) string
}

type Result struct {
	Watch         Watch
	Compacted     []libagent.Message
	Summary       string
	FirstKeptID   string
	TokensBefore  int64
	Summarized    int
	Kept          int
	ContextWindow int64
}

func Compact(ctx context.Context, msgs []libagent.Message, runtimeModel libagent.RuntimeModel, opts Options) (Result, error) {
	if len(msgs) == 0 {
		return Result{}, fmt.Errorf("nothing to compact")
	}

	contextWindow := runtimeModel.EffectiveContextWindow()
	keepRecentTokens := KeepRecentTokens(contextWindow, DefaultReserveTokens)
	cutIdx := FindCutIndex(msgs, keepRecentTokens)
	if cutIdx <= 0 || cutIdx >= len(msgs) {
		return Result{}, fmt.Errorf("nothing to compact")
	}

	toSummarize := msgs[:cutIdx]
	keptMsgs := msgs[cutIdx:]
	summary, err := GenerateSummary(ctx, runtimeModel, toSummarize, opts.CustomInstructions)
	if err != nil {
		return Result{}, err
	}

	firstKeptID := FirstPersistedMessageID(keptMsgs, opts.PersistedIDLookup)
	if firstKeptID == "" {
		return Result{}, fmt.Errorf("cannot compact: no persisted message ID in kept window")
	}

	compacted := make([]libagent.Message, 0, len(keptMsgs)+1)
	compacted = append(compacted, SummaryMessage(summary))
	for _, msg := range keptMsgs {
		compacted = append(compacted, libagent.CloneMessage(msg))
	}

	return Result{
		Watch:         NewWatch(msgs, contextWindow),
		Compacted:     compacted,
		Summary:       summary,
		FirstKeptID:   firstKeptID,
		TokensBefore:  EstimateConversationTokens(msgs),
		Summarized:    len(toSummarize),
		Kept:          len(keptMsgs),
		ContextWindow: contextWindow,
	}, nil
}

func KeepRecentTokens(contextWindow, reserveTokens int64) int64 {
	keepRecent := DefaultKeepRecentFallback
	if contextWindow <= 0 {
		return keepRecent
	}

	targetUsageTokens := int64(math.Floor(float64(contextWindow) * TargetFillFraction))
	if targetUsageTokens > ApproximateOverheadTokens {
		keepRecent = targetUsageTokens - ApproximateOverheadTokens
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

func GenerateSummary(ctx context.Context, model libagent.RuntimeModel, msgs []libagent.Message, customInstructions string) (string, error) {
	conversation := SerializeConversation(msgs)
	if strings.TrimSpace(conversation) == "" {
		return "", fmt.Errorf("no content to summarize")
	}

	prompt := "<conversation>\n" + conversation + "\n</conversation>\n\n" + promptTemplate
	customInstructions = strings.TrimSpace(customInstructions)
	if customInstructions != "" {
		prompt += "\n\nAdditional focus: " + customInstructions
	}

	reserve := DefaultReserveTokens
	if cw := model.EffectiveContextWindow(); cw > 0 {
		reserve = min(reserve, cw/2)
	}
	maxOut := max(int64(math.Floor(float64(reserve)*0.8)), 256)

	var sb strings.Builder
	err := libagent.StreamText(ctx, model.Model, systemPrompt, prompt, maxOut, func(delta string) {
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

func FindCutIndex(msgs []libagent.Message, keepRecentTokens int64) int {
	base := findTokenBudgetCutIndex(msgs, keepRecentTokens)
	if base <= 0 || base >= len(msgs) {
		return 0
	}

	for i := base; i < len(msgs); i++ {
		if IsValidCutIndex(msgs, i) {
			return i
		}
	}
	for i := base - 1; i > 0; i-- {
		if IsValidCutIndex(msgs, i) {
			return i
		}
	}
	return 0
}

func IsValidCutIndex(msgs []libagent.Message, cut int) bool {
	if cut <= 0 || cut >= len(msgs) {
		return false
	}
	if _, ok := msgs[cut].(*libagent.ToolResultMessage); ok {
		return false
	}
	return libagent.HasBijectiveToolCoupling(msgs[:cut]) && libagent.HasBijectiveToolCoupling(msgs[cut:])
}

func EstimateMessageTokens(msg libagent.Message) int64 {
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

func EstimateConversationTokens(msgs []libagent.Message) int64 {
	var total int64
	for _, msg := range msgs {
		total += EstimateMessageTokens(msg)
	}
	return total
}

func ApproximateConversationUsageTokens(msgs []libagent.Message) int64 {
	return ApproximateOverheadTokens + EstimateConversationTokens(msgs)
}

func FirstPersistedMessageID(msgs []libagent.Message, lookup func(libagent.Message) string) string {
	for _, msg := range msgs {
		if id := strings.TrimSpace(libagent.MessageID(msg)); id != "" {
			return id
		}
		if lookup != nil {
			if id := strings.TrimSpace(lookup(msg)); id != "" {
				return id
			}
		}
	}
	return ""
}

func SerializeConversation(msgs []libagent.Message) string {
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

func SummaryMessage(summary string) libagent.Message {
	return &libagent.UserMessage{
		Role:      "user",
		Content:   CheckpointPrefix + strings.TrimSpace(summary),
		Timestamp: time.Now(),
	}
}

func FormatTokenCount(tokens int64) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%dk", tokens/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

var systemPrompt = "You are a context summarization assistant. Do NOT continue the conversation. Output only the requested structured summary."

var promptTemplate = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

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

func findTokenBudgetCutIndex(msgs []libagent.Message, keepRecentTokens int64) int {
	if len(msgs) == 0 || keepRecentTokens <= 0 {
		return 0
	}
	var acc int64
	cut := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		acc += EstimateMessageTokens(msgs[i])
		if acc >= keepRecentTokens {
			cut = i
			break
		}
	}
	return cut
}

func normalizeReserveTokens(contextWindow, reserveTokens int64) int64 {
	if reserveTokens <= 0 {
		reserveTokens = DefaultReserveTokens
	}
	if contextWindow > 0 && reserveTokens >= contextWindow {
		reserveTokens = contextWindow / 2
	}
	if reserveTokens < 0 {
		return 0
	}
	return reserveTokens
}
