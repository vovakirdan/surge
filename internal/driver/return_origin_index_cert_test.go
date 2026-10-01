package driver

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/symbols"
)

// N-INDEXCERT: an index is answered by the callee its selection certifies.
//
//   - A Map read `m[k]` is the core `Map.__index` body: the checked summary
//     names only the map formal, so the element is a borrow of the indexed
//     map's storage, the owner an array element has. A store `m[k] = v` is the
//     core `Map.__index_set` body, the weak store insert makes into the map's
//     proven backing. Only a K and V that hold no borrow and keep no storage
//     loan are answered.
//   - An `own` container binding owns its elements as a by-value binding does.
//   - A nominal `__index` with an int index is the call an index of any other
//     type is (selectedIndexCall).
const indexCertSource = `type Holder = { m: Map<string, int> };
type P = { a: int, b: int };
type Bag = { values: int[] };
extern<Bag> {
    fn __index(self: &Bag, index: int) -> int {
        return self.values[index];
    }
}
type MutBag = { values: int[] };
extern<MutBag> {
    fn __index(self: &mut MutBag, index: int) -> int {
        return self.values[index];
    }
}
type RefBag = { values: int[] };
extern<RefBag> {
    fn __index(self: &RefBag, index: int) -> &int {
        return self.values[index];
    }
}

fn param_read(m: &Map<string, int>) -> &int {
    return m["x"];
}

fn deref_read(pm: &Map<string, int>) -> &int {
    return (*pm)["x"];
}

fn field_read(h: &Holder) -> &int {
    return h.m["x"];
}

fn int_key(m: &Map<int, P>, k: int) -> &P {
    return m[k];
}

fn local_read() -> int {
    let m = { "x" => 1 };
    let r: &int = m["x"];
    return *r;
}

fn local_escape() -> &int {
    let m = { "x" => 1 };
    let r: &int = m["x"];
    return r;
}

fn store_local() -> int {
    let mut m = Map::<string, int>.new();
    m["x"] = 3;
    let r: &int = m["x"];
    return *r;
}

fn store_param(m: &mut Map<string, int>) -> nothing {
    m["y"] = 4;
    return nothing;
}

fn loan_value(m: &Map<string, int[]>) -> &int[] {
    return m["a"];
}

fn loan_store(m: &mut Map<string, int[]>, xs: int[]) -> nothing {
    m["a"] = xs;
    return nothing;
}

fn own_read(xs: own int[]) -> int {
    return xs[0] + xs[1];
}

fn own_escape(xs: own int[]) -> &int {
    let r: &int = xs[0];
    return r;
}

fn nominal_read(b: &Bag) -> int {
    return b[1];
}

fn nominal_mut(b: &mut MutBag) -> int {
    return b[1];
}

fn nominal_ref(b: &RefBag) -> &int {
    return b[1];
}
`

const (
	indexCertMapElement  = "map index requires a key and value that hold no borrow and keep no storage loan"
	indexCertContainer   = "index requires its selected container transfer"
	indexCertDeclaration = "map index lacks its original core declaration certificate"
)

