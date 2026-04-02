package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCommandTargetExplicitPathWinsOverManifest(t *testing.T) {
	root := t.TempDir()
	writeCLIFile(t, filepath.Join(root, "surge.toml"), `[package]
name = "demo"
version = "0.1.0"

[run]
main = "main.sg"
`)
	writeCLIFile(t, filepath.Join(root, "main.sg"), `@entrypoint
fn main() -> int {
    return 0;
}
`)
	writeCLIFile(t, filepath.Join(root, "examples", "ini_smoke.sg"), `@entrypoint
fn main() -> int {
    return 0;
}
`)
	chdirForTest(t, root)

	got, err := resolveCommandTarget([]string{"examples/ini_smoke.sg"})
	if err != nil {
		t.Fatalf("resolveCommandTarget explicit file: %v", err)
	}
	if got.usesManifest {
		t.Fatalf("explicit file unexpectedly used manifest target")
	}
	if got.targetPath != filepath.Join("examples", "ini_smoke.sg") {
		t.Fatalf("targetPath = %q, want %q", got.targetPath, filepath.Join("examples", "ini_smoke.sg"))
	}
	if got.outputName != "ini_smoke" {
		t.Fatalf("outputName = %q, want ini_smoke", got.outputName)
	}
}

func TestResolveCommandTargetExplicitPathBypassesInvalidManifest(t *testing.T) {
	root := t.TempDir()
	writeCLIFile(t, filepath.Join(root, "surge.toml"), `[package]
name = "broken"
version = "0.1.0"
`)
	writeCLIFile(t, filepath.Join(root, "examples", "ini_smoke.sg"), `@entrypoint
fn main() -> int {
    return 0;
}
`)
	chdirForTest(t, root)

	got, err := resolveCommandTarget([]string{"examples/ini_smoke.sg"})
	if err != nil {
		t.Fatalf("resolveCommandTarget should ignore invalid manifest for explicit file: %v", err)
	}
	if got.usesManifest {
		t.Fatalf("explicit file unexpectedly used invalid manifest")
	}
}

func TestResolveCommandTargetDotUsesManifest(t *testing.T) {
	root := t.TempDir()
	writeCLIFile(t, filepath.Join(root, "surge.toml"), `[package]
name = "demo"
version = "0.1.0"

[run]
main = "main.sg"
`)
	writeCLIFile(t, filepath.Join(root, "main.sg"), `@entrypoint
fn main() -> int {
    return 0;
}
`)
	chdirForTest(t, root)

	got, err := resolveCommandTarget([]string{"."})
	if err != nil {
		t.Fatalf("resolveCommandTarget dot: %v", err)
	}
	if !got.usesManifest {
		t.Fatalf("dot target should use manifest")
	}
	if got.targetPath != filepath.Join(root, "main.sg") {
		t.Fatalf("targetPath = %q, want %q", got.targetPath, filepath.Join(root, "main.sg"))
	}
	if got.baseDir != root {
		t.Fatalf("baseDir = %q, want %q", got.baseDir, root)
	}
	if got.outputName != "demo" {
		t.Fatalf("outputName = %q, want demo", got.outputName)
	}
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
}

func writeCLIFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
