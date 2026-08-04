//go:build linux

package goldencheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestScanRejectsUnsafeFilesystemNode(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "unsafe.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(root); err == nil || !strings.Contains(err.Error(), "unsafe.fifo") {
		t.Fatalf("FIFO error = %v", err)
	}
}

func TestGoldenScriptRejectsUnsafeFilesystemNode(t *testing.T) {
	fixture := newScriptFixture(t, "valid.sg")
	fifo := filepath.Join(fixture.root, "testdata", "golden", "unsafe.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := fixture.run(t, "", nil, nil)
	if err == nil || !strings.Contains(output, "unsafe filesystem entry") {
		t.Fatalf("script accepted FIFO: %v\n%s", err, output)
	}
	info, statErr := os.Lstat(fifo)
	if statErr != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO changed: %v, %v", info, statErr)
	}
	assertNoGoldenStaging(t, fixture.root)
}