func TestAnalyzeIndexCertificate(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "index_certificate", indexCertSource, true, nil)
	key, file := f.unit.SourceKey, f.owner.File.ID
	for _, row := range []struct {
		name, header string
		slots        []uint32 // nil: an erased result
	}{
		{"map_read_names_the_map_formal", "fn param_read(", []uint32{0}},
		{"map_read_through_a_dereference", "fn deref_read(", []uint32{0}},
		{"map_read_through_a_field_of_a_formal", "fn field_read(", []uint32{0}},
		{"map_read_with_an_int_key", "fn int_key(", []uint32{0}},
		{"map_read_of_a_local_map", "fn local_read(", nil},
		{"map_store_then_read_of_a_local_map", "fn store_local(", nil},
		{"map_store_through_a_mutable_formal", "fn store_param(", nil},
		{"own_container_read", "fn own_read(", nil},
		{"nominal_int_index_with_an_erased_result", "fn nominal_read(", nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			fn := patternFn(t, indexCertSource, row.header)
			originExactPending(t, analysis, key, fn, nil)
			originNoEscape(t, analysis, file, fn)
			name := strings.TrimSuffix(strings.TrimPrefix(row.header, "fn "), "(")
			requireOriginSummary(t, analysis, file, name, false, row.slots)
		})
	}
	for _, row := range []struct{ name, header, at, reason string }{
		{"map_read_of_a_loan_carrying_value_stays_refused", "fn loan_value(", `m["a"]`, indexCertMapElement},
		{"map_store_of_a_loan_carrying_value_stays_refused", "fn loan_store(", `m["a"] = xs`, indexCertMapElement},
		{"nominal_mutable_receiver_stays_refused", "fn nominal_mut(", "b[1]", indexCertContainer},
		{"nominal_reference_result_stays_refused", "fn nominal_ref(", "b[1]", indexCertContainer},
	} {
		t.Run(row.name, func(t *testing.T) {
			fn := patternFn(t, indexCertSource, row.header)
			at := patternIn(t, indexCertSource, fn, row.at)
			if !originPendingAt(analysis, key, at, row.reason) {
				t.Errorf("%q lost its refusal %q: %+v", row.at, row.reason, originPendingWithin(analysis, key, fn.start, fn.end))
			}
		})
	}
	for _, row := range []struct{ name, header string }{
		{"local_map_element_escape_is_diagnosed", "fn local_escape("},
		{"own_container_element_escape_is_diagnosed", "fn own_escape("},
	} {
		t.Run(row.name, func(t *testing.T) {
			fn := patternFn(t, indexCertSource, row.header)
			at := patternIn(t, indexCertSource, fn, "return r;")
			if !slices.ContainsFunc(analysis.Diagnostics, func(d diag.Diagnostic) bool {
				return d.Code == diag.SemaBorrowEscapesReturn && d.Primary.File == file && int(d.Primary.Start) >= at.start && int(d.Primary.End) <= at.end
			}) {
				t.Errorf("the escaping element lost its SEM3139: diagnostics=%+v pending=%+v", analysis.Diagnostics, originPendingWithin(analysis, key, fn.start, fn.end))
			}
		})
	}
}

// A detached mutation replaces the selection on one Map index node with a
// namesake that is not the core Map body. The certificate is the declaration,
// so the swapped selection is refused by name and nothing is transferred.
func TestAnalyzeMapIndexCertificateAuthority(t *testing.T) {
	for _, row := range []struct {
		name  string
		set   bool
		match func(sema.CallableCandidate) bool
	}{
		{"read_selects_the_core_array_index", false, func(c sema.CallableCandidate) bool {
			return c.Name == "__index" && c.Builtin && c.Intrinsic && c.ModulePath == "core/intrinsics" && c.SourceKey == "builtin" &&
				len(c.TemplateParams) == 1 && len(c.ParamTypes) == 2 && strings.Contains(string(c.ReceiverKey), "Array") &&
				!strings.Contains(string(c.ReceiverKey), "Fixed") && !strings.Contains(string(c.Params[1]), "Range")
		}},
		{"read_selects_the_core_map_store", false, func(c sema.CallableCandidate) bool {
			return c.Name == "__index_set" && c.HasBody && strings.Contains(string(c.ReceiverKey), "Map")
		}},
		{"store_selects_the_core_map_read", true, func(c sema.CallableCandidate) bool {
			return c.Name == "__index" && c.HasBody && strings.Contains(string(c.ReceiverKey), "Map")
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			header, snippet := "fn param_read(", `m["x"]`
			if row.set {
				header, snippet = "fn store_param(", `m["y"]`
			}
			fn := patternFn(t, indexCertSource, header)
			at := patternIn(t, indexCertSource, fn, snippet)
			changed := 0
			f, analysis := analyzeOriginRoot(t, "index_certificate_"+row.name, indexCertSource, true, func(f originalGenericFixture) {
				replacement := indexCallReplacement(t, f, row.match)
				id := originExprAt(t, f.unit, f.owner.File.ID, at, ast.ExprIndex)
				for i := range f.inputs.units {
					unit := &f.inputs.units[i]
					if unit.Builder.Files.Get(unit.FileID).Span.File != f.owner.File.ID {
						continue
					}
					detached := *unit.Sema
					selections := map[ast.ExprID]symbols.SymbolID(detached.IndexSymbols)
					if row.set {
						selections = detached.IndexSetSymbols
					}
					selections = maps.Clone(selections)
					if !selections[id].IsValid() || selections[id] == replacement {
						t.Fatal("PRECONDITION: the original selection is not distinct from the replacement")
					}
					selections[id], changed = replacement, changed+1
					if row.set {
						detached.IndexSetSymbols = selections
					} else {
						detached.IndexSymbols = selections
					}
					unit.Sema = &detached
				}
			})
			if changed != 1 {
				t.Fatalf("PRECONDITION: the mutation reached %d owning units", changed)
			}
			if !originPendingAt(analysis, f.unit.SourceKey, at, indexCertDeclaration) {
				t.Errorf("a swapped Map index selection was not refused by its certificate: %+v", originPendingWithin(analysis, f.unit.SourceKey, fn.start, fn.end))
			}
		})
	}
}

