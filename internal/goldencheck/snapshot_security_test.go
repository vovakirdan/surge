package goldencheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanRejectsSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	root := filepath.Join(t.TempDir(), "golden")
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked root error = %v", err)
	}
}

func TestScanRejectsEveryNestedSymlink(t *testing.T) {
	tests := []struct {
		name string
		path string
		dir  bool
	}{
		{name: "source", path: "case.sg"},
		{name: "sidecar", path: "case.tokens"},
		{name: "directory", path: "nested", dir: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(t.TempDir(), "target")
			if test.dir {
				if err := os.Mkdir(target, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				writeTestFile(t, target, "outside", 0o600)
			}
			link := filepath.Join(root, test.path)
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if _, err := Scan(root); err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("nested symlink error = %v", err)
			}
		})
	}
}

func TestScanRecordsRootAndSpecialModeBits(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, "entry")
	writeTestFile(t, filename, "content", 0o755)
	baseline, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Entries) != 2 || baseline.Entries[0].Path != "." || baseline.Entries[0].Kind != "directory" {
		t.Fatalf("root entry = %#v", baseline.Entries)
	}
	if chmodErr := os.Chmod(filename, 0o755|os.ModeSetuid); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	setuid, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(baseline, setuid)
	if len(changes) != 1 || changes[0].Path != "entry" || changes[0].After.Mode&uint32(os.ModeSetuid) == 0 {
		t.Fatalf("setuid changes = %#v", changes)
	}
	if chmodErr := os.Chmod(root, 0o755); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	rootMode, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	changes = Diff(setuid, rootMode)
	if len(changes) != 1 || changes[0].Path != "." {
		t.Fatalf("root mode changes = %#v", changes)
	}
}
