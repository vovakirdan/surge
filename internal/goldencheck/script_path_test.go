package goldencheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenScriptUsesOnlyRelativeFixturePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "[x]", "invalid", "hir", "mir", "mono", "directives", "repo")
	fixture := newScriptFixtureAt(t, root, "valid.sg")
	protected := []string{
		"spec_audit/protected.sg",
		"crossing/crosses_deferred/protected.sg",
	}
	for _, relative := range protected {
		writeTestFile(t, filepath.Join(root, "testdata", "golden", filepath.FromSlash(relative)), "fn protected() {}\n", 0o644)
		sidecar := strings.TrimSuffix(relative, ".sg") + ".tokens"
		writeTestFile(t, filepath.Join(root, "testdata", "golden", filepath.FromSlash(sidecar)), "hand-authored\n", 0o644)
	}
	cmd := fixture.command(t, "", nil, nil, nil)
	cmd.Env = append(cmd.Env, "REJECT_COLLECT_DIRECTIVES=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("path-independent generation failed: %v\n%s", err, output)
	}
	for _, extension := range []string{".hir", ".mir", ".mono"} {
		filename := filepath.Join(root, "testdata", "golden", "valid"+extension)
		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			t.Fatalf("checkout path enabled %s generation: %v", extension, err)
		}
	}
	for _, relative := range protected {
		base := filepath.Join(root, "testdata", "golden", filepath.FromSlash(strings.TrimSuffix(relative, ".sg")))
		filename := base + ".diag"
		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			t.Fatalf("protected fixture was generated: %s: %v", relative, err)
		}
		if content, err := os.ReadFile(base + ".tokens"); err != nil || string(content) != "hand-authored\n" {
			t.Fatalf("protected sidecar changed: %s: %q, %v", relative, content, err)
		}
	}
	assertNoGoldenStaging(t, root)
}