// An imported function no live body calls has no finalized use for the
// generic index it performs: the closure is a reachability fixpoint. Its
// index is answered by the original request recorded at that site, as a
// generic call's is: a borrow of a formal is accepted, and an escape of a local
// is refused as the escape it is instead of as an unfinished analysis.
func TestReturnOriginUnreachedCallerIndex(t *testing.T) {
	const reads = "pub fn keep(xs: &byte[]) -> &byte {\n    return xs[1];\n}\n\npub fn read(xs: &int[], i: int) -> int {\n    return xs[i] + 1;\n}\n"
	const leak = "\npub fn leak() -> &int {\n    let xs: int[2] = [1, 2];\n    let r: &int = xs[0];\n    return r;\n}\n"
	const used = "pub fn used() -> int {\n    return 1;\n}\n\n"
	const main = "import ./mod as m;\n\n@entrypoint\nfn main() -> int {\n    return m.used() - 1;\n}\n"
	for _, tc := range []struct {
		name  string
		files map[string]string
		leaks bool
	}{
		{"second_file_of_a_module", map[string]string{"app/mod/a.sg": "pragma module;\n\n" + used, "app/mod/b.sg": "pragma module;\n\n" + reads, "app/main.sg": main}, false},
		{"single_file_module", map[string]string{"app/mod.sg": used + reads, "app/main.sg": main}, false},
		{"second_file_of_a_module_leaks", map[string]string{"app/mod/a.sg": "pragma module;\n\n" + used, "app/mod/b.sg": "pragma module;\n\n" + reads + leak, "app/main.sg": main}, true},
		{"single_file_module_leaks", map[string]string{"app/mod.sg": used + reads + leak, "app/main.sg": main}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			escapes, pending, failure := unreachedIndexOutcome(t, tc.files, "app/main.sg")
			if len(pending) != 0 {
				t.Fatalf("the unreached index stayed unfinished: %v", pending)
			}
			refused := len(escapes) != 0 || strings.Contains(failure, "SEM3139")
			if refused != tc.leaks || (!tc.leaks && failure != "") {
				t.Fatalf("leaks=%v but escapes=%v failure=%q", tc.leaks, escapes, failure)
			}
		})
	}
}

// unreachedIndexOutcome is importedCallableOutcome that keeps a failed
// diagnosis as its message: a SEM3139 inside an imported module fails the
// diagnosis of its importer (the refusal is not represented in the importer's
// diagnostics), which is a refusal all the same.
func unreachedIndexOutcome(t *testing.T, files map[string]string, target string) (escapes []int, pending []string, failure string) {
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
		return nil, pending, ""
	case err != nil:
		return nil, nil, err.Error()
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
	return escapes, nil, ""
}
