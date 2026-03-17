package shellinit

import (
	"strings"
	"testing"
)

func TestCompletionsIncludeOneShotStatus(t *testing.T) {
	lines := strings.Split(Completions(), "\n")
	foundStatus := false
	foundReasoning := false
	foundEdit := false
	for _, line := range lines {
		if line == "status" {
			foundStatus = true
		}
		if line == "reasoning" {
			foundReasoning = true
		}
		if line == "edit" {
			foundEdit = true
		}
	}
	if !foundStatus || !foundReasoning || !foundEdit {
		t.Fatalf("expected completions to include status, reasoning, and edit, got %q", lines)
	}
}

func TestCompleteSlashCommand(t *testing.T) {
	out := Complete(":/add")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if line == ":/add-model" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected :/add-model in completions, got %q", lines)
	}
}

func TestCompleteSkillsPrefix(t *testing.T) {
	out := Complete(":+")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if strings.HasPrefix(line, ":+") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one skill completion, got %q", lines)
	}
}

func TestCompleteColonShowsSkillsAndCommands(t *testing.T) {
	out := Complete(":")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	foundSkill := false
	foundCommand := false
	for _, line := range lines {
		if strings.HasPrefix(line, ":+") {
			foundSkill = true
		}
		if strings.HasPrefix(line, ":/") {
			foundCommand = true
		}
	}
	if !foundSkill || !foundCommand {
		t.Fatalf("expected both skill and command completions, got %q", lines)
	}
}

func TestCompleteMidSentenceSkillToken(t *testing.T) {
	out := Complete(":please use +")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	foundSkill := false
	for _, line := range lines {
		if strings.HasPrefix(line, "+") {
			foundSkill = true
			break
		}
	}
	if !foundSkill {
		t.Fatalf("expected skill completion for mid-sentence token, got %q", lines)
	}
}

func TestCompleteMidSentenceSlashToken(t *testing.T) {
	out := Complete(":please run /add")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if line == "/add-model" || line == ":/add-model" {
			found = true
			break
		}
	}
	if found {
		t.Fatalf("expected no /command completion for mid-sentence token, got %q", lines)
	}
}

func TestCompleteMidSentenceSkillWithPrefixStillCompletes(t *testing.T) {
	out := Complete(":please use +tm")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	found := false
	for _, line := range lines {
		if strings.HasPrefix(line, "+tm") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected +skill completion for mid-sentence token, got %q", lines)
	}
}
