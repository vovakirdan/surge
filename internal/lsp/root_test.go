package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProjectRootWithManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "surge.toml"), []byte(""), 0644); err != nil {
		t.Fatalf("write surge.toml: %v", err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(nested, "main.sg")
	if err := os.WriteFile(filePath, []byte("@entrypoint\nfn main() { return; }\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got := detectProjectRoot(root, filePath)
	if got != root {
		t.Fatalf("expected root %q, got %q", root, got)
	}
}

func TestDetectProjectRootFallbackToFileDir(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "main.sg")
	if err := os.WriteFile(filePath, []byte("@entrypoint\nfn main() { return; }\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got := detectProjectRoot("", filePath)
	if got != root {
		t.Fatalf("expected fallback %q, got %q", root, got)
	}
}
