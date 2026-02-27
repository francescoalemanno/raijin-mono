package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libagent"
)

const toolTemporarilyDisabledMsg = "`%s` is temporarily disabled for this user request only; it will be automatically re-enabled immediately after this request is completed."

type allowedToolsContextKey struct{}

// WithAllowedTools stores a per-execution allowlist in context.
// An empty list means all tools are allowed.
func WithAllowedTools(ctx context.Context, allowed []string) context.Context {
	if len(allowed) == 0 {
		return ctx
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, toolName := range allowed {
		normalized := strings.ToLower(strings.TrimSpace(toolName))
		if normalized == "" {
			continue
		}
		allow[normalized] = struct{}{}
	}
	if len(allow) == 0 {
		return ctx
	}
	return context.WithValue(ctx, allowedToolsContextKey{}, allow)
}

func toolExecutionGate(ctx context.Context, toolName string) (libagent.ToolResponse, bool) {
	if isToolAllowed(ctx, toolName) {
		return libagent.ToolResponse{}, false
	}
	return libagent.NewTextErrorResponse(fmt.Sprintf(toolTemporarilyDisabledMsg, toolName)), true
}

func isToolAllowed(ctx context.Context, toolName string) bool {
	allow, _ := ctx.Value(allowedToolsContextKey{}).(map[string]struct{})
	if len(allow) == 0 {
		return true
	}
	_, ok := allow[strings.ToLower(strings.TrimSpace(toolName))]
	return ok
}
