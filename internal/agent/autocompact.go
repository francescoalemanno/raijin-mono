package agent

import (
	"context"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/compaction"
	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func (a *SessionAgent) autoCompactTransform(_ string, runtimeModel libagent.RuntimeModel, messageIDs *messageIDIndex) libagent.TransformContextFn {
	return func(ctx context.Context, msgs []libagent.Message) ([]libagent.Message, error) {
		if len(msgs) == 0 {
			return msgs, nil
		}

		watch := compaction.NewWatch(msgs, runtimeModel.EffectiveContextWindow())
		if !watch.ShouldCompact() {
			return msgs, nil
		}

		start := contextCompactionEvent(libagent.ContextCompactionPhaseStart, libagent.ContextCompactionModeAuto, watch, compaction.EstimateConversationTokens(msgs), 0, 0, "")
		a.emitContextCompactionEvent(start)

		result, err := compaction.Compact(ctx, msgs, runtimeModel, compaction.Options{
			PersistedIDLookup: messageIDs.Lookup,
		})
		if err != nil {
			a.emitContextCompactionEvent(contextCompactionEvent(libagent.ContextCompactionPhaseFailed, libagent.ContextCompactionModeAuto, watch, compaction.EstimateConversationTokens(msgs), 0, 0, err.Error()))
			return msgs, nil
		}

		end := contextCompactionEvent(libagent.ContextCompactionPhaseEnd, libagent.ContextCompactionModeAuto, result.Watch, result.TokensBefore, result.Summarized, result.Kept, "")
		if err := a.store.AppendCompactionWithEvents(start, result.Summary, result.FirstKeptID, result.TokensBefore, end); err != nil {
			a.emitContextCompactionEvent(contextCompactionEvent(libagent.ContextCompactionPhaseFailed, libagent.ContextCompactionModeAuto, result.Watch, result.TokensBefore, 0, 0, err.Error()))
			return msgs, nil
		}

		a.emitContextCompactionEvent(end)
		return result.Compacted, nil
	}
}

func (a *SessionAgent) emitContextCompactionEvent(ev libagent.ContextCompactionEvent) {
	cb := a.EventCallback()
	if cb == nil {
		return
	}
	cb(libagent.AgentEvent{
		Type:              libagent.AgentEventTypeContextCompaction,
		ContextCompaction: cloneContextCompactionEvent(ev),
	})
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

func cloneContextCompactionEvent(ev libagent.ContextCompactionEvent) *libagent.ContextCompactionEvent {
	clone := ev
	return &clone
}
