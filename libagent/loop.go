package libagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
)

// AgentLoop runs a new agent turn starting from the given prompt messages.
// It pushes events to eventCh and signals completion on done.
// The caller is responsible for closing nothing; the function closes done when done.
//
// Returns the slice of new messages added during this loop
// (prompts + assistant + tool outputs + injected attachment user messages).
func AgentLoop(
	ctx context.Context,
	prompts []Message,
	agentCtx *AgentContext,
	cfg AgentLoopConfig,
	eventCh chan<- AgentEvent,
) ([]Message, error) {
	newMessages := make([]Message, 0, len(prompts)+4)
	newMessages = append(newMessages, prompts...)

	currentCtx := &AgentContext{
		SystemPrompt: agentCtx.SystemPrompt,
		Messages:     append(append([]Message{}, agentCtx.Messages...), prompts...),
		Tools:        agentCtx.Tools,
	}

	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeAgentStart})
	defer func() {
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeAgentEnd, Messages: newMessages})
	}()
	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeTurnStart})

	for _, p := range prompts {
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: p})
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: p})
	}

	added, err := runLoop(ctx, currentCtx, cfg, eventCh, true)
	if err != nil {
		return newMessages, err
	}
	newMessages = append(newMessages, added...)
	return newMessages, nil
}

// AgentLoopContinue resumes an agent loop from the existing context without adding new messages.
// The last message in context must not be an assistant message.
func AgentLoopContinue(
	ctx context.Context,
	agentCtx *AgentContext,
	cfg AgentLoopConfig,
	eventCh chan<- AgentEvent,
) ([]Message, error) {
	newMessages := []Message(nil)
	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeAgentStart})
	defer func() {
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeAgentEnd, Messages: newMessages})
	}()

	if len(agentCtx.Messages) == 0 {
		return nil, fmt.Errorf("cannot continue: no messages in context")
	}
	last := agentCtx.Messages[len(agentCtx.Messages)-1]
	if last.GetRole() == "assistant" {
		return nil, fmt.Errorf("cannot continue from message role: assistant")
	}

	currentCtx := &AgentContext{
		SystemPrompt: agentCtx.SystemPrompt,
		Messages:     append([]Message{}, agentCtx.Messages...),
		Tools:        agentCtx.Tools,
	}
	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeTurnStart})

	var err error
	newMessages, err = runLoop(ctx, currentCtx, cfg, eventCh, true)
	if err != nil {
		return newMessages, err
	}
	return newMessages, nil
}

