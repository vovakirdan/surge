package fix

import (
	"errors"
	"os"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

// probeSource is the one line every row edits. Keeping it fixed is what lets a
// row assert the exact bytes after an apply, and the exact bytes after a
// refusal.
const probeSource = "let x = 1"

// applyProbe puts one diagnostic carrying the given fixes over a known file.
func applyProbe(t *testing.T, fixes ...*diag.Fix) (*source.FileSet, []*diag.Diagnostic, string) {
	t.Helper()
	content := probeSource
	path, _ := createTestFile(t, "probe.sg", []byte(content))
	fs := source.NewFileSet()
	fileID := fs.Add(path, []byte(content), 0)
	for _, f := range fixes {
		for i := range f.Edits {
			f.Edits[i].Span.File = fileID
		}
	}
	return fs, []*diag.Diagnostic{{
		Severity: diag.SevError,
		Code:     diag.Code(0x0001),
		Message:  "probe",
		Primary:  source.Span{File: fileID, Start: 0, End: 1},
		Fixes:    fixes,
	}}, path
}

// TestApplyOnceRefusesEverythingButAlwaysSafe is the automatic-application
// contract.
//
// `--once` used to fall back to the first candidate of ANY applicability when
// no safe one existed. That turned "the compiler is not sure this edit
// preserves intent" into "the compiler edited your source anyway" — the one
// outcome an applicability grade exists to prevent.
func TestApplyOnceRefusesEverythingButAlwaysSafe(t *testing.T) {
	for _, applicability := range []diag.FixApplicability{
		diag.FixApplicabilitySafeWithHeuristics,
		diag.FixApplicabilityManualReview,
	} {
		t.Run(applicability.String(), func(t *testing.T) {
			fs, diagnostics, path := applyProbe(t, &diag.Fix{
				ID:            "unsafe-1",
				Title:         "an edit needing a human",
				Applicability: applicability,
				Edits: []diag.TextEdit{
					{Span: source.Span{Start: 0, End: 3}, NewText: "var", OldText: "let"},
				},
			})
			res, err := Apply(fs, diagnostics, ApplyOptions{Mode: ApplyModeOnce})
			if !errors.Is(err, ErrNoFixes) {
				t.Fatalf("apply error = %v, want ErrNoFixes", err)
			}
			if len(res.Applied) != 0 {
				t.Fatalf("an unsafe edit was applied automatically: %+v", res.Applied)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read probe: %v", readErr)
			}
			if string(after) != probeSource {
				t.Fatalf("the file was rewritten: %q", after)
			}
			// A skip with no way forward reads as a defect. The reason names
			// the grade and the flag that applies it deliberately.
			if len(res.Skipped) != 1 {
				t.Fatalf("skipped = %+v, want one explained skip", res.Skipped)
			}
			if !strings.Contains(res.Skipped[0].Reason, applicability.String()) ||
				!strings.Contains(res.Skipped[0].Reason, "--id unsafe-1") {
				t.Fatalf("skip reason does not say why or how to proceed: %q", res.Skipped[0].Reason)
			}
		})
	}
}

// TestApplyOnceStillTakesTheSafeEditPastAnUnsafeOne keeps the change from
// becoming "apply nothing when anything is unsafe".
func TestApplyOnceStillTakesTheSafeEditPastAnUnsafeOne(t *testing.T) {
	fs, diagnostics, path := applyProbe(t,
		&diag.Fix{
			ID:            "unsafe-1",
			Title:         "needs a human",
			Applicability: diag.FixApplicabilityManualReview,
			Edits:         []diag.TextEdit{{Span: source.Span{Start: 0, End: 3}, NewText: "var", OldText: "let"}},
		},
		&diag.Fix{
			ID:            "safe-1",
			Title:         "always safe",
			Applicability: diag.FixApplicabilityAlwaysSafe,
			Edits:         []diag.TextEdit{{Span: source.Span{Start: 8, End: 9}, NewText: "2", OldText: "1"}},
		},
	)
	res, err := Apply(fs, diagnostics, ApplyOptions{Mode: ApplyModeOnce})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res.Applied) != 1 || res.Applied[0].ID != "safe-1" {
		t.Fatalf("applied = %+v, want the always-safe edit alone", res.Applied)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read probe: %v", readErr)
	}
	if string(after) != "let x = 2" {
		t.Fatalf("file after apply = %q", after)
	}
}

// TestApplyOnceReachesAnUnsafeEditByID proves the way forward the skip reason
// names actually exists.
func TestApplyOnceReachesAnUnsafeEditByID(t *testing.T) {
	fs, diagnostics, path := applyProbe(t, &diag.Fix{
		ID:            "unsafe-1",
		Title:         "needs a human",
		Applicability: diag.FixApplicabilityManualReview,
		Edits:         []diag.TextEdit{{Span: source.Span{Start: 0, End: 3}, NewText: "var", OldText: "let"}},
	})
	if _, err := Apply(fs, diagnostics, ApplyOptions{Mode: ApplyModeID, TargetID: "unsafe-1"}); err != nil {
		t.Fatalf("apply by id: %v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read probe: %v", readErr)
	}
	if string(after) != "var x = 1" {
		t.Fatalf("an explicitly requested edit did not apply: %q", after)
	}
}

// TestUnguardedReplaceIsRefused pins the other half of the safety contract.
//
// A replace or a delete destroys what is there. Without an OldText guard a span
// that has since moved silently eats whatever now sits at those offsets, and
// nothing in the pipeline would notice. Empty OldText means insertion, and
// nothing else.
func TestUnguardedReplaceIsRefused(t *testing.T) {
	cases := []struct {
		name string
		edit diag.TextEdit
	}{
		{"replace", diag.TextEdit{Span: source.Span{Start: 0, End: 3}, NewText: "var"}},
		{"delete", diag.TextEdit{Span: source.Span{Start: 0, End: 4}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fs, diagnostics, path := applyProbe(t, &diag.Fix{
				ID:            "unguarded-1",
				Title:         "an edit that never looked",
				Applicability: diag.FixApplicabilityAlwaysSafe,
				Edits:         []diag.TextEdit{testCase.edit},
			})
			res, err := Apply(fs, diagnostics, ApplyOptions{Mode: ApplyModeAll})
			if !errors.Is(err, ErrNoFixes) {
				t.Fatalf("apply error = %v, want ErrNoFixes", err)
			}
			if len(res.Applied) != 0 {
				t.Fatalf("an unguarded %s was applied: %+v", testCase.name, res.Applied)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read probe: %v", readErr)
			}
			if string(after) != probeSource {
				t.Fatalf("the file was rewritten by an unguarded %s: %q", testCase.name, after)
			}
			if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "OldText") {
				t.Fatalf("skip reason does not name the missing guard: %+v", res.Skipped)
			}
		})
	}
}

// TestUnguardedInsertionStaysLegal is the boundary: an insertion adds between
// two characters and is fully described by its position, so it needs no guard.
func TestUnguardedInsertionStaysLegal(t *testing.T) {
	fs, diagnostics, path := applyProbe(t, &diag.Fix{
		ID:            "insert-1",
		Title:         "an insertion",
		Applicability: diag.FixApplicabilityAlwaysSafe,
		Edits:         []diag.TextEdit{{Span: source.Span{Start: 0, End: 0}, NewText: "own "}},
	})
	if _, err := Apply(fs, diagnostics, ApplyOptions{Mode: ApplyModeAll}); err != nil {
		t.Fatalf("apply insertion: %v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read probe: %v", readErr)
	}
	if string(after) != "own let x = 1" {
		t.Fatalf("insertion result = %q", after)
	}
}
