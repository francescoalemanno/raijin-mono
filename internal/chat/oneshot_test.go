package chat

import (
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestRunOneShotRejectsBuiltinCommand(t *testing.T) {
	t.Parallel()

	_, err := RunOneShot(libagent.RuntimeModel{}, libagent.ModelConfig{}, "/help")
	if err == nil {
		t.Fatal("expected one-shot to reject /help")
	}
}

func TestRunOneShotRequiresConfiguredModelForNormalPrompt(t *testing.T) {
	t.Parallel()

	_, err := RunOneShot(libagent.RuntimeModel{}, libagent.ModelConfig{}, "hello world")
	if err == nil {
		t.Fatal("expected one-shot to fail without configured model")
	}
}
