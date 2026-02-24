package version

import _ "embed"

// Version is embedded from the VERSION file at build time.
//
//go:embed VERSION
var Version string
