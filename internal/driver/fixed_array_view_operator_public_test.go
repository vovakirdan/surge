package driver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

// The refusal a programmer actually reads, taken from the published bag.
//
// Golden `.diag` files record no notes at all, so a golden fixture cannot witness the
// two the call route adds. The public diagnosis keeps them, which is where this test
// looks. It stops at the SEMA stage on purpose: the eager rule is all it is about, the
// operator's own body carries an unrelated refusal further down the pipeline, and
// stopping here keeps the test valid on a lane that has no return-origin analysis.
const publicFixedViewSource = `type Arr = { items: uint64[4] };

extern<Arr> {
    fn __add(self: &Arr, other: &Arr) -> uint64[] {
        return self.items[[0..2]];
    }
}

fn bound() -> uint64[] {
    let a: Arr = Arr { items = [11:uint64, 22:uint64, 33:uint64, 44:uint64] };
    let b: Arr = Arr { items = [55:uint64, 66:uint64, 77:uint64, 88:uint64] };
    let v = a + b;
    return v;
}

@entrypoint
fn main() -> int {
    let p = bound();
    return (p[0] to int) - 11;
}
`

const publicFixedViewDigest = "25622e3856da01c09ae26db0f943fecc01eeeb0992ed60eb40c9725a33d936ca"

func TestDiagnoseFixedArrayViewEscapesThroughOperator(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(publicFixedViewSource))); got != publicFixedViewDigest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	for _, frozen := range []struct {
		start, end int
		snippet    string
	}{
		{61, 71, "self: &Arr"},
		{339, 344, "a + b"},
		{357, 358, "v"},
	} {
		if publicFixedViewSource[frozen.start:frozen.end] != frozen.snippet {
			t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", frozen.start, frozen.end, frozen.snippet)
		}
	}
	stdlibRoot := detectStdlibRootFrom(".")
	if stdlibRoot == "" {
		t.Fatal("failed to locate stdlib root")
	}
	t.Setenv("SURGE_STDLIB", stdlibRoot)
	dir := t.TempDir()
	path := filepath.Join(dir, "origin.sg")
	if err := os.WriteFile(path, []byte(publicFixedViewSource), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := DiagnoseOptions{Stage: DiagnoseStageSema, BaseDir: dir, MaxDiagnostics: 64, IgnoreWarnings: true, KeepArtifacts: true}
	res, err := DiagnoseWithOptions(t.Context(), path, &opts)
	if err != nil {
		t.Fatalf("public diagnosis did not finish: %v", err)
	}
	if res == nil || res.Bag == nil || res.File == nil {
		t.Fatal("public diagnosis returned no result")
	}
	items, marshalErr := json.Marshal(res.Bag.Items())
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	t.Logf("FIXED_VIEW_OPERATOR_PUBLIC diagnostics=%s", items)
	var errs []*diag.Diagnostic
	for _, d := range res.Bag.Items() {
		if d != nil && d.Severity >= diag.SevError {
			errs = append(errs, d)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("published errors=%s, want exactly one SEM3198", items)
	}
	got := errs[0]
	primary := source.Span{File: res.File.ID, Start: 357, End: 358}
	const headline = "cannot return a slice of local 'a': it is a fixed array, so the slice points at this call frame"
	if got.Code != diag.SemaFixedArrayViewEscapes || got.Primary != primary || got.Message != headline {
		t.Fatalf("refusal = %+v, want SEM3198 at %v with the fixed-array headline", *got, primary)
	}
	wants := []diag.Note{
		{Span: source.Span{File: res.File.ID, Start: 339, End: 344},
			Msg: "'__add' gives back a slice of what its parameter 'self' points at, and here that is 'a'"},
		{Span: source.Span{File: res.File.ID, Start: 61, End: 71},
			Msg: "this parameter is a reference: its referent is the caller's, which is why the slice " +
				"is allowed here and refused there"},
	}
	for _, want := range wants {
		if !slices.ContainsFunc(got.Notes, func(note diag.Note) bool { return note == want }) {
			t.Errorf("missing note %+v in %+v", want, got.Notes)
		}
	}
}
