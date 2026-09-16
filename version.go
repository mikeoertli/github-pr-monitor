package gprm

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var version string

// Version returns the version embedded from the repository's VERSION file.
func Version() string { return strings.TrimSpace(version) }
