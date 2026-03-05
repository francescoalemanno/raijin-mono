package agent

import (
	"testing"

	libagent "github.com/francescoalemanno/raijin-mono/libagent"
)

func TestPersistAssistant_NoIDOnBufferedMessage(t *testing.T) {
	rs := &runState{}
	rs.currentAssistant = &libagent.AssistantMessage{Role: "assistant"}
	if rs.currentAssistant.Meta.ID != "" {
		t.Fatalf("buffered assistant should not have an ID before persistence")
	}
}
