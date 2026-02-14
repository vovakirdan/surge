package driver

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/diagfmt"
	"surge/internal/source"
)

func TestDiagnoseDirWithOptions_RelativeDirDoesNotDoubleJoin(t *testing.T) {
	tmp := t.TempDir()
	projDir := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(filepath.Join(projDir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Intentionally missing semicolon to force at least one diagnostic.
	badProgram := "@entrypoint\nfn main() {\n    print(\"hi\")\n}\n"

	mainPath := filepath.Join(projDir, "main.sg")
	if err := os.WriteFile(mainPath, []byte(badProgram), 0o600); err != nil {
		t.Fatalf("write main.sg: %v", err)
	}
	nestedPath := filepath.Join(projDir, "nested", "nested.sg")
	if err := os.WriteFile(nestedPath, []byte(badProgram), 0o600); err != nil {
		t.Fatalf("write nested.sg: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	relProj, err := filepath.Rel(cwd, projDir)
	if err != nil {
		t.Fatalf("rel path: %v", err)
	}
	if filepath.IsAbs(relProj) {
		t.Fatalf("expected relative dir path, got %q", relProj)
	}

	opts := DiagnoseOptions{
		Stage:          DiagnoseStageAll,
		MaxDiagnostics: 8,
	}
	fs, results, err := DiagnoseDirWithOptions(context.Background(), relProj, &opts, 1)
	if err != nil {
		t.Fatalf("DiagnoseDirWithOptions error: %v", err)
	}
	if fs == nil {
		t.Fatalf("expected fileset")
	}
	if len(results) < 2 {
		t.Fatalf("expected >= 2 results, got %d", len(results))
	}

	found := map[string]DiagnoseDirResult{}
	for _, res := range results {
		found[res.Path] = res
	}
	for _, want := range []string{mainPath, nestedPath} {
		res, ok := found[want]
		if !ok {
			t.Fatalf("missing result for %s; got paths: %#v", want, keys(found))
		}
		if res.Bag == nil || res.Bag.Len() == 0 {
			t.Fatalf("expected diagnostics for %s", want)
		}
		if !fs.HasFile(res.FileID) {
			t.Fatalf("fileset missing file ID %d for %s", res.FileID, want)
		}
		f := fs.Get(res.FileID)
		if f.Flags&source.FileVirtual != 0 {
			t.Fatalf("expected %s to be loaded from disk, got virtual file", want)
		}

		// Directory diagnosis feeds into pretty formatting in the CLI; ensure it never panics.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("pretty formatting panicked for %s: %v", want, r)
				}
			}()
			var buf bytes.Buffer
			diagfmt.Pretty(&buf, res.Bag, fs, diagfmt.PrettyOpts{Context: 1, PathMode: diagfmt.PathModeRelative})
		}()
	}
}

func keys[M ~map[string]V, V any](m M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
