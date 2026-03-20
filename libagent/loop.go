package libagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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

// runLoop is the shared inner loop: it performs LLM calls and tool executions
// until the agent reaches a terminal assistant response.
// firstTurn indicates that a turn_start was already emitted by the caller.
func runLoop(
	ctx context.Context,
	currentCtx *AgentContext,
	cfg AgentLoopConfig,
	eventCh chan<- AgentEvent,
	firstTurn bool,
) ([]Message, error) {
	newMessages := make([]Message, 0, 8)
	hasMoreToolCalls := true

	for hasMoreToolCalls {
		if !firstTurn {
			sendEvent(eventCh, AgentEvent{Type: AgentEventTypeTurnStart})
		} else {
			firstTurn = false
		}

		var (
			assistantMsg *AssistantMessage
			plannedCalls []plannedToolCall
			err          error
		)
		for attempt := 0; ; attempt++ {
			assistantMsg, plannedCalls, err = streamAssistantResponse(ctx, currentCtx, cfg, eventCh)
			if err == nil {
				break
			}
			var retryErr *retryableStreamError
			if errors.As(err, &retryErr) {
				if attempt < maxRetries {
					if waitErr := emitRetryAndWait(ctx, attempt+1, maxRetries, eventCh, retryErr); waitErr != nil {
						return newMessages, waitErr
					}
					continue
				}
				finalErr := fmt.Errorf("failed after %d retries: %w", maxRetries, retryErr)
				assistantMsg = errorAssistantMessage(finalErr)
				currentCtx.Messages = append(currentCtx.Messages, assistantMsg)
				sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: assistantMsg})
				sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: assistantMsg})
				plannedCalls = nil
				break
			}
			return newMessages, err
		}
		newMessages = append(newMessages, assistantMsg)

		var turnErr error
		if assistantMsg.FinishReason == fantasy.FinishReasonError {
			if assistantMsg.Error != nil {
				turnErr = assistantMsg.Error
			} else {
				turnErr = fmt.Errorf("language model finished with error")
			}
			for i := range plannedCalls {
				if strings.TrimSpace(plannedCalls[i].SkipReason) != "" {
					continue
				}
				plannedCalls[i].SkipReason = fmt.Sprintf("tool execution skipped: assistant failed: %v", turnErr)
			}
		}

		hasMoreToolCalls = len(plannedCalls) > 0

		var toolResults []*ToolResultMessage
		if hasMoreToolCalls {
			produced, results, err := executeToolCalls(ctx, currentCtx.Tools, plannedCalls, eventCh)
			if err != nil {
				return newMessages, err
			}
			toolResults = results
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
		if turnErr != nil {
			return newMessages, turnErr
		}
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
) (*AssistantMessage, []plannedToolCall, error) {
	// Optional context transform (e.g. pruning).
	messages := currentCtx.Messages
	if cfg.TransformContext != nil {
		var err error
		messages, err = cfg.TransformContext(ctx, messages)
		if err != nil {
			return nil, nil, fmt.Errorf("transform context: %w", err)
		}
	}

	// Convert to fantasy messages.
	convertFn := cfg.ConvertToLLM
	if convertFn == nil {
		convertFn = DefaultConvertToLLM
	}
	llmMessages, err := convertFn(ctx, messages)
	if err != nil {
		return nil, nil, fmt.Errorf("convert to LLM messages: %w", err)
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
		if isRetryableError(err) {
			return nil, nil, &retryableStreamError{cause: err}
		}
		assistantMsg := errorAssistantMessage(err)
		currentCtx.Messages = append(currentCtx.Messages, assistantMsg)
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: assistantMsg})
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: assistantMsg})
		return assistantMsg, nil, nil
	}

	// Accumulate response and emit events.
	assistantMsg := &AssistantMessage{
		Role:      "assistant",
		Timestamp: time.Now(),
	}

	messageStarted := false

	type textState struct {
		text     string
		metadata fantasy.ProviderMetadata
	}
	type reasoningState struct {
		text     string
		metadata fantasy.ProviderMetadata
	}

	// Track active text/reasoning/tool-input builders.
	textByID := map[string]textState{}
	reasoningByID := map[string]reasoningState{}
	toolInputByID := map[string]string{}
	toolNameByID := map[string]string{}
	toolMetaByID := map[string]fantasy.ProviderMetadata{}
	toolInputOrder := make([]string, 0, 4)

	for part := range streamResp {
		if !messageStarted && part.Type != fantasy.StreamPartTypeWarnings {
			if part.Type == fantasy.StreamPartTypeError && isRetryableError(part.Error) {
				return nil, nil, &retryableStreamError{cause: part.Error}
			}
			sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: assistantMsg})
			messageStarted = true
		}
		switch part.Type {
		case fantasy.StreamPartTypeTextStart:
			textByID[part.ID] = textState{metadata: part.ProviderMetadata}
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "text_start", ID: part.ID},
			})

		case fantasy.StreamPartTypeTextDelta:
			st := textByID[part.ID]
			st.text += part.Delta
			if len(part.ProviderMetadata) > 0 {
				st.metadata = part.ProviderMetadata
			}
			textByID[part.ID] = st
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "text_delta", ID: part.ID, Delta: part.Delta},
			})

		case fantasy.StreamPartTypeTextEnd:
			if st, ok := textByID[part.ID]; ok {
				if len(part.ProviderMetadata) > 0 {
					st.metadata = part.ProviderMetadata
				}
				assistantMsg.Content = append(assistantMsg.Content, fantasy.TextContent{
					Text:             st.text,
					ProviderMetadata: st.metadata,
				})
				delete(textByID, part.ID)
			}
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "text_end", ID: part.ID},
			})

		case fantasy.StreamPartTypeReasoningStart:
			reasoningByID[part.ID] = reasoningState{
				text:     part.Delta,
				metadata: part.ProviderMetadata,
			}
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "reasoning_start", ID: part.ID, Delta: part.Delta},
			})

		case fantasy.StreamPartTypeReasoningDelta:
			st := reasoningByID[part.ID]
			st.text += part.Delta
			if len(part.ProviderMetadata) > 0 {
				st.metadata = part.ProviderMetadata
			}
			reasoningByID[part.ID] = st
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "reasoning_delta", ID: part.ID, Delta: part.Delta},
			})

		case fantasy.StreamPartTypeReasoningEnd:
			if st, ok := reasoningByID[part.ID]; ok {
				if len(part.ProviderMetadata) > 0 {
					st.metadata = part.ProviderMetadata
				}
				assistantMsg.Content = append(assistantMsg.Content, fantasy.ReasoningContent{
					Text:             st.text,
					ProviderMetadata: st.metadata,
				})
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
			if len(part.ProviderMetadata) > 0 {
				toolMetaByID[part.ID] = part.ProviderMetadata
			}
			sendEvent(eventCh, AgentEvent{
				Type:    AgentEventTypeMessageUpdate,
				Message: assistantMsg,
				Delta:   &StreamDelta{Type: "tool_input_start", ID: part.ID, ToolName: part.ToolCallName},
			})

		case fantasy.StreamPartTypeToolInputDelta:
			toolInputByID[part.ID] += part.Delta
			if len(part.ProviderMetadata) > 0 {
				toolMetaByID[part.ID] = part.ProviderMetadata
			}
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
				ToolCallID:       part.ID,
				ToolName:         part.ToolCallName,
				Input:            part.ToolCallInput,
				ProviderExecuted: part.ProviderExecuted,
				ProviderMetadata: part.ProviderMetadata,
			}
			// Align with fantasy's built-in loop semantics:
			// explicit tool_call input is authoritative; fallback to accumulated
			// tool_input deltas only when explicit input is empty.
			if accumulated, ok := toolInputByID[part.ID]; ok {
				if strings.TrimSpace(tc.Input) == "" {
					tc.Input = accumulated
				}
				delete(toolInputByID, part.ID)
				delete(toolNameByID, part.ID)
				if len(tc.ProviderMetadata) == 0 {
					tc.ProviderMetadata = toolMetaByID[part.ID]
				}
				delete(toolMetaByID, part.ID)
			}
			assistantMsg.Content = append(assistantMsg.Content, tc)

		case fantasy.StreamPartTypeFinish:
			assistantMsg.FinishReason = part.FinishReason
			assistantMsg.Usage = part.Usage

		case fantasy.StreamPartTypeSource:
			assistantMsg.Content = append(assistantMsg.Content, fantasy.SourceContent{
				SourceType:       part.SourceType,
				ID:               part.ID,
				URL:              part.URL,
				Title:            part.Title,
				ProviderMetadata: part.ProviderMetadata,
			})

		case fantasy.StreamPartTypeError:
			if isRetryableError(part.Error) {
				return nil, nil, &retryableStreamError{cause: part.Error}
			}
			assistantMsg.FinishReason = fantasy.FinishReasonError
			assistantMsg.Error = part.Error
		}
	}

	// Drain any remaining open text blocks (defensive).
	for id, st := range textByID {
		assistantMsg.Content = append(assistantMsg.Content, fantasy.TextContent{
			Text:             st.text,
			ProviderMetadata: st.metadata,
		})
		_ = id
	}
	for id, st := range reasoningByID {
		assistantMsg.Content = append(assistantMsg.Content, fantasy.ReasoningContent{
			Text:             st.text,
			ProviderMetadata: st.metadata,
		})
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
			ToolCallID:       id,
			ToolName:         toolNameByID[id],
			Input:            input,
			ProviderMetadata: toolMetaByID[id],
		})
	}
	usedToolCallIDs := collectUsedToolCallIDs(currentCtx.Messages)
	var plannedCalls []plannedToolCall
	assistantMsg.Content, plannedCalls = canonicalizeAssistantToolCalls(assistantMsg.Content, usedToolCallIDs)
	toolCalls := assistantMsg.Content.ToolCalls()
	if ctxErr := ctx.Err(); ctxErr != nil {
		for i := range plannedCalls {
			if strings.TrimSpace(plannedCalls[i].SkipReason) != "" {
				continue
			}
			plannedCalls[i].SkipReason = fmt.Sprintf("tool execution canceled: %v", ctxErr)
		}
	}

	if assistantMsg.FinishReason == "" {
		switch {
		case ctx.Err() != nil:
			assistantMsg.FinishReason = fantasy.FinishReasonError
			assistantMsg.Error = ctx.Err()
		case len(toolCalls) > 0:
			assistantMsg.FinishReason = fantasy.FinishReasonToolCalls
		default:
			assistantMsg.FinishReason = fantasy.FinishReasonStop
		}
	}
	if assistantMsg.FinishReason == fantasy.FinishReasonError && assistantMsg.Error == nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			assistantMsg.Error = ctxErr
		} else {
			assistantMsg.Error = errors.New("language model finished with error")
		}
	}
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
	if assistantMsg.FinishReason == fantasy.FinishReasonLength {
		assistantMsg.FinishReason = fantasy.FinishReasonError
		assistantMsg.Error = fmt.Errorf("language model response was truncated: output token limit reached")
	}

	if !messageStarted {
		sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageStart, Message: assistantMsg})
	}

	// Append assistant message to context.
	currentCtx.Messages = append(currentCtx.Messages, assistantMsg)

	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeMessageEnd, Message: assistantMsg})
	return assistantMsg, plannedCalls, nil
}

