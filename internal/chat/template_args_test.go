package chat

import "testing"

func TestTemplateNeedsArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "plain", content: "hello world", want: false},
		{name: "all args", content: "task: $@", want: true},
		{name: "positional", content: "task: $1", want: true},
		{name: "slice", content: "task: ${@:2}", want: true},
		{name: "escaped dollar", content: "task: \\$@", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := templateNeedsArguments(tt.content); got != tt.want {
				t.Fatalf("templateNeedsArguments(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}
