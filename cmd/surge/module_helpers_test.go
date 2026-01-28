package main

import (
	"os"
	"path/filepath"
	"testing"

	"surge/internal/project"
)

func TestDeriveModuleNameFromURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"https://github.com/vovakirdan/sigil.git", "sigil"},
		{"https://github.com/vovakirdan/sigil", "sigil"},
		{"git@github.com:vovakirdan/sigil.git", "sigil"},
		{"https://example.com/org/repo/", "repo"},
	}
	for _, tc := range cases {
		got, err := deriveModuleNameFromURL(tc.input)
		if err != nil {
			t.Fatalf("deriveModuleNameFromURL(%q) error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("deriveModuleNameFromURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLoadProjectModules(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "surge.toml")
	data := `# test manifest
[package]
name = "demo"
version = "0.1.0"

[run]
main = "main.sg"

[modules]
sigil = { source = "git", url = "https://github.com/vovakirdan/sigil.git" }
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write surge.toml: %v", err)
	}
	mods, err := project.LoadProjectModules(path)
	if err != nil {
		t.Fatalf("LoadProjectModules: %v", err)
	}
	spec, ok := mods["sigil"]
	if !ok {
		t.Fatalf("expected sigil module")
	}
	if spec.Source != "git" {
		t.Fatalf("spec.Source = %q, want git", spec.Source)
	}
	if spec.URL != "https://github.com/vovakirdan/sigil.git" {
		t.Fatalf("spec.URL = %q, want url", spec.URL)
	}
}
