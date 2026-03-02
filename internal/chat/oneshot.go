package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"

	"github.com/francescoalemanno/raijin-mono/internal/agent"
	chatsession "github.com/francescoalemanno/raijin-mono/internal/chat/session"
	"github.com/francescoalemanno/raijin-mono/internal/message"
)

// RunOneShot executes a single prompt turn in non-interactive CLI mode and
// returns only the final assistant text response.
func RunOneShot(runtimeModel libagent.RuntimeModel, modelCfg libagent.ModelConfig, rawPrompt string) (string, error) {
	resolved, err := resolvePromptSubmission(context.Background(), rawPrompt, promptModeOneShot)
	if err != nil {
		return "", err
	}
	if resolved.builtin != nil {
		return "", fmt.Errorf("interactive slash command /%s is not supported in -p mode", resolved.builtin.name)
	}

	sess, err := chatsession.New(runtimeModel)
	if err != nil && sess == nil {
		return "", err
	}
	if sess == nil || sess.Agent() == nil || sess.ID() == "" {
		return "", errors.New("no model configured")
	}

	prepared, err := preparePromptInput(resolved.promptText, sess.Paths())
	if err != nil {
		return "", err
	}

	maxTokens := modelCfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = libagent.DefaultMaxTokens
	}

	err = sess.Agent().Run(context.Background(), agent.SessionAgentCall{
		SessionID:       sess.ID(),
		Prompt:          prepared.text,
		Attachments:     prepared.attachments,
		MaxOutputTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}

	msgs, err := sess.ListMessages(context.Background())
	if err != nil {
		return "", err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != message.Assistant {
			continue
		}
		text := msgs[i].Content().Text
		if strings.TrimSpace(text) == "" {
			continue
		}
		return text, nil
	}
	return "", errors.New("no final assistant response produced")
}
