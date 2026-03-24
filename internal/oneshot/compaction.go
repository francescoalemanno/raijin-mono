package oneshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/compaction"
	"github.com/francescoalemanno/raijin-mono/internal/session"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func compactSession(ctx context.Context, sess *session.Session, runtimeModel libagent.RuntimeModel, customInstructions string, onEvent func(libagent.ContextCompactionEvent)) (summarized, kept int, err error) {
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

	watch := compaction.NewWatch(msgs, runtimeModel.EffectiveContextWindow())
	start := contextCompactionEvent(libagent.ContextCompactionPhaseStart, libagent.ContextCompactionModeManual, watch, compaction.EstimateConversationTokens(msgs), 0, 0, "")
	emitContextCompactionEvent(onEvent, start)

	result, err := compaction.Compact(ctx, msgs, runtimeModel, compaction.Options{
		CustomInstructions: customInstructions,
	})
	if err != nil {
		failed := contextCompactionEvent(libagent.ContextCompactionPhaseFailed, libagent.ContextCompactionModeManual, watch, compaction.EstimateConversationTokens(msgs), 0, 0, err.Error())
		emitContextCompactionEvent(onEvent, failed)
		return 0, 0, err
	}

	end := contextCompactionEvent(libagent.ContextCompactionPhaseEnd, libagent.ContextCompactionModeManual, result.Watch, result.TokensBefore, result.Summarized, result.Kept, "")
	if err := sess.AppendCompactionWithEvents(start, result.Summary, result.FirstKeptID, result.TokensBefore, end); err != nil {
		failed := contextCompactionEvent(libagent.ContextCompactionPhaseFailed, libagent.ContextCompactionModeManual, result.Watch, result.TokensBefore, 0, 0, err.Error())
		emitContextCompactionEvent(onEvent, failed)
		return 0, 0, fmt.Errorf("failed to append compaction entry: %w", err)
	}

	emitContextCompactionEvent(onEvent, end)
	return result.Summarized, result.Kept, nil
}

func emitContextCompactionEvent(onEvent func(libagent.ContextCompactionEvent), ev libagent.ContextCompactionEvent) {
	if onEvent != nil {
		onEvent(ev)
	}
}

func contextCompactionEvent(phase libagent.ContextCompactionPhase, mode libagent.ContextCompactionMode, watch compaction.Watch, tokensBefore int64, summarized, kept int, errMsg string) libagent.ContextCompactionEvent {
	return libagent.ContextCompactionEvent{
		Phase:                  phase,
		Mode:                   mode,
		TriggerEstimatedTokens: watch.EstimatedTokens,
		TriggerContextPercent:  watch.ContextPercent,
		TokensBefore:           tokensBefore,
		Summarized:             summarized,
		Kept:                   kept,
		ErrorMessage:           strings.TrimSpace(errMsg),
	}
}