// executeToolCalls runs the tool calls from an assistant message, emitting events.
// It returns all produced messages in order (tool results and injected user attachments)
// and the completed ToolResultMessages.
func executeToolCalls(
	ctx context.Context,
	tools []fantasy.AgentTool,
	plannedCalls []plannedToolCall,
	eventCh chan<- AgentEvent,
) ([]Message, []*ToolResultMessage, error) {
	var produced []Message
	var results []*ToolResultMessage

	// Build a name→tool map for fast lookup.
	toolMap := make(map[string]fantasy.AgentTool, len(tools))
	for _, t := range tools {
		toolMap[t.Info().Name] = t
	}

	for _, planned := range plannedCalls {
		tc := planned.Call
		if reason := strings.TrimSpace(planned.SkipReason); reason != "" {
			skippedResult := skipToolCall(tc, reason, eventCh)
			results = append(results, skippedResult)
			produced = append(produced, skippedResult)
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			skippedResult := skipToolCall(tc, fmt.Sprintf("tool execution canceled: %v", ctxErr), eventCh)
			results = append(results, skippedResult)
			produced = append(produced, skippedResult)
			continue
		}

		if strings.TrimSpace(tc.Input) == "" {
			tc.Input = "{}"
		}

		tool, found := toolMap[tc.ToolName]
		if !found {
			skippedResult := skipToolCall(tc, fmt.Sprintf("tool not found: %s", tc.ToolName), eventCh)
			results = append(results, skippedResult)
			produced = append(produced, skippedResult)
			continue
		}

		sendEvent(eventCh, AgentEvent{
			Type:       AgentEventTypeToolExecutionStart,
			ToolCallID: tc.ToolCallID,
			ToolName:   tc.ToolName,
			ToolArgs:   tc.Input,
		})

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

		var resultContent string
		var isError bool
		var resultData []byte
		var resultMIMEType string
		var resultMetadata string
		var injectedAttachmentMsg *UserMessage

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
	}

	return produced, results, nil
}

