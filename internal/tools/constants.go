package tools

import "time"

const (
	// Scanner buffer sizes
	defaultScannerBufferSize = 64 * 1024
	maxScannerBufferSize     = 1024 * 1024

	// File operation constants
	defaultDirPerm  = 0o755
	defaultFilePerm = 0o644

	// HTTP status codes
	httpStatusSuccessMin = 200
	httpStatusSuccessMax = 300

	// Web fetch limits
	defaultWebFetchTimeout = 30 * time.Second
	maxWebFetchBodySize    = 5 * 1024 * 1024 // 5MB

)
