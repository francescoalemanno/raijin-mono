package core

import (
	"reflect"
	"testing"
)

func TestDedupePreservesFirstSeenOrder(t *testing.T) {
	t.Parallel()

	got := Dedupe([]string{" READ ", "grep", "read", "bash", "GREP"})
	want := []string{"read", "grep", "bash"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dedupe = %#v, want %#v", got, want)
	}
}

func TestDedupeSorted(t *testing.T) {
	t.Parallel()

	got := DedupeSorted([]string{"bash", " READ ", "grep", "read"})
	want := []string{"bash", "grep", "read"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DedupeSorted = %#v, want %#v", got, want)
	}
}
