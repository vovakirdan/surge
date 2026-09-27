package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// A shared and a `&mut` parameter fed one reference, and references a call
// returned, reach the same referent as the reference they came from: each was
// accepted and read freed storage natively (valgrind: invalid read).
func TestMutRefArgumentsReachingOneReferentAreChecked(t *testing.T) {
	rows := []struct{ name, text, snippet string }{
		{"shared_then_mut_parameter_fed_one_reference", `fn g(r: &mut int[]) -> int {
    rd2(r, r);
    return 0;
}
`, "r);"},
		{"mut_then_shared_parameter_fed_one_reference", `fn g(r: &mut int[]) -> int {
    rd3(r, r);
    return 0;
}
`, "r);"},
		{"returned_reference_and_its_source", `fn g(r: &mut int[]) -> int {
    app2(id(r), r);
    return 0;
}
`, "r);"},
		{"two_references_returned_from_one_source", `fn g(r: &mut int[]) -> int {
    app2(id(r), id(r));
    return 0;
}
`, "id(r));"},
		{"bound_returned_reference_and_its_source", `fn g(r: &mut int[]) -> int {
    let q = id(r);
    app2(q, r);
    return 0;
}
`, "r);\n    return"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, mutRefHandOffPrelude+row.text, diag.SemaBorrowConflict, row.snippet)
		})
	}
}

func TestMutRefArgumentsReachingDifferentReferentsStayAccepted(t *testing.T) {
	bytesViewAccepted(t, mutRefHandOffPrelude+`fn g(r: &mut int[], s: &mut int[]) -> int {
    let n = two(r, r);
    rd2(s, r);
    rd3(r, s);
    app2(id(r), s);
    app2(id(s), id(r));
    return n;
}
`)
}

// A store through a `&mut` reference reaches its referent, and never revives
// the reference itself: after `let r3 = r;` or `@drop r`, `*r = v` is refused
// as it was before the referent's paths were spelled one way (a regression
// that accepted m08/h02 and read freed storage natively).
func TestStoreThroughAMovedMutRefStaysRefused(t *testing.T) {
	rows := []struct{ name, text, snippet string }{
		{"moved_string_reference_then_store_and_view", `fn g(r: &mut string) -> int {
    let r3 = r;
    *r = "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz0";
    let v = r.bytes();
    overwrite(r3);
    return v[0] to int;
}
`, `*r = "abc`},
		{"moved_array_reference_then_store_and_element", `fn g(r: &mut int[]) -> int {
    let r3 = r;
    *r = [1, 2, 3];
    let e = &r[0];
    app(r3);
    return *e;
}
`, "*r = [1"},
		{"moved_scalar_reference_then_store", `fn g(r: &mut int) -> int {
    let r3 = r;
    *r = 5;
    *r3 = 6;
    return 0;
}
`, "*r = 5"},
		{"dropped_reference_then_store", `fn g(r: &mut int[]) -> int {
    @drop r;
    *r = [1, 2, 3];
    return 0;
}
`, "*r = [1"},
		{"dropped_local_reference_then_store", `fn g() -> int {
    let mut ys: int[] = [10, 20];
    let r = &mut ys;
    @drop r;
    let e = &ys[0];
    *r = [1, 2, 3, 4, 5, 6, 7, 8, 9];
    return *e;
}
`, "*r = [1"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			storeIntoMovedRefusal(t, mutRefHandOffPrelude+row.text, row.snippet)
		})
	}
}

// storeIntoMovedRefusal requires a use-after-move error at the store and no
// error of any other code.
func storeIntoMovedRefusal(t *testing.T, text, snippet string) {
	t.Helper()
	result, err := bytesViewDiagnose(t, text)
	if result == nil || result.Bag == nil {
		t.Fatalf("no diagnostics bag: %v", err)
	}
	start := strings.Index(text, snippet)
	if start < 0 || strings.Count(text, snippet) != 1 {
		t.Fatalf("PRECONDITION: %q is not unique in the fixture", snippet)
	}
	atStore := false
	for _, d := range result.Bag.Items() {
		if d.Severity < diag.SevError {
			continue
		}
		if d.Code != diag.SemaUseAfterMove {
			t.Fatalf("PRECONDITION: the fixture has another error:\n%s", diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
		}
		if int(d.Primary.Start) == start {
			atStore = true
		}
	}
	if !atStore {
		t.Fatalf("want %s at %q, got:\n%s", diag.SemaUseAfterMove.ID(), snippet, diag.FormatGoldenDiagnostics(result.Bag.Items(), result.FileSet, false))
	}
}
