package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FindSurgeToml walks up from startDir to locate surge.toml.
func FindSurgeToml(startDir string) (path string, ok bool, err error) {
	if startDir == "" {
		startDir = "."
	}
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve start directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, "surge.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false, fmt.Errorf("failed to stat %q: %w", candidate, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false, nil
}

// FindProjectRoot returns the directory containing surge.toml, if any.
func FindProjectRoot(startDir string) (root string, ok bool, err error) {
	manifestPath, ok, err := FindSurgeToml(startDir)
	if err != nil || !ok {
		return "", ok, err
	}
	return filepath.Dir(manifestPath), true, nil
}
