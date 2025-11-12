package version

import "github.com/fatih/color"

// Version information for the surge CLI.
// These variables can be overridden at build time via -ldflags.

var (
	versionMajorColor = color.New(color.FgYellow, color.Bold)
	versionMinorColor = color.New(color.FgGreen, color.Bold)
	versionPatchColor = color.New(color.FgBlue, color.Bold)

	// Version is the semantic version of the CLI.
	Version = versionMajorColor.Sprint("0") + "." + versionMinorColor.Sprint("1") + "." + versionPatchColor.Sprint("0") + "-dev"

	// GitCommit is an optional git commit hash.
	GitCommit = ""

	// GitMessage is an optional git commit message.
	GitMessage = ""

	// BuildDate is an optional build date in ISO-8601.
	BuildDate = ""
)
