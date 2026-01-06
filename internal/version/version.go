package version

import (
	"strings"

	"github.com/fatih/color"
)

// Version information for the surge CLI.
// These variables can be overridden at build time via -ldflags.

var (
	// BaseVersion is the base semantic version (without pre-release suffix).
	BaseVersion = "0.1.0"

	// PreRelease is the optional pre-release suffix, e.g. "-dev".
	PreRelease = "-dev"

	// GitCommit is an optional git commit hash.
	GitCommit = ""

	// GitMessage is an optional git commit message.
	GitMessage = ""

	// BuildDate is an optional build date in ISO-8601.
	BuildDate = ""
)

var (
	versionMajorColor = color.New(color.FgYellow, color.Bold)
	versionMinorColor = color.New(color.FgGreen, color.Bold)
	versionPatchColor = color.New(color.FgBlue, color.Bold)
)

// VersionString returns the plain semantic version string.
func VersionString() string {
	return strings.TrimSpace(BaseVersion) + strings.TrimSpace(PreRelease)
}

// String returns the plain semantic version string.
func String() string {
	return VersionString()
}

// PrettyVersion returns the colored version string for CLI output.
func PrettyVersion() string {
	base := strings.TrimSpace(BaseVersion)
	if base == "" {
		base = "dev"
	}
	parts := strings.SplitN(base, ".", 3)
	if len(parts) != 3 {
		return base + strings.TrimSpace(PreRelease)
	}
	return versionMajorColor.Sprint(parts[0]) + "." + versionMinorColor.Sprint(parts[1]) + "." + versionPatchColor.Sprint(parts[2]) + strings.TrimSpace(PreRelease)
}