// runLoop is the shared inner loop: it performs LLM calls, tool executions,
// steering injection, and follow-up message handling until the agent is done.
// firstTurn indicates that a turn_start was already emitted by the caller.
func runLoop(
	ctx context.Context,
	currentCtx *AgentContext,
	cfg AgentLoopConfig,
	eventCh chan<- AgentEvent,
	firstTurn bool,
) ([]Message, error) {
	newMessages := make([]Message, 0, 8)

	// Poll for steering messages at the very start (user may have typed while we were queued).
	var pendingMessages []Message
	if cfg.GetSteeringMessages != nil {
		sm, err := cfg.GetSteeringMessages(ctx)
		if err != nil {
			return newMessages, fmt.Errorf("get steering messages: %w", err)
		}
		pendingMessages = sm
	}

	for {
		hasMoreToolCalls := true
		var steeringAfterTools []Message

		// Inner loop: process tool calls and steering messages.
		for hasMoreToolCalls || len(pendingMessages) > 0 {
			if !firstTurn {
				sendEvent(eventCh, AgentEvent{Type: AgentEventTypeTurnStart})
			} else {
				firstTurn = false
			}

			// Inject pending (steering) messages.
			if len(pendingMessages) > 0 {
				for _, m := range pendingMessages {
					sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: m})
					sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: m})
					currentCtx.Messages = append(currentCtx.Messages, m)
					newMessages = append(newMessages, m)
				}
				pendingMessages = nil
			}

			// Stream the assistant response.
			assistantMsg, err := streamAssistantResponse(ctx, currentCtx, cfg, eventCh)
			if err != nil {
				return newMessages, err
			}
			newMessages = append(newMessages, assistantMsg)

			// Check for error/abort finish.
			if assistantMsg.FinishReason == fantasy.FinishReasonError {
				sendEvent(eventCh, AgentEvent{
					Type:        AgentEventTypeTurnEnd,
					TurnMessage: assistantMsg,
				})
				if assistantMsg.Error != nil {
					return newMessages, assistantMsg.Error
				}
				return newMessages, fmt.Errorf("language model finished with error")
			}

			// Collect tool calls from the response.
			toolCalls := assistantMsg.Content.ToolCalls()
			hasMoreToolCalls = len(toolCalls) > 0

			var toolResults []*ToolResultMessage
			if hasMoreToolCalls {
				produced, results, steering, err := executeToolCalls(ctx, currentCtx.Tools, toolCalls, cfg.GetSteeringMessages, eventCh)
				if err != nil {
					return newMessages, err
				}
				toolResults = results
				steeringAfterTools = steering
				for _, m := range produced {
					currentCtx.Messages = append(currentCtx.Messages, m)
					newMessages = append(newMessages, m)
				}
			}

			sendEvent(eventCh, AgentEvent{
				Type:        AgentEventTypeTurnEnd,
				TurnMessage: assistantMsg,
				ToolResults: toolResults,
			})

			// Resolve next steering messages.
			if len(steeringAfterTools) > 0 {
				pendingMessages = steeringAfterTools
				steeringAfterTools = nil
			} else if cfg.GetSteeringMessages != nil {
				sm, err := cfg.GetSteeringMessages(ctx)
				if err != nil {
					return newMessages, fmt.Errorf("get steering messages: %w", err)
				}
				pendingMessages = sm
			}
		}

		// Agent would stop. Check for follow-up messages.
		if cfg.GetFollowUpMessages != nil {
			fm, err := cfg.GetFollowUpMessages(ctx)
			if err != nil {
				return newMessages, fmt.Errorf("get follow-up messages: %w", err)
			}
			if len(fm) > 0 {
				pendingMessages = fm
				continue
			}
		}

		break
	}

	return newMessages, nil
}

