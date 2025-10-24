package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build [flags] [path]",
	Short: "Build a surge project (stub)",
	Long:  "Build a surge project. This is a placeholder command; functionality is not implemented yet.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		release, _ := cmd.Flags().GetBool("release")
		dev, _ := cmd.Flags().GetBool("dev")

		if release && dev {
			return fmt.Errorf("--release and --dev are mutually exclusive")
		}

		target := "."
		if len(args) == 1 {
			target = args[0]
		}

		// For now, just acknowledge the invocation.
		mode := "default"
		if release {
			mode = "release"
		} else if dev {
			mode = "dev"
		}
		fmt.Fprintf(os.Stdout, "build (stub): target=%s mode=%s\n", target, mode)
		return nil
	},
}

// init registers the command-line flags for buildCmd.
// It adds the --release flag ("optimize for release") and the --dev flag ("development build with extra checks").
func init() {
	buildCmd.Flags().Bool("release", false, "optimize for release")
	buildCmd.Flags().Bool("dev", false, "development build with extra checks")
}
