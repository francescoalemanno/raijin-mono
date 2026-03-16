package oneshot

import (
	"errors"
	"testing"
)

func TestIsCodexAuthFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "status 401", err: errors.New("request failed (status 401)"), want: true},
		{name: "unauthorized", err: errors.New("unauthorized access"), want: true},
		{name: "token_invalidated", err: errors.New("code=token_invalidated"), want: true},
		{name: "other", err: errors.New("timeout"), want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isCodexAuthFailure(tc.err); got != tc.want {
				t.Fatalf("isCodexAuthFailure(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
