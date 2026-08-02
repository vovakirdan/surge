package ownershipgate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ownershipRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("ownership gate: get working directory: %v", err)
	}
	for {
		goMod, readErr := os.ReadFile(filepath.Join(dir, "go.mod"))
		makeInfo, statErr := os.Stat(filepath.Join(dir, "Makefile"))
		if readErr == nil && statErr == nil && !makeInfo.IsDir() && hasModuleLine(string(goMod), "surge") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("ownership gate: no repository root with module surge and Makefile above working directory")
		}
		dir = parent
	}
}

func hasModuleLine(goMod, module string) bool {
	for _, line := range strings.Split(goMod, "\n") {
		if strings.TrimSpace(line) == "module "+module {
			return true
		}
	}
	return false
}

func TestOwnershipRepoRootUsesFilesystemMarkers(t *testing.T) {
	root := ownershipRepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "ownershipgate")); err != nil {
		t.Fatalf("resolved ownership repository root %s: %v", root, err)
	}
}
