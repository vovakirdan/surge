package parser

// Generic arguments in a call are spelled with the turbofish. Two spellings get
// it wrong — `Object<T>(...)` and `Object<T>::f(...)` — and both report the same
// code, because they are the same mistake. These tests pin the number literally:
// a constant is always "its own", so only a literal here would notice another
// lane claiming 2208.

import (
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

const (
	bareGenericPathSource = "fn f() {\n    let ch = Channel<int>::new(2:uint);\n}\n"
	bareGenericCallSource = "fn id<T>(value: T) -> T {\n    return value;\n}\n\nfn f() {\n    let a: int = id<int>(1);\n}\n"
)

func diagnosticWithCode(bag *diag.Bag, id string) *diag.Diagnostic {
	if bag == nil {
		return nil
	}
	for _, d := range bag.Items() {
		if d != nil && d.Code.ID() == id {
			return d
		}
	}
	return nil
}

func TestBareGenericArgsReportSYN2208(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "path_tail", source: bareGenericPathSource},
		{name: "call_tail", source: bareGenericCallSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, bag, _ := parseProgram(t, tc.source)
			found := diagnosticWithCode(bag, "SYN2208")
			if found == nil {
				t.Fatalf("expected SYN2208 for %s, got: %s", tc.name, diagnosticsSummary(bag))
			}
			if found.Message != "generic type arguments must use '::<' syntax" {
				t.Fatalf("SYN2208 headline drifted: %q", found.Message)
			}
			if len(found.Notes) != 1 {
				t.Fatalf("SYN2208 must carry the note explaining the comparison reading, got %d notes", len(found.Notes))
			}
			if !strings.Contains(found.Notes[0].Msg, "less-than operator") {
				t.Fatalf("note stopped explaining why the line was read as a comparison: %q", found.Notes[0].Msg)
			}
			if len(found.Fixes) != 1 {
				t.Fatalf("SYN2208 must carry exactly one fix, got %d", len(found.Fixes))
			}
		})
	}
}

// The `::` spelling has no legal reading, so the parser carries on as though the
// turbofish had been written. Without that recovery the statement dies and the
// author is handed a cascade — "expected expression after binary operator",
// then an unresolved name — none of which names the real mistake.
func TestBareGenericPathRecoversInsteadOfCascading(t *testing.T) {
	_, bag, _ := parseProgram(t, bareGenericPathSource)
	for _, unwanted := range []string{"SYN2203", "SEM3005"} {
		if diagnosticWithCode(bag, unwanted) != nil {
			t.Fatalf("recovery should have suppressed %s, got: %s", unwanted, diagnosticsSummary(bag))
		}
	}
	if got := len(bag.Items()); got != 1 {
		t.Fatalf("expected exactly one diagnostic after recovery, got %d: %s", got, diagnosticsSummary(bag))
	}
}

// The quick fix is offered, not swept in: the byte scan that recognises the
// shape shares its window with a legitimate double comparison in the `(` tail,
// so the edit stays heuristic and `--all` lists it instead of applying it.
// Both spellings carry the same one, so neither drifts into a different answer.
func TestBareGenericFixInsertsTurbofishAndStaysHeuristic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "path_tail", source: bareGenericPathSource},
		{name: "call_tail", source: bareGenericCallSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := source.NewFileSet()
			fs.AddVirtual("test.sg", []byte(tc.source))

			_, bag, _ := parseProgram(t, tc.source)
			found := diagnosticWithCode(bag, "SYN2208")
			if found == nil {
				t.Fatalf("expected SYN2208, got: %s", diagnosticsSummary(bag))
			}

			materialized, err := diag.MaterializeFixes(diag.FixBuildContext{FileSet: fs}, found.Fixes)
			if err != nil {
				t.Fatalf("materialize fixes: %v", err)
			}
			if len(materialized) != 1 {
				t.Fatalf("expected one materialized fix, got %d", len(materialized))
			}
			got := materialized[0]
			if got.Title != "insert '::' for generic call" {
				t.Fatalf("fix title drifted: %q", got.Title)
			}
			if got.Applicability != diag.FixApplicabilitySafeWithHeuristics {
				t.Fatalf("fix must stay heuristic so --all does not apply it, got %v", got.Applicability)
			}
			if !got.IsPreferred {
				t.Fatalf("fix must be preferred: it is the edit the author almost certainly wants")
			}
			if len(got.Edits) != 1 {
				t.Fatalf("expected exactly one edit, got %+v", got.Edits)
			}
			edit := got.Edits[0]
			if edit.NewText != "::" {
				t.Fatalf("fix must insert exactly '::', got %q", edit.NewText)
			}
			// An insertion, not a replacement: the span is empty and sits where
			// the `::` belongs, so the callee text itself is never rewritten.
			if edit.Span.Start != edit.Span.End {
				t.Fatalf("insertion span must be empty, got %+v", edit.Span)
			}
		})
	}
}
