package agent

import "errors"

var (
	ErrNoModelConfigured        = errors.New("no model configured")
	ErrProviderNotConfigured    = errors.New("provider not configured")
	ErrEmptyPrompt              = errors.New("empty prompt")
	ErrSessionMissing           = errors.New("session ID is required")
	ErrModelNoImageSupport      = errors.New("selected model does not support image inputs")
	ErrImageSupportLookupFailed = errors.New("failed to determine model image capability")
)
