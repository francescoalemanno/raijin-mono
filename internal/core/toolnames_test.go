package core

import (
	"reflect"
	"testing"
)

func TestDedupePreservesFirstSeenOrder(t *testing.T) {
	t.Parallel()

	got := Dedupe([]string{" READ ", "grep", "read", "webfetch", "GREP"})
	want := []string{"read", "grep", "webfetch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dedupe = %#v, want %#v", got, want)
	}
}

func TestDedupeSorted(t *testing.T) {
	t.Parallel()

	got := DedupeSorted([]string{"webfetch", " READ ", "grep", "read"})
	want := []string{"grep", "read", "webfetch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DedupeSorted = %#v, want %#v", got, want)
	}
}