// streamAssistantResponse performs a single LLM streaming call and returns the
// completed AssistantMessage, also emitting message_start/update/end events.
func streamAssistantResponse(
	ctx context.Context,
	currentCtx *AgentContext,
	cfg AgentLoopConfig,
	eventCh chan<- AgentEvent,
) (*AssistantMessage, error) {
	// Optional context transform (e.g. pruning).
	messages := currentCtx.Messages
	if cfg.TransformContext != nil {
		var err error
		messages, err = cfg.TransformContext(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("transform context: %w", err)
		}
	}

	// Convert to fantasy messages.
	convertFn := cfg.ConvertToLLM
	if convertFn == nil {
		convertFn = DefaultConvertToLLM
	}
	llmMessages, err := convertFn(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("convert to LLM messages: %w", err)
	}

	// Build system message.
	systemPrompt := currentCtx.SystemPrompt
	if cfg.SystemPromptOverride != nil {
		systemPrompt = *cfg.SystemPromptOverride
	}
	prompt := fantasy.Prompt{}
	if systemPrompt != "" {
		prompt = append(prompt, fantasy.NewSystemMessage(systemPrompt))
	}
	prompt = append(prompt, llmMessages...)

	// Build tool list.
	tools := make([]fantasy.Tool, 0, len(currentCtx.Tools))
	for _, t := range currentCtx.Tools {
		info := t.Info()
		tools = append(tools, fantasy.FunctionTool{
			Name:        info.Name,
			Description: info.Description,
			InputSchema: buildInputSchema(info),
		})
	}

	// Stream from the model.
	toolChoice := fantasy.ToolChoiceAuto
	call := fantasy.Call{
		Prompt:          prompt,
		Tools:           tools,
		ToolChoice:      &toolChoice,
		ProviderOptions: cfg.ProviderOptions,
		MaxOutputTokens: cfg.MaxOutputTokens,
	}
	streamResp, err := cfg.Model.Stream(ctx, call)
	if err != nil {
		assistantMsg := errorAssistantMessage(err)
		currentCtx.Messages = append(currentCtx.Messages, assistantMsg)
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: assistantMsg})
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: assistantMsg})
		return assistantMsg, nil
	}

	// Accumulate response and emit events.
	assistantMsg := &AssistantMessage{
		Role:      "assistant",
		Timestamp: time.Now(),
	}

	// Emit message_start with empty message.
	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: assistantMsg})

	// Track active text/tool-input builders.
	textByID := map[string]string{}
	reasoningByID := map[string]string{}
	toolInputByID := map[string]string{}
	toolNameByID := map[string]string{}
	toolInputOrder := make([]string, 0, 4)

	for part := range streamResp {
		switch part.Type {
		case fantasy.StreamPartTypeTextStart:
			textByID[part.ID] = ""
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "text_start", ID: part.ID},
			})

		case fantasy.StreamPartTypeTextDelta:
			textByID[part.ID] += part.Delta
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "text_delta", ID: part.ID, Delta: part.Delta},
			})

		case fantasy.StreamPartTypeTextEnd:
			if text, ok := textByID[part.ID]; ok {
				assistantMsg.Content = append(assistantMsg.Content, fantasy.TextContent{Text: text})
				delete(textByID, part.ID)
			}
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "text_end", ID: part.ID},
			})

		case fantasy.StreamPartTypeReasoningStart:
			reasoningByID[part.ID] = part.Delta
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "reasoning_start", ID: part.ID, Delta: part.Delta},
			})

		case fantasy.StreamPartTypeReasoningDelta:
			reasoningByID[part.ID] += part.Delta
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "reasoning_delta", ID: part.ID, Delta: part.Delta},
			})

		case fantasy.StreamPartTypeReasoningEnd:
			if text, ok := reasoningByID[part.ID]; ok {
				assistantMsg.Content = append(assistantMsg.Content, fantasy.ReasoningContent{Text: text})
				delete(reasoningByID, part.ID)
			}
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "reasoning_end", ID: part.ID},
			})

		case fantasy.StreamPartTypeToolInputStart:
			if _, exists := toolInputByID[part.ID]; !exists {
				toolInputOrder = append(toolInputOrder, part.ID)
			}
			toolInputByID[part.ID] = ""
			toolNameByID[part.ID] = part.ToolCallName
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "tool_input_start", ID: part.ID, ToolName: part.ToolCallName},
			})

		case fantasy.StreamPartTypeToolInputDelta:
			toolInputByID[part.ID] += part.Delta
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "tool_input_delta", ID: part.ID, Delta: part.Delta},
			})

		case fantasy.StreamPartTypeToolInputEnd:
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "tool_input_end", ID: part.ID},
			})

		case fantasy.StreamPartTypeToolCall:
			tc := fantasy.ToolCallContent{
				ToolCallID: part.ID,
				ToolName:   part.ToolCallName,
				Input:      part.ToolCallInput,
			}
			// If we have accumulated input via deltas, prefer that.
			if accumulated, ok := toolInputByID[part.ID]; ok {
				tc.Input = accumulated
				delete(toolInputByID, part.ID)
				delete(toolNameByID, part.ID)
			}
			assistantMsg.Content = append(assistantMsg.Content, tc)

		case fantasy.StreamPartTypeFinish:
			assistantMsg.FinishReason = part.FinishReason
			assistantMsg.Usage = part.Usage

		case fantasy.StreamPartTypeError:
			assistantMsg.FinishReason = fantasy.FinishReasonError
			assistantMsg.Error = part.Error
		}
	}

	// Drain any remaining open text blocks (defensive).
	for id, text := range textByID {
		assistantMsg.Content = append(assistantMsg.Content, fantasy.TextContent{Text: text})
		_ = id
	}
	for id, text := range reasoningByID {
		assistantMsg.Content = append(assistantMsg.Content, fantasy.ReasoningContent{Text: text})
		_ = id
	}
	// Some providers stream tool_input_* blocks but do not emit a final tool_call
	// part. Synthesize any still-pending tool calls so the loop can continue.
	for _, id := range toolInputOrder {
		input, ok := toolInputByID[id]
		if !ok {
			continue
		}
		assistantMsg.Content = append(assistantMsg.Content, fantasy.ToolCallContent{
			ToolCallID: id,
			ToolName:   toolNameByID[id],
			Input:      input,
		})
	}
	toolCalls := assistantMsg.Content.ToolCalls()
	// Enforce loop invariants so callers get actionable errors instead of
	// silently stopping in inconsistent states.
	if assistantMsg.FinishReason == fantasy.FinishReasonToolCalls && len(toolCalls) == 0 {
		assistantMsg.FinishReason = fantasy.FinishReasonError
		assistantMsg.Error = fmt.Errorf("language model requested tool calls but returned none")
	}
	if assistantMsg.FinishReason == fantasy.FinishReasonStop &&
		assistantMsg.Content.Text() == "" &&
		len(toolCalls) == 0 {
		assistantMsg.FinishReason = fantasy.FinishReasonError
		assistantMsg.Error = fmt.Errorf("language model returned no final response")
	}

	// Append assistant message to context.
	currentCtx.Messages = append(currentCtx.Messages, assistantMsg)

	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: assistantMsg})
	return assistantMsg, nil
}

