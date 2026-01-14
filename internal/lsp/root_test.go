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

	gotRoot, gotMode := detectAnalysisScope(root, filePath)
	if gotRoot != root {
		t.Fatalf("expected root %q, got %q", root, gotRoot)
	}
	if gotMode != modeProjectRoot {
		t.Fatalf("expected project root mode, got %v", gotMode)
	}
}

func TestDetectProjectRootFallbackToFileDir(t *testing.T) {
	base := t.TempDir()
	rootA := filepath.Join(base, "rootA")
	rootB := filepath.Join(base, "rootB")
	if err := os.MkdirAll(rootA, 0755); err != nil {
		t.Fatalf("mkdir rootA: %v", err)
	}
	if err := os.MkdirAll(rootB, 0755); err != nil {
		t.Fatalf("mkdir rootB: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootA, "surge.toml"), []byte(""), 0644); err != nil {
		t.Fatalf("write surge.toml: %v", err)
	}
	filePath := filepath.Join(rootA, "main.sg")
	if err := os.WriteFile(filePath, []byte("@entrypoint\nfn main() { return; }\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	gotRoot, gotMode := detectAnalysisScope(base, filePath)
	if gotRoot != rootA {
		t.Fatalf("expected root %q, got %q", rootA, gotRoot)
	}
	if gotMode != modeProjectRoot {
		t.Fatalf("expected project root mode, got %v", gotMode)
	}

	looseFile := filepath.Join(rootB, "loose.sg")
	if err := os.WriteFile(looseFile, []byte("@entrypoint\nfn main() { return; }\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	looseRoot, looseMode := detectAnalysisScope("", looseFile)
	if looseRoot != rootB {
		t.Fatalf("expected open-files root %q, got %q", rootB, looseRoot)
	}
	if looseMode != modeOpenFiles {
		t.Fatalf("expected open-files mode, got %v", looseMode)
	}
}
