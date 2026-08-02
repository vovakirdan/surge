//go:build runtime_v2_ownership_corpus

package ownershipgate_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const censusReportFilename = "ownership-corpus-census.json"

func censusReportPath(root string) string {
	return filepath.Join(root, "target", "runtime-v2", censusReportFilename)
}

// invalidateCensusReport makes an interrupted or failed run distinguishable
// from the last successful run. The full census calls it before discovery,
// ledger loading, or compilation can fail.
func invalidateCensusReport(root string) error {
	path := censusReportPath(root)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale ownership census %s: %w", path, err)
	}
	return nil
}

func writeCensusReport(root string, report corpusCensusReport) (string, error) {
	dir := filepath.Join(root, "target", "runtime-v2")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create ownership census directory %s: %w", dir, err)
	}
	data, err := encodeCensusReport(report)
	if err != nil {
		return "", err
	}

	path := censusReportPath(root)
	temporary, err := os.CreateTemp(dir, ".ownership-corpus-census-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary ownership census in %s: %w", dir, err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return "", fmt.Errorf("set temporary ownership census mode: %w", err)
	}
	if _, err := io.Copy(temporary, bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("write temporary ownership census: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync temporary ownership census: %w", err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return "", fmt.Errorf("close temporary ownership census: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", fmt.Errorf("publish ownership census %s: %w", path, err)
	}
	return path, nil
}
