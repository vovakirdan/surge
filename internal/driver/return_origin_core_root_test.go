package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// Core is diagnosed AS core by giving the driver the real core file
// under the stdlib root (the path scripts/golden_update.sh uses for core_stdlib/).
// The driver admits a module named `core` only for a file inside the stdlib root
// (validateCoreModule), so a user module cannot claim core's identity; the golden
// copy stays an ordinary user module (TestAnalyzeFoldedBuiltinDeclaration/core_stdlib_mirror).

// coreRootDiagnose diagnoses one file with the repository as the stdlib root and
// baseDir as the directory source keys are relative to (the CLI's working directory;
// the harness runs from the repository root).
func coreRootDiagnose(t *testing.T, path, baseDir string) (*DiagnoseResult, error) {
	t.Helper()
	t.Setenv("SURGE_STDLIB", repoRootFromDriverTest(t))
	return DiagnoseWithOptions(context.Background(), path,
		&DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: baseDir, MaxDiagnostics: 64})
}

func coreRootHasErrors(result *DiagnoseResult) bool {
	if result == nil || result.Bag == nil {
		return true
	}
	for _, d := range result.Bag.Items() {
		if d.Severity >= diag.SevError {
			return true
		}
	}
	return false
}

// The harness path: every core file, diagnosed as the root program with core's
// identity. Nine finish clean. string.sg, as the entry, also finalizes its generic
// array concatenations (`prev + one`), whose finalized generic use sits on a binary
// operator and keeps its row: pinned here.
func TestCoreRootDiagnosesCoreAsCore(t *testing.T) {
	repo := repoRootFromDriverTest(t)
	files, err := filepath.Glob(filepath.Join(repo, "core", "*.sg"))
	if err != nil || len(files) != 10 {
		t.Fatalf("PRECONDITION: core has %d files (%v)", len(files), err)
	}
	for _, path := range files {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			result, err := coreRootDiagnose(t, path, repo)
			if name != "string.sg" {
				if err != nil || coreRootHasErrors(result) {
					t.Fatalf("core as core is not clean: err=%v", err)
				}
				return
			}
			var unfinished *returnOriginUnfinishedError
			if !errors.As(err, &unfinished) {
				t.Fatalf("string.sg as core: want the pinned unfinished rows, got err=%v", err)
			}
			text, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := map[string]bool{"prev + one": false, "curr + first": false, "curr + one": false}
			for _, row := range unfinished.Pending {
				snippet := string(text[row.Span.Start:row.Span.End])
				if _, known := want[snippet]; !known || row.SourceKey != "core/string.sg" ||
					row.Reason != "generic use disagrees with its original typed operation" {
					t.Errorf("unexpected row %q at %s %q", row.Reason, row.SourceKey, snippet)
					continue
				}
				want[snippet] = true
			}
			for snippet, seen := range want {
				if !seen {
					t.Errorf("missing the pinned row at %q", snippet)
				}
			}
		})
	}
}

// The relative spelling of the same file is not inside the stdlib root and is
// refused as a reserved namespace, as before this packet.
func TestCoreRootRelativePathStaysReserved(t *testing.T) {
	repo := repoRootFromDriverTest(t)
	t.Chdir(repo)
	_, err := coreRootDiagnose(t, filepath.Join("core", "base.sg"), repo)
	if err == nil || !strings.Contains(err.Error(), "core namespace reserved") {
		t.Fatalf("relative core path: want core namespace reserved, got %v", err)
	}
}

// coreRootCopy copies core's ten files into parent/name and returns the parent
// and one of the copies. Diagnosed with the parent as base dir, a copy named `core`
// has exactly core's source keys (`core/intrinsics.sg`, ...).
func coreRootCopy(t *testing.T, name string) (string, string) {
	t.Helper()
	repo := repoRootFromDriverTest(t)
	parent := t.TempDir()
	dst := filepath.Join(parent, name)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(repo, "core", "*.sg"))
	if err != nil || len(files) != 10 {
		t.Fatalf("PRECONDITION: core has %d files (%v)", len(files), err)
	}
	for _, path := range files {
		text, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(path)), text, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return parent, filepath.Join(dst, "base.sg")
}

// Canaries: core's exact text outside the stdlib root never gets core's identity.
// Named `core`, it is refused as the reserved namespace; named anything else, it is
// a user `no_std` module whose intrinsics no identity certificate answers.
func TestCoreRootIdentityCannotBeClaimedByACopy(t *testing.T) {
	t.Run("copy_named_core", func(t *testing.T) {
		parent, path := coreRootCopy(t, "core")
		_, err := coreRootDiagnose(t, path, parent)
		if err == nil || !strings.Contains(err.Error(), "core namespace reserved") {
			t.Fatalf("a copy of core named core outside the stdlib root: want core namespace reserved, got %v", err)
		}
	})
	t.Run("copy_as_user_module", func(t *testing.T) {
		parent, path := coreRootCopy(t, "mycore")
		_, err := coreRootDiagnose(t, path, parent)
		var unfinished *returnOriginUnfinishedError
		if !errors.As(err, &unfinished) || len(unfinished.Pending) == 0 {
			t.Fatalf("a copy of core as a user module must stay uncertified (unfinished), got %v", err)
		}
		certified := 0
		for _, row := range unfinished.Pending {
			if strings.HasPrefix(row.SourceKey, "core/") {
				certified++
			}
		}
		if certified != 0 {
			t.Errorf("%d rows were reported against core's own source key", certified)
		}
	})
}

// The 8 core sites are explicit tag constructors whose tag is declared in a SIBLING
// file of the root module. The same shapes in a user multi-file module.
func TestCoreRootSiblingTagShapes(t *testing.T) {
	const decls = `pragma module::shapes;

pub tag Found<T>(T);
pub type Lookup<T> = Found(T) | nothing;
pub tag Done<T>(T);
pub type Outcome<T> = Done(T) | Error;
`
	const uses = `pragma module::shapes;

pub fn index_of(xs: &int[], value: int) -> Lookup<uint> {
    let length: int = xs.__len() to int;
    let mut i: int = 0;
    while i < length {
        if xs[i] == value {
            return Found(i to uint);
        }
        i = i + 1;
    }
    return nothing;
}

pub fn from_text(text: string) -> Outcome<string> {
    return Done(text);
}

pub fn failure(o: Outcome<int>) -> Lookup<Error> {
    return compare o {
        Done(_) => nothing;
        err => Found(err);
    };
}
`
	dir := filepath.Join(t.TempDir(), "shapes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"a.sg": decls, "b.sg": uses} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := coreRootDiagnose(t, filepath.Join(dir, "b.sg"), filepath.Dir(dir))
	if err != nil || coreRootHasErrors(result) {
		msg := ""
		if result != nil && result.Bag != nil {
			msg = diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false)
		}
		t.Fatalf("sibling-file tag shapes are not clean: err=%v\n%s", err, msg)
	}
}
