# Refactoring Opportunities: Better Leverage `libagent`

## Goal
Reduce `internal/` orchestration complexity by moving duplicated or provider-specific behavior closer to `libagent` primitives.

## 1) Unify resource parsing for `Run` and `Steer` (Done)
- Problem:
  - Attachment and skill parsing logic is duplicated in chat prompt submission and steering paths.
- Current duplication:
  - `internal/chat/chat.go` (`runOnce` parse/build path)
  - `internal/chat/chat.go` (`trySteer` parse/build path)
- Refactor:
  - Extract a single helper that converts user input into:
    - parsed text
    - binary attachments
    - skill attachments
    - skill script registration side effects
- Expected outcome:
  - Less drift between normal prompt and steering behavior.
  - Lower bug surface when changing resource parsing rules.

## 2) Remove stop-boundary bookkeeping where queue semantics already suffice (Done)
- Problem:
  - `SessionAgent` still tracks stop requests and boundary cancellation despite all-at-once steering/follow-up.
- Current complexity:
  - `internal/agent/session_agent.go`
    - `stopRequests`
    - `RequestStop`
    - `consumeStopRequest`
    - `isStopBoundaryEvent`
- Refactor:
  - Prefer `libagent` queue behavior (`Steer`/`FollowUp`) plus explicit `Cancel` for hard stop.
  - Remove boundary stop-state if no longer required by UX semantics.
- Expected outcome:
  - Cleaner control flow in event loop.
  - Fewer race-prone state transitions.

## 3) Move media capability adaptation into `libagent` (Done)
- Problem:
  - Model capability policy is implemented in `internal/agent`, not near model/runtime machinery.
- Current internal policy code:
  - `internal/agent/session_agent.go`
    - `normalizeHistoryForModelCapabilities`
    - `stripMediaFromMessages`
    - `adaptToolsForImageCapability`
    - `mediaDisabledTool`
- Refactor:
  - Introduce runtime/model options in `libagent` for media-capability handling.
  - Apply adaptation centrally in `libagent` during prompt/tool execution setup.
- Expected outcome:
  - `internal/agent` becomes thinner and less provider-aware.
  - Consistent behavior for all `libagent` consumers.

## 4) Reduce or remove the `internal/core.AgentEvent` shim (Done)
- Problem:
  - Chat consumes translated events instead of native `libagent` events.
- Current layering:
  - `internal/agent/session_agent.go` maps `libagent.AgentEvent` -> `core.AgentEvent`
  - `internal/chat/chat.go` consumes `core.AgentEvent`
- Refactor:
  - Either pass through `libagent.AgentEvent` directly, or make `core.AgentEvent` a strict alias-style minimal wrapper.
- Expected outcome:
  - Less translation glue code.
  - Lower risk of event field drift and missed cases.

## 5) Introduce persistence hooks/sinks in `libagent` (Pending)
- Problem:
  - `SessionAgent` manually persists each event/message branch.
- Current hotspot:
  - `internal/agent/session_agent.go` event handler logic for assistant/user/tool persistence.
- Refactor:
  - Add a `libagent` persistence sink interface for message lifecycle events.
  - Move generic persistence sequencing close to the event source.
- Expected outcome:
  - Smaller `SessionAgent`.
  - More deterministic, reusable persistence flow for future consumers.

## Suggested implementation order
1. Unify parsing/building in chat (`Run` + `Steer`).
2. Simplify stop-boundary logic.
3. Move media adaptation into `libagent`.
4. Revisit event shim (`core.AgentEvent`).
5. Design and adopt `libagent` persistence sink.

## Notes
- Preserve current user-visible behavior while refactoring.
- Keep replay/live parity as a hard requirement.
- Add targeted tests per step before broadening changes.
