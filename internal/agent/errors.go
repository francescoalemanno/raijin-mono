package agent

import "errors"

var (
	ErrNoModelConfigured        = errors.New("no model configured")
	ErrMessageServiceMissing    = errors.New("message service is required")
	ErrProviderNotConfigured    = errors.New("provider not configured")
	ErrEmptyPrompt              = errors.New("empty prompt")
	ErrSessionStoreMissing      = errors.New("session store is required")
	ErrSessionMissing           = errors.New("session ID is required")
	ErrModelNoImageSupport      = errors.New("selected model does not support image inputs")
	ErrImageSupportLookupFailed = errors.New("failed to determine model image capability")
)
