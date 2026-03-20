package version

import (
	_ "embed"
	"strings"
)

// Version is embedded from the VERSION file at build time.
//
//go:embed VERSION
var Version string

func init() {
	Version = strings.TrimSpace(Version)
}
