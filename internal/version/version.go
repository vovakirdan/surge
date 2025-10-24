package version

// Version information for the surge CLI.
// These variables can be overridden at build time via -ldflags.

var (
	// Version is the semantic version of the CLI.
	Version = "0.1.0-dev"

	// GitCommit is an optional git commit hash.
	GitCommit = ""

	// BuildDate is an optional build date in ISO-8601.
	BuildDate = ""
)
