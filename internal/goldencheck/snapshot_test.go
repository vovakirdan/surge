package goldencheck

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestScanPreservesLexicalPathsAndDetectsContent(t *testing.T) {
	root := t.TempDir()
	paths := []string{"line\nbreak", "tab\tname", "雪"}
	for _, path := range paths {
		writeTestFile(t, filepath.Join(root, path), path, 0o644)
	}
	before, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Entries) != len(paths)+1 {
		t.Fatalf("entry count = %d, want %d", len(before.Entries), len(paths)+1)
	}
	if before.Entries[0].Path != "." || before.Entries[1].Path != "line\nbreak" || before.Entries[2].Path != "tab\tname" || before.Entries[3].Path != "雪" {
		t.Fatalf("lexical paths changed: %#v", before.Entries)
	}
	writeTestFile(t, filepath.Join(root, "tab\tname"), "changed", 0o644)
	after, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Path != "tab\tname" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestSnapshotDigestFramesPathAndContent(t *testing.T) {
	hash := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	left := Snapshot{Entries: []Entry{{Path: "a", Kind: "file", Mode: 0o644, ContentSHA256: hash("bc")}}}
	right := Snapshot{Entries: []Entry{{Path: "ab", Kind: "file", Mode: 0o644, ContentSHA256: hash("c")}}}
	if left.Digest() == right.Digest() {
		t.Fatal("length-framed snapshots collided")
	}
}

func TestScanRecordsModeAndType(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "entry")
	writeTestFile(t, filename, "content", 0o644)
	regular, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if chmodErr := os.Chmod(filename, 0o755); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	executable, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(Diff(regular, executable)) != 1 {
		t.Fatal("mode change was not detected")
	}
	if removeErr := os.Remove(filename); removeErr != nil {
		t.Fatal(removeErr)
	}
	if symlinkErr := os.Symlink("target", filename); symlinkErr != nil {
		t.Fatal(symlinkErr)
	}
	if _, err := Scan(root); err == nil {
		t.Fatal("Scan accepted a symlink")
	}
}

func TestParsePorcelainUsesNULTerminatedPaths(t *testing.T) {
	changes, err := parsePorcelain([]byte("?? testdata/golden/line\nbreak\t雪.tokens\x00!! testdata/golden/ignored\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Path != "testdata/golden/line\nbreak\t雪.tokens" {
		t.Fatalf("changes = %#v", changes)
	}
}
