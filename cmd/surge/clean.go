package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean [path]",
	Short: "Remove Surge build cache (target directory)",
	Long:  "Remove the target directory used for Surge build artifacts and caches.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runClean,
}

func runClean(_ *cobra.Command, args []string) error {
	baseDir := "."
	if len(args) > 0 && args[0] != "" {
		baseDir = args[0]
	}
	baseDir, err := resolveCleanBase(baseDir)
	if err != nil {
		return err
	}
	targetDir := filepath.Join(baseDir, "target")
	info, err := os.Stat(targetDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(os.Stdout, "target directory not found\n")
			return nil
		}
		return fmt.Errorf("failed to stat %q: %w", targetDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", targetDir)
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("failed to remove %q: %w", targetDir, err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "removed %s\n", formatPathForOutput(baseDir, targetDir))
	return nil
}

func resolveCleanBase(base string) (string, error) {
	info, err := os.Stat(base)
	if err != nil {
		return "", fmt.Errorf("failed to stat %q: %w", base, err)
	}
	if !info.IsDir() {
		base = filepath.Dir(base)
	}
	manifest, ok, err := loadProjectManifest(base)
	if err != nil {
		return "", err
	}
	if ok {
		return manifest.Root, nil
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return base, nil
	}
	return abs, nil
}