// executeToolCalls runs the tool calls from an assistant message, emitting events.
// It returns all produced messages in order (tool results and injected user attachments),
// completed ToolResultMessages, and any steering messages detected mid-run.
func executeToolCalls(
	ctx context.Context,
	tools []fantasy.AgentTool,
	toolCalls []fantasy.ToolCallContent,
	getSteeringMessages GetSteeringMessagesFn,
	eventCh chan<- AgentEvent,
) ([]Message, []*ToolResultMessage, []Message, error) {
	var produced []Message
	var results []*ToolResultMessage
	var steeringMessages []Message

	// Build a name→tool map for fast lookup.
	toolMap := make(map[string]fantasy.AgentTool, len(tools))
	for _, t := range tools {
		toolMap[t.Info().Name] = t
	}

	for i, tc := range toolCalls {
		sendEvent(eventCh, AgentEvent{
			Type:       AgentEventTypeToolExecutionStart,
			ToolCallID: tc.ToolCallID,
			ToolName:   tc.ToolName,
			ToolArgs:   tc.Input,
		})

		var resultContent string
		var isError bool
		var resultData []byte
		var resultMIMEType string
		var resultMetadata string
		var injectedAttachmentMsg *UserMessage

		if raw := strings.TrimSpace(tc.Input); raw != "" && !json.Valid([]byte(raw)) {
			resultContent = fmt.Sprintf(
				"invalid tool call JSON input for %q: %q\nThe arguments are incomplete or malformed. Re-issue this tool call with valid JSON.",
				tc.ToolName,
				tc.Input,
			)
			isError = true
		} else {
			if raw == "" {
				tc.Input = "{}"
			}
			tool, found := toolMap[tc.ToolName]
			if !found {
				resultContent = fmt.Sprintf("tool not found: %s", tc.ToolName)
				isError = true
			} else {
				call := fantasy.ToolCall{ID: tc.ToolCallID, Name: tc.ToolName, Input: tc.Input}
				var resp fantasy.ToolResponse
				var runErr error

				if st, ok := tool.(StreamingAgentTool); ok {
					onUpdate := func(partial fantasy.ToolResponse) {
						sendEvent(eventCh, AgentEvent{
							Type:        AgentEventTypeToolExecutionUpdate,
							ToolCallID:  tc.ToolCallID,
							ToolName:    tc.ToolName,
							ToolArgs:    tc.Input,
							ToolResult:  partial.Content,
							ToolIsError: partial.IsError,
						})
					}
					resp, runErr = st.RunStreaming(ctx, call, onUpdate)
				} else {
					resp, runErr = tool.Run(ctx, call)
				}

				if runErr != nil {
					resultContent = runErr.Error()
					isError = true
				} else {
					resultMetadata = resp.Metadata
					isError = resp.IsError
					isMediaResponse := len(resp.Data) > 0 ||
						strings.EqualFold(resp.Type, "image") ||
						strings.EqualFold(resp.Type, string(ToolResponseTypeMedia))
					if isMediaResponse {
						resultContent = fmt.Sprintf("user will provide the attachment for tool call #%s", tc.ToolCallID)
						mediaData := normalizeMediaPayload(resp.Data)
						filename := fmt.Sprintf("tool-call-%s", tc.ToolCallID)
						if strings.TrimSpace(resp.MediaType) != "" {
							filename += "." + strings.ReplaceAll(resp.MediaType, "/", "_")
						}
						injectedAttachmentMsg = &UserMessage{
							Role:    "user",
							Content: fmt.Sprintf("attachment for tool call #%s", tc.ToolCallID),
							Files: []FilePart{{
								Filename:  filename,
								MediaType: resp.MediaType,
								Data:      mediaData,
							}},
							Timestamp: time.Now(),
						}
					} else {
						resultContent = resp.Content
						resultData = resp.Data
						resultMIMEType = resp.MediaType
					}
				}
			}
		}

		sendEvent(eventCh, AgentEvent{
			Type:        AgentEventTypeToolExecutionEnd,
			ToolCallID:  tc.ToolCallID,
			ToolName:    tc.ToolName,
			ToolArgs:    tc.Input,
			ToolResult:  resultContent,
			ToolIsError: isError,
		})

		result := &ToolResultMessage{
			Role:       "toolResult",
			ToolCallID: tc.ToolCallID,
			ToolName:   tc.ToolName,
			Content:    resultContent,
			IsError:    isError,
			Data:       resultData,
			MIMEType:   resultMIMEType,
			Metadata:   resultMetadata,
			Timestamp:  time.Now(),
		}
		results = append(results, result)
		produced = append(produced, result)

		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: result})
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: result})
		if injectedAttachmentMsg != nil {
			sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: injectedAttachmentMsg})
			sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: injectedAttachmentMsg})
			produced = append(produced, injectedAttachmentMsg)
		}

		// Check for steering messages - skip remaining tools if user interrupted.
		if getSteeringMessages != nil {
			sm, err := getSteeringMessages(ctx)
			if err != nil {
				return produced, results, nil, fmt.Errorf("get steering messages: %w", err)
			}
			if len(sm) > 0 {
				steeringMessages = sm
				// Skip remaining tool calls.
				for _, skipped := range toolCalls[i+1:] {
					skippedResult := skipToolCall(skipped, eventCh)
					results = append(results, skippedResult)
					produced = append(produced, skippedResult)
				}
				break
			}
		}
	}

	return produced, results, steeringMessages, nil
}

