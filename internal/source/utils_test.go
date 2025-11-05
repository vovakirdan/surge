package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRelativePathOutsideBaseFallsBackToAbsolute(t *testing.T) {
	tmp := t.TempDir()

	baseDir := filepath.Join(tmp, "base")
	otherDir := filepath.Join(tmp, "other")

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("failed to create other dir: %v", err)
	}

	target := filepath.Join(otherDir, "file.sg")

	got, err := RelativePath(target, baseDir)
	if err != nil {
		t.Fatalf("RelativePath returned error: %v", err)
	}

	want := normalizePath(target)
	if got != want {
		t.Fatalf("expected absolute fallback %q, got %q", want, got)
	}
}

func TestRelativePathInsideBaseStaysRelative(t *testing.T) {
	tmp := t.TempDir()

	baseDir := filepath.Join(tmp, "base")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}

	target := filepath.Join(baseDir, "nested", "file.sg")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	got, err := RelativePath(target, baseDir)
	if err != nil {
		t.Fatalf("RelativePath returned error: %v", err)
	}

	want := normalizePath(filepath.Join("nested", "file.sg"))
	if got != want {
		t.Fatalf("expected relative path %q, got %q", want, got)
	}
}