type plannedToolCall struct {
	Call       fantasy.ToolCallContent
	SkipReason string
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
// ToolResultMessage with the given reason as an error.
func skipToolCall(tc fantasy.ToolCallContent, reason string, eventCh chan<- AgentEvent) *ToolResultMessage {
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
		ToolResult:  reason,
		ToolIsError: true,
	})

	result := &ToolResultMessage{
		Role:       "toolResult",
		ToolCallID: tc.ToolCallID,
		ToolName:   tc.ToolName,
		Content:    reason,
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

func canonicalizeAssistantToolCalls(content fantasy.ResponseContent, usedToolCallIDs map[string]struct{}) (fantasy.ResponseContent, []plannedToolCall) {
	nonTool := make(fantasy.ResponseContent, 0, len(content))
	toolCalls := content.ToolCalls()
	planned := make([]plannedToolCall, 0, len(toolCalls))

	nextToolCallID := func() string {
		seed := len(usedToolCallIDs) + 1
		for {
			id := "tool-call-" + strconv.Itoa(seed)
			seed++
			if _, exists := usedToolCallIDs[id]; !exists {
				return id
			}
		}
	}

	for _, part := range content {
		tc, ok := part.(fantasy.ToolCallContent)
		if !ok {
			nonTool = append(nonTool, part)
			continue
		}

		name := strings.TrimSpace(tc.ToolName)
		if name == "" {
			continue
		}
		id := strings.TrimSpace(tc.ToolCallID)
		if id == "" {
			id = nextToolCallID()
		}
		if _, exists := usedToolCallIDs[id]; exists {
			base := id
			suffix := 2
			for {
				candidate := base + "-" + strconv.Itoa(suffix)
				if _, used := usedToolCallIDs[candidate]; !used {
					id = candidate
					break
				}
				suffix++
			}
		}
		usedToolCallIDs[id] = struct{}{}

		originalInput := tc.Input
		input := strings.TrimSpace(tc.Input)
		skipReason := ""
		if input == "" {
			input = "{}"
		}
		if !json.Valid([]byte(input)) {
			skipReason = fmt.Sprintf(
				"invalid tool call JSON input for %q: %q\nThe arguments are incomplete or malformed. Re-issue this tool call with valid JSON.",
				name, originalInput,
			)
			input = "{}"
		}

		tc.ToolCallID = id
		tc.ToolName = name
		tc.Input = input
		planned = append(planned, plannedToolCall{
			Call:       tc,
			SkipReason: skipReason,
		})
	}

	out := make(fantasy.ResponseContent, 0, len(nonTool)+len(planned))
	out = append(out, nonTool...)
	for _, p := range planned {
		out = append(out, p.Call)
	}
	return out, planned
}

func collectUsedToolCallIDs(messages []Message) map[string]struct{} {
	used := make(map[string]struct{})
	for _, msg := range messages {
		switch m := msg.(type) {
		case *AssistantMessage:
			for _, tc := range AssistantToolCalls(m) {
				id := strings.TrimSpace(tc.ID)
				if id == "" {
					continue
				}
				used[id] = struct{}{}
			}
		case *ToolResultMessage:
			id := strings.TrimSpace(m.ToolCallID)
			if id == "" {
				continue
			}
			used[id] = struct{}{}
		}
	}
	return used
}

const (
	maxRetries = 5
	baseDelay  = 1 * time.Second
	maxDelay   = 8 * time.Second
)

type retryableStreamError struct {
	cause error
}

func (e *retryableStreamError) Error() string {
	if e == nil || e.cause == nil {
		return "retryable stream error"
	}
	return e.cause.Error()
}

func (e *retryableStreamError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func retryDelay(attempt int) time.Duration {
	delay := baseDelay * time.Duration(1<<uint(max(attempt-1, 0)))
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

func emitRetryAndWait(ctx context.Context, attempt, maxAttempts int, eventCh chan<- AgentEvent, err error) error {
	delay := retryDelay(attempt)
	prefix := "Connection error"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		prefix = err.Error()
	}
	sendEvent(eventCh, AgentEvent{Type: AgentEventTypeRetry, RetryMessage: fmt.Sprintf("%s: retry %d/%d in %v...", prefix, attempt, maxAttempts, delay)})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// isRetryableError returns true for all errors except context cancellation.
// We retry on all other errors because transient network failures are common
// and hard to reliably detect across different providers.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Check using errors.Is for standard context errors
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Retry everything else including DNS errors like "no such host"
	return true
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
