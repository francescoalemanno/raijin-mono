package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bridgecfg "github.com/francescoalemanno/raijin-mono/llmbridge/pkg/config"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"

	"github.com/francescoalemanno/raijin-mono/internal/message"
	"github.com/francescoalemanno/raijin-mono/internal/session"
)

type cancelingRuntime struct{}

func (r *cancelingRuntime) Stream(ctx context.Context, req llm.StreamRequest) (*llm.RunResult, error) {
	if req.Callbacks.PrepareStep != nil {
		if _, err := req.Callbacks.PrepareStep(ctx, nil); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnReasoningDelta != nil {
		if err := req.Callbacks.OnReasoningDelta("r1", "thinking..."); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnTextDelta != nil {
		if err := req.Callbacks.OnTextDelta("t1", "partial reply"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnToolInputStart != nil {
		if err := req.Callbacks.OnToolInputStart("call-1", "bash"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnToolInputDelta != nil {
		if err := req.Callbacks.OnToolInputDelta("call-1", `{"cmd":"echo`); err != nil {
			return nil, err
		}
	}
	return nil, context.Canceled
}
func (r *cancelingRuntime) ProviderID() string                                        { return "test" }
func (r *cancelingRuntime) RefreshAPIKey(ctx context.Context, newAPIKey string) error { return nil }

type cancelAfterCompletedStepRuntime struct{}

func (r *cancelAfterCompletedStepRuntime) Stream(ctx context.Context, req llm.StreamRequest) (*llm.RunResult, error) {
	// Step 1: complete and stable.
	if req.Callbacks.PrepareStep != nil {
		if _, err := req.Callbacks.PrepareStep(ctx, nil); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnTextDelta != nil {
		if err := req.Callbacks.OnTextDelta("t1", "stable step"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnStepFinish != nil {
		if err := req.Callbacks.OnStepFinish(llm.StepResult{FinishReason: llm.FinishReasonToolCalls}); err != nil {
			return nil, err
		}
	}

	// Step 2: interrupted mid-stream.
	if req.Callbacks.PrepareStep != nil {
		if _, err := req.Callbacks.PrepareStep(ctx, nil); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnReasoningDelta != nil {
		if err := req.Callbacks.OnReasoningDelta("r2", "partial thinking"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnToolInputStart != nil {
		if err := req.Callbacks.OnToolInputStart("call-2", "bash"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnToolInputDelta != nil {
		if err := req.Callbacks.OnToolInputDelta("call-2", `{"cmd":"echo`); err != nil {
			return nil, err
		}
	}
	return nil, context.Canceled
}
func (r *cancelAfterCompletedStepRuntime) ProviderID() string { return "test" }
func (r *cancelAfterCompletedStepRuntime) RefreshAPIKey(ctx context.Context, newAPIKey string) error {
	return nil
}

func TestRun_Canceled_DoesNotPersistAssistantArtifacts(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime: &cancelingRuntime{},
			ModelCfg: bridgecfg.SelectedModel{
				Model:         "test-model",
				Provider:      "test-provider",
				ContextWindow: 8192,
			},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})

	_, err = a.Run(context.Background(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "hello",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}

	msgs, err := msgService.List(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("message count = %d, want 1 (only user message)", len(msgs))
	}
	if msgs[0].Role != message.User {
		t.Fatalf("remaining message role = %q, want %q", msgs[0].Role, message.User)
	}
}

func TestRun_Canceled_PreservesCompletedEarlierSteps(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime: &cancelAfterCompletedStepRuntime{},
			ModelCfg: bridgecfg.SelectedModel{
				Model:         "test-model",
				Provider:      "test-provider",
				ContextWindow: 8192,
			},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})

	_, err = a.Run(context.Background(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "hello",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}

	msgs, err := msgService.List(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("message count = %d, want 2 (user + stable assistant step)", len(msgs))
	}
	if msgs[0].Role != message.User {
		t.Fatalf("message[0] role = %q, want %q", msgs[0].Role, message.User)
	}
	if msgs[1].Role != message.Assistant {
		t.Fatalf("message[1] role = %q, want %q", msgs[1].Role, message.Assistant)
	}
	if got := msgs[1].Content().Text; got != "stable step" {
		t.Fatalf("assistant text = %q, want %q", got, "stable step")
	}
}

type incompleteToolLifecycleRuntime struct {
	calls   int
	prompts []string
}

func (r *incompleteToolLifecycleRuntime) Stream(ctx context.Context, req llm.StreamRequest) (*llm.RunResult, error) {
	r.calls++
	r.prompts = append(r.prompts, req.Prompt)

	if req.Callbacks.PrepareStep != nil {
		if _, err := req.Callbacks.PrepareStep(ctx, nil); err != nil {
			return nil, err
		}
	}

	if r.calls == 1 {
		if req.Callbacks.OnToolCall != nil {
			if err := req.Callbacks.OnToolCall(llm.ToolCallPart{
				ToolCallID: "call-missing-result",
				ToolName:   "bash",
				InputJSON:  `{"cmd":"echo hi"}`,
			}); err != nil {
				return nil, err
			}
		}
		if req.Callbacks.OnStepFinish != nil {
			if err := req.Callbacks.OnStepFinish(llm.StepResult{FinishReason: llm.FinishReasonToolCalls}); err != nil {
				return nil, err
			}
		}
		return &llm.RunResult{}, nil
	}

	if req.Callbacks.OnTextDelta != nil {
		if err := req.Callbacks.OnTextDelta("t2", "recovered"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnStepFinish != nil {
		if err := req.Callbacks.OnStepFinish(llm.StepResult{FinishReason: llm.FinishReasonStop}); err != nil {
			return nil, err
		}
	}
	return &llm.RunResult{}, nil
}
func (r *incompleteToolLifecycleRuntime) ProviderID() string { return "test" }
func (r *incompleteToolLifecycleRuntime) RefreshAPIKey(ctx context.Context, newAPIKey string) error {
	return nil
}

type mixedToolLifecycleRuntime struct{}

func (r *mixedToolLifecycleRuntime) Stream(ctx context.Context, req llm.StreamRequest) (*llm.RunResult, error) {
	if req.Callbacks.PrepareStep != nil {
		if _, err := req.Callbacks.PrepareStep(ctx, nil); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnToolCall != nil {
		if err := req.Callbacks.OnToolCall(llm.ToolCallPart{
			ToolCallID: "call-complete",
			ToolName:   "read",
			InputJSON:  `{"path":"AGENTS.md"}`,
		}); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnToolResult != nil {
		if err := req.Callbacks.OnToolResult(llm.ToolResultPart{
			ToolCallID: "call-complete",
			Output: llm.ToolResultOutput{
				Type: llm.ToolResultOutputText,
				Text: "ok",
			},
		}, "read"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnStepFinish != nil {
		if err := req.Callbacks.OnStepFinish(llm.StepResult{FinishReason: llm.FinishReasonToolCalls}); err != nil {
			return nil, err
		}
	}
	return &llm.RunResult{}, nil
}
func (r *mixedToolLifecycleRuntime) ProviderID() string { return "test" }
func (r *mixedToolLifecycleRuntime) RefreshAPIKey(ctx context.Context, newAPIKey string) error {
	return nil
}

type noCallRuntime struct {
	called bool
}

func (r *noCallRuntime) Stream(ctx context.Context, req llm.StreamRequest) (*llm.RunResult, error) {
	r.called = true
	return &llm.RunResult{}, nil
}
func (r *noCallRuntime) ProviderID() string                                        { return "test" }
func (r *noCallRuntime) RefreshAPIKey(ctx context.Context, newAPIKey string) error { return nil }

type captureRuntime struct {
	messages []llm.Message
}

func (r *captureRuntime) Stream(ctx context.Context, req llm.StreamRequest) (*llm.RunResult, error) {
	r.messages = req.Messages
	return &llm.RunResult{}, nil
}
func (r *captureRuntime) ProviderID() string                                        { return "test" }
func (r *captureRuntime) RefreshAPIKey(ctx context.Context, newAPIKey string) error { return nil }

type stopAwareRuntime struct {
	afterStep1 chan struct{}
}

func (r *stopAwareRuntime) Stream(ctx context.Context, req llm.StreamRequest) (*llm.RunResult, error) {
	steps := make([]llm.StepResult, 0, 2)

	if req.Callbacks.PrepareStep != nil {
		if _, err := req.Callbacks.PrepareStep(ctx, nil); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnTextDelta != nil {
		if err := req.Callbacks.OnTextDelta("t1", "first step"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnStepFinish != nil {
		if err := req.Callbacks.OnStepFinish(llm.StepResult{FinishReason: llm.FinishReasonToolCalls}); err != nil {
			return nil, err
		}
	}
	steps = append(steps, llm.StepResult{FinishReason: llm.FinishReasonToolCalls})

	if r.afterStep1 != nil {
		close(r.afterStep1)
	}
	// Give the test goroutine time to call RequestStop.
	time.Sleep(20 * time.Millisecond)

	for _, stop := range req.StopWhen {
		if stop != nil && stop(steps) {
			return &llm.RunResult{Steps: steps}, nil
		}
	}

	if req.Callbacks.PrepareStep != nil {
		if _, err := req.Callbacks.PrepareStep(ctx, nil); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnTextDelta != nil {
		if err := req.Callbacks.OnTextDelta("t2", "second step"); err != nil {
			return nil, err
		}
	}
	if req.Callbacks.OnStepFinish != nil {
		if err := req.Callbacks.OnStepFinish(llm.StepResult{FinishReason: llm.FinishReasonStop}); err != nil {
			return nil, err
		}
	}
	steps = append(steps, llm.StepResult{FinishReason: llm.FinishReasonStop})

	return &llm.RunResult{Steps: steps}, nil
}
func (r *stopAwareRuntime) ProviderID() string                                        { return "test" }
func (r *stopAwareRuntime) RefreshAPIKey(ctx context.Context, newAPIKey string) error { return nil }

func TestRun_PurifiesIncompleteToolLifecycleMessages(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	runtime := &incompleteToolLifecycleRuntime{}
	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime: runtime,
			ModelCfg: bridgecfg.SelectedModel{
				Model:         "test-model",
				Provider:      "test-provider",
				ContextWindow: 8192,
			},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})

	if _, err := a.Run(context.Background(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "hello",
	}); err != nil {
		t.Fatalf("Run() unexpected error = %v", err)
	}

	if runtime.calls != 2 {
		t.Fatalf("runtime calls = %d, want 2 (retry after purification)", runtime.calls)
	}
	secondPrompt := ""
	if len(runtime.prompts) > 1 {
		secondPrompt = runtime.prompts[1]
	}
	if !strings.Contains(secondPrompt, "you attempted a tool call that did not complete and was removed") {
		t.Fatalf("second prompt = %q, want injected purification notice", secondPrompt)
	}

	msgs, err := msgService.List(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	// Expected: original user msg, retry user msg (purification notice), recovered assistant msg.
	if len(msgs) != 3 {
		t.Fatalf("message count = %d, want 3 (user + purification-notice user + recovered assistant)", len(msgs))
	}
	if msgs[0].Role != message.User {
		t.Fatalf("message[0] role = %q, want %q", msgs[0].Role, message.User)
	}
	if msgs[1].Role != message.User {
		t.Fatalf("message[1] role = %q, want %q", msgs[1].Role, message.User)
	}
	if msgs[2].Role != message.Assistant {
		t.Fatalf("message[2] role = %q, want %q", msgs[2].Role, message.Assistant)
	}
	if got := msgs[2].Content().Text; got != "recovered" {
		t.Fatalf("assistant text = %q, want %q", got, "recovered")
	}
}

func TestRun_PurificationKeepsCompleteToolLifecycle(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime: &mixedToolLifecycleRuntime{},
			ModelCfg: bridgecfg.SelectedModel{
				Model:         "test-model",
				Provider:      "test-provider",
				ContextWindow: 8192,
			},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})

	if _, err := a.Run(context.Background(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "hello",
	}); err != nil {
		t.Fatalf("Run() unexpected error = %v", err)
	}

	msgs, err := msgService.List(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("message count = %d, want 3 (user + assistant + tool)", len(msgs))
	}

	assistant := msgs[1]
	if assistant.Role != message.Assistant {
		t.Fatalf("assistant role = %q, want %q", assistant.Role, message.Assistant)
	}
	calls := assistant.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("assistant tool calls = %d, want 1", len(calls))
	}
	if calls[0].ID != "call-complete" {
		t.Fatalf("assistant tool call id = %q, want %q", calls[0].ID, "call-complete")
	}

	toolMsg := msgs[2]
	if toolMsg.Role != message.Tool {
		t.Fatalf("tool role = %q, want %q", toolMsg.Role, message.Tool)
	}
	results := toolMsg.ToolResults()
	if len(results) != 1 {
		t.Fatalf("tool results = %d, want 1", len(results))
	}
	if results[0].ToolCallID != "call-complete" {
		t.Fatalf("tool result call id = %q, want %q", results[0].ToolCallID, "call-complete")
	}
}

func TestRun_ImageAttachmentBlockedWhenModelDoesNotSupportImages(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	runtime := &noCallRuntime{}
	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime: runtime,
			ModelCfg: bridgecfg.SelectedModel{
				Model:    "blocked-model",
				Provider: "blocked-provider",
			},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})
	a.modelSupportsImagesFn = func(ctx context.Context, providerID, modelID string) (bool, bool, error) {
		return false, true, nil
	}

	_, err = a.Run(context.Background(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "describe this",
		Attachments: []message.BinaryContent{{
			Path:     "sample.png",
			MIMEType: "image/png",
			Data:     []byte{0x89, 0x50, 0x4e, 0x47},
		}},
	})
	if !errors.Is(err, ErrModelNoImageSupport) {
		t.Fatalf("Run() error = %v, want ErrModelNoImageSupport", err)
	}
	if runtime.called {
		t.Fatalf("runtime Stream should not be called when image input is blocked")
	}
}

func TestRun_ImageAttachmentAllowedWhenCapabilityUnknown(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	runtime := &noCallRuntime{}
	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime: runtime,
			ModelCfg: bridgecfg.SelectedModel{
				Model:    "unknown-model",
				Provider: "unknown-provider",
			},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})
	a.modelSupportsImagesFn = func(ctx context.Context, providerID, modelID string) (bool, bool, error) {
		return false, false, nil
	}

	_, err = a.Run(context.Background(), SessionAgentCall{
		SessionID: sess.ID,
		Prompt:    "describe this",
		Attachments: []message.BinaryContent{{
			Path:     "sample.png",
			MIMEType: "image/png",
			Data:     []byte{0x89, 0x50, 0x4e, 0x47},
		}},
	})
	if err != nil {
		t.Fatalf("Run() unexpected error = %v", err)
	}
	if !runtime.called {
		t.Fatalf("runtime Stream should be called when image capability is unknown")
	}
}

func TestRun_StripsHistoricalMediaWhenModelDoesNotSupportImages(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err = msgService.Create(context.Background(), sess.ID, message.CreateParams{
		Role: message.User,
		Parts: []message.ContentPart{
			message.TextContent{Text: "earlier context"},
			message.BinaryContent{Path: "old.png", MIMEType: "image/png", Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		},
	})
	if err != nil {
		t.Fatalf("create prior user message: %v", err)
	}
	_, err = msgService.Create(context.Background(), sess.ID, message.CreateParams{
		Role: message.Tool,
		Parts: []message.ContentPart{message.ToolResult{
			ToolCallID: "call-1",
			Name:       "read",
			Content:    "Loaded image/png content",
			Data:       "iVBORw0KGgo=",
			MIMEType:   "image/png",
		}},
	})
	if err != nil {
		t.Fatalf("create prior tool message: %v", err)
	}

	runtime := &captureRuntime{}
	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime:  runtime,
			ModelCfg: bridgecfg.SelectedModel{Model: "glm", Provider: "zhipu"},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})
	a.modelSupportsImagesFn = func(ctx context.Context, providerID, modelID string) (bool, bool, error) {
		return false, true, nil
	}

	_, err = a.Run(context.Background(), SessionAgentCall{SessionID: sess.ID, Prompt: "continue"})
	if err != nil {
		t.Fatalf("Run() unexpected error = %v", err)
	}

	for _, msg := range runtime.messages {
		for _, part := range msg.Content {
			if _, ok := part.(llm.FilePart); ok {
				t.Fatalf("expected no historical file/media parts for non-image model")
			}
			if tr, ok := part.(llm.ToolResultPart); ok && tr.Output.Type == llm.ToolResultOutputMedia {
				t.Fatalf("expected historical tool media outputs to be downgraded to text")
			}
		}
	}
}

func TestAdaptToolsForImageCapability_StripsMediaToolResponses(t *testing.T) {
	t.Parallel()

	tool := llm.NewAgentTool("mock", "test", func(ctx context.Context, input map[string]any, call llm.ToolCall) (llm.ToolResponse, error) {
		return llm.NewMediaResponse([]byte("ZmFrZQ=="), "image/png"), nil
	})
	adapted := adaptToolsForImageCapability([]llm.Tool{tool}, false, true)
	if len(adapted) != 1 {
		t.Fatalf("adapted tools count = %d, want 1", len(adapted))
	}
	resp, err := adapted[0].Run(context.Background(), llm.ToolCall{Input: `{}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Type != llm.ToolResponseTypeText {
		t.Fatalf("response type = %q, want text", resp.Type)
	}
	if resp.Content == "" {
		t.Fatalf("expected explanatory text when media is stripped")
	}
}

func TestRun_RequestStopStopsAtStepBoundary(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	runtime := &stopAwareRuntime{afterStep1: make(chan struct{})}
	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime: runtime,
			ModelCfg: bridgecfg.SelectedModel{
				Model:         "test-model",
				Provider:      "test-provider",
				ContextWindow: 8192,
			},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})

	errCh := make(chan error, 1)
	go func() {
		_, runErr := a.Run(context.Background(), SessionAgentCall{
			SessionID: sess.ID,
			Prompt:    "hello",
		})
		errCh <- runErr
	}()

	<-runtime.afterStep1
	a.RequestStop(sess.ID)

	if runErr := <-errCh; runErr != nil {
		t.Fatalf("Run() error = %v, want nil", runErr)
	}

	msgs, err := msgService.List(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("message count = %d, want 2 (user + first step assistant)", len(msgs))
	}
	if got := msgs[1].Content().Text; got != "first step" {
		t.Fatalf("assistant text = %q, want %q", got, "first step")
	}
}

func TestRun_UnknownContextWindowDoesNotForceStopAfterFirstStep(t *testing.T) {
	t.Parallel()

	msgService := message.NewInMemoryService()
	sessService := session.NewInMemoryService()
	sess, err := sessService.Create(context.Background())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	runtime := &stopAwareRuntime{}
	a := NewSessionAgent(SessionAgentOptions{
		Model: bridgecfg.RuntimeModel{
			Runtime: runtime,
			ModelCfg: bridgecfg.SelectedModel{
				Model:         "test-model",
				Provider:      "test-provider",
				ContextWindow: 0,
			},
		},
		SystemPrompt: "system",
		Messages:     msgService,
		Sessions:     sessService,
	})

	if _, err := a.Run(context.Background(), SessionAgentCall{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatalf("Run() unexpected error = %v", err)
	}

	msgs, err := msgService.List(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("message count = %d, want 3 (user + two assistant steps)", len(msgs))
	}
	if got := msgs[2].Content().Text; !strings.Contains(got, "second step") {
		t.Fatalf("assistant text = %q, want second step output", got)
	}
}
