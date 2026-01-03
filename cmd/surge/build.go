package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"surge/internal/driver"
	"surge/internal/project"
)

var buildCmd = &cobra.Command{
	Use:   "build [flags] [path]",
	Short: "Build a surge project",
	Long:  "Build a surge project using surge.toml as the entrypoint definition.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		release, err := cmd.Flags().GetBool("release")
		if err != nil {
			return err
		}
		dev, err := cmd.Flags().GetBool("dev")
		if err != nil {
			return err
		}

		if release && dev {
			return fmt.Errorf("--release and --dev are mutually exclusive")
		}

		manifest, manifestFound, err := loadProjectManifest(".")
		if err != nil {
			return err
		}
		if !manifestFound {
			return errors.New(noSurgeTomlMessage)
		}

		targetPath, dirInfo, err := resolveProjectRunTarget(manifest)
		if err != nil {
			return err
		}
		maxDiagnostics, err := cmd.Root().PersistentFlags().GetInt("max-diagnostics")
		if err != nil {
			return fmt.Errorf("failed to get max-diagnostics flag: %w", err)
		}

		opts := driver.DiagnoseOptions{
			Stage:          driver.DiagnoseStageSema,
			MaxDiagnostics: maxDiagnostics,
			BaseDir:        manifest.Root,
			RootKind:       project.ModuleKindBinary,
		}
		result, err := driver.DiagnoseWithOptions(cmd.Context(), targetPath, opts)
		if err != nil {
			return fmt.Errorf("compilation failed: %w", err)
		}
		if result.Bag != nil && result.Bag.HasErrors() {
			for _, d := range result.Bag.Items() {
				fmt.Fprintln(os.Stderr, d.Message)
			}
			return fmt.Errorf("diagnostics reported errors")
		}
		if err := validateEntrypoints(result); err != nil {
			return err
		}
		if dirInfo != nil && dirInfo.fileCount > 1 {
			meta := result.RootModuleMeta()
			if meta == nil {
				return fmt.Errorf("failed to resolve module metadata for %q", dirInfo.path)
			}
			if !meta.HasModulePragma {
				return fmt.Errorf("directory %q is not a module; add pragma module/binary to all .sg files or run a file", dirInfo.path)
			}
		}

		outputName := manifest.Config.Package.Name
		outDir, err := os.Getwd()
		if err != nil {
			outDir = "."
		}
		outputPath := filepath.Join(outDir, outputName)
		script := fmt.Sprintf("#!/bin/sh\nset -e\ncd %q\nexec surge run -- \"$@\"\n", manifest.Root)
		if err := os.WriteFile(outputPath, []byte(script), 0o644); err != nil {
			return fmt.Errorf("failed to write build output %q: %w", outputPath, err)
		}
		if err := os.Chmod(outputPath, 0o755); err != nil {
			return fmt.Errorf("failed to mark build output executable: %w", err)
		}

		fmt.Fprintf(os.Stdout, "built %s\n", outputPath)
		return nil
	},
}

// init registers the command-line flags for buildCmd.
// It adds the --release flag ("optimize for release") and the --dev flag ("development build with extra checks").
func init() {
	buildCmd.Flags().Bool("release", false, "optimize for release")
	buildCmd.Flags().Bool("dev", false, "development build with extra checks")
}
