package agent

import (
	"testing"

	"github.com/francescoalemanno/raijin-mono/internal/message"
)

func TestPersistAssistant_NoIDOnBufferedMessage(t *testing.T) {
	rs := &runState{}
	rs.currentAssistant = &message.Message{Role: message.Assistant}
	if rs.currentAssistant.ID != "" {
		t.Fatalf("buffered assistant should not have an ID before persistence")
	}
}
