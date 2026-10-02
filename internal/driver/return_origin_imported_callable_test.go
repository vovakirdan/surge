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

const importedCallableRefusal = "selected callable lacks its published callable authority"

const importedCallableFirst = `pragma module;

pub fn first(a: &string, b: &string) -> &string {
    return a;
}
`

const importedCallableTotal = `pragma module;

pub fn total(xs: &int[]) -> int {
    let mut s: int = 0;
    let mut i: int = 0;
    let n: int = xs.__len() to int;
    while i < n {
        s = s + xs[i];
        i = i + 1;
    }
    return s;
}

fn put(xs: &mut int[]) -> nothing {
    xs[0] = 7;
}

fn part(xs: &int[], r: Range<int>) -> int {
    let v = xs[r];
    return v[0];
}
`

const importedCallableElements = `pragma module;

pub type Buf = {
    data: byte[],
};

extern<Buf> {
    pub fn add(self: &mut Buf, b: byte) -> nothing {
        self.data.push(b);
    }
}

fn first(xs: &int[]) -> &int {
    return xs[0];
}

fn leak() -> &int {
    let xs: int[] = [1, 2];
    return xs[0];
}
`

// importedCallableOutcome diagnoses one file of a small project and returns
// the borrow escapes it reports (by line) and its unfinished rows.
func importedCallableOutcome(t *testing.T, files map[string]string, target string) (escapes []int, pending []string) {
	t.Helper()
	repo := repoRootFromDriverTest(t)
	t.Setenv("SURGE_STDLIB", repo)
	project := t.TempDir()
	for name, text := range files {
		path := filepath.Join(project, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	res, err := DiagnoseWithOptions(context.Background(), filepath.Join(project, target),
		&DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: project, MaxDiagnostics: 64})
	var unfinished *returnOriginUnfinishedError
	switch {
	case errors.As(err, &unfinished):
		for _, row := range unfinished.Pending {
			pending = append(pending, row.SourceKey+" "+row.Reason)
		}
		return nil, pending
	case err != nil:
		t.Fatalf("PRECONDITION: diagnosis failed: %v", err)
	}
	for _, d := range res.Bag.Items() {
		switch {
		case d.Code == diag.SemaBorrowEscapesReturn:
			start, _ := res.FileSet.Resolve(d.Primary)
			escapes = append(escapes, int(start.Line))
		case d.Severity == diag.SevError:
			t.Fatalf("PRECONDITION: unexpected error %v: %s", d.Code, d.Message)
		}
	}
	return escapes, nil
}

// A second file of a directory module selects its own copies of core. Each
// copy carries the merge key of the one declaration and reaches its body and
// promise: a borrow of a parameter is accepted, an escape of a local is
// reported by name where it happens, and one finalized generic instance serves
// the copies of both files.
func TestReturnOriginImportedCallableAuthority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		files   map[string]string
		target  string
		escapes []int
	}{
		{
			name: "second_file_of_a_module",
			files: map[string]string{
				"mod/a.sg": "pragma module;\n\npub fn head(s: &string) -> BytesView {\n    return s.bytes();\n}\n",
				"mod/b.sg": "pragma module;\n\nfn view(s: &string) -> BytesView {\n    return s.bytes();\n}\n\nfn leak() -> BytesView {\n    let s: string = \"x\";\n    return s.bytes();\n}\n",
			},
			target: "mod/b.sg", escapes: []int{9},
		},
		{
			name: "generic_core_operations_across_module_files",
			files: map[string]string{
				"mod/a.sg": importedCallableTotal,
				"mod/b.sg": importedCallableElements,
			},
			target: "mod/a.sg", escapes: []int{19},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			escapes, pending := importedCallableOutcome(t, tc.files, tc.target)
			if len(pending) != 0 {
				t.Fatalf("the imported selection stayed unfinished: %v", pending)
			}
			if len(escapes) != len(tc.escapes) {
				t.Fatalf("escapes at lines %v, want %v", escapes, tc.escapes)
			}
			for i := range escapes {
				if escapes[i] != tc.escapes[i] {
					t.Fatalf("escapes at lines %v, want %v", escapes, tc.escapes)
				}
			}
		})
	}
}

// A user type's own `__index` is its declaration, not the core container
// operation: an index through it stays refused whether or not the element
// reference outlives its owner.
func TestReturnOriginImportedMagicMethodStaysRefused(t *testing.T) {
	bag := "pragma module;\n\npub type Bag = {\n    items: int[],\n};\n\nextern<Bag> {\n    pub fn __index(self: &Bag, i: int) -> &int {\n        return &self.items[i];\n    }\n}\n"
	for name, body := range map[string]string{
		"forwarded": "fn get(b: &bag.Bag) -> &int {\n    return b[0];\n}\n",
		"leaked":    "fn leak() -> &int {\n    let b: bag.Bag = bag.Bag { items = [1, 2] };\n    return b[0];\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			escapes, pending := importedCallableOutcome(t, map[string]string{
				"app/lib/bag.sg": bag,
				"app/main.sg":    "import ./lib/bag as bag;\n\n" + body,
			}, "app/main.sg")
			if len(pending) == 0 && len(escapes) == 0 {
				t.Fatal("an index through a user `__index` was accepted")
			}
			for _, row := range pending {
				if strings.HasSuffix(row, importedCallableRefusal) {
					t.Fatalf("the user operator lost its declaration authority: %v", pending)
				}
			}
		})
	}
}