func normalizeMediaPayload(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err == nil && len(decoded) > 0 {
		return decoded
	}
	return data
}

// skipToolCall emits start/end events for a skipped tool call and returns a
// ToolResultMessage indicating it was skipped due to steering.
func skipToolCall(tc fantasy.ToolCallContent, eventCh chan<- AgentEvent) *ToolResultMessage {
	const skipMsg = "Skipped due to queued user message."

	sendEvent(eventCh, AgentEvent{
		Type:       AgentEventTypeToolExecutionStart,
		ToolCallID: tc.ToolCallID,
		ToolName:   tc.ToolName,
		ToolArgs:   tc.Input,
	})
	sendEvent(eventCh, AgentEvent{
		Type:        AgentEventTypeToolExecutionEnd,
		ToolCallID:  tc.ToolCallID,
		ToolName:    tc.ToolName,
		ToolArgs:    tc.Input,
		ToolResult:  skipMsg,
		ToolIsError: true,
	})

	result := &ToolResultMessage{
		Role:       "toolResult",
		ToolCallID: tc.ToolCallID,
		ToolName:   tc.ToolName,
		Content:    skipMsg,
		IsError:    true,
		Timestamp:  time.Now(),
	}
	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: result})
	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: result})
	return result
}

// buildInputSchema constructs the JSON-schema map for a tool's parameters.
func buildInputSchema(info fantasy.ToolInfo) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": info.Parameters,
	}
	if len(info.Required) > 0 {
		schema["required"] = info.Required
	}
	return schema
}

// errorAssistantMessage wraps an error into an AssistantMessage with FinishReasonError.
func errorAssistantMessage(err error) *AssistantMessage {
	return &AssistantMessage{
		Role:         "assistant",
		FinishReason: fantasy.FinishReasonError,
		Error:        err,
		Timestamp:    time.Now(),
	}
}

// sendEvent sends an event to eventCh. It is a no-op if eventCh is nil.
func sendEvent(ch chan<- AgentEvent, event AgentEvent) {
	if ch != nil {
		ch <- event
	}
}
