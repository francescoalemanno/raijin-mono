package oneshot

import (
	"strings"
	"testing"
)

func TestReplStatusRefreshPrintsEvenWhenUnchanged(t *testing.T) {
	m := replModel{
		statusLoaded: true,
		status:       "test-model · medium",
		historyIndex: -1,
	}

	updated, cmd := m.Update(replStatusMsg{label: "test-model · medium"})
	if cmd == nil {
		t.Fatalf("status refresh returned nil command")
	}

	next, ok := updated.(replModel)
	if !ok {
		t.Fatalf("updated model type = %T, want replModel", updated)
	}
	if next.status != "test-model · medium" {
		t.Fatalf("status = %q, want unchanged status", next.status)
	}
}

func TestReplEmptySubmitRepeatsPrompt(t *testing.T) {
	m := replModel{
		statusLoaded: true,
		status:       "test-model · medium",
		historyIndex: -1,
	}

	view := m.View().Content
	if got := strings.Count(view, replPrompt); got != 1 {
		t.Fatalf("initial prompt count = %d, want 1", got)
	}
	if strings.Contains(view, "Raijin REPL") {
		t.Fatalf("view unexpectedly contains static banner: %q", view)
	}

	updated, cmd := m.submit()
	if cmd == nil {
		t.Fatalf("submit returned nil command for empty prompt")
	}

	next, ok := updated.(replModel)
	if !ok {
		t.Fatalf("updated model type = %T, want replModel", updated)
	}
	if len(next.history) != 0 {
		t.Fatalf("history len = %d, want 0", len(next.history))
	}
	if got := strings.Count(next.View().Content, replPrompt); got != 1 {
		t.Fatalf("prompt count after empty submit = %d, want 1", got)
	}
}
