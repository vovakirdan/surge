package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"surge/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the surge tool version",
	Run: func(cmd *cobra.Command, args []string) {
		// Simple human-readable version output
		v := version.Version
		if v == "" {
			v = "dev"
		}
		extra := ""
		if version.GitCommit != "" {
			extra += fmt.Sprintf(" (%s)", version.GitCommit)
		}
		if version.BuildDate != "" {
			extra += fmt.Sprintf(" built %s", version.BuildDate)
		}
		fmt.Fprintf(os.Stdout, "surge %s%s\n", v, extra)
	},
}
