package fix

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

func TestGatherCandidatesSkipsDuplicateFixIDs(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte(""))
	span := source.Span{File: fileID, Start: 0, End: 0}

	diagnostics := []diag.Diagnostic{{
		Code:    diag.SynExpectSemicolon,
		Message: "missing semicolon",
		Primary: span,
		Fixes: []diag.Fix{
			{
				ID:    "fix-duplicate",
				Title: "insert semicolon",
				Edits: []diag.TextEdit{{Span: span, NewText: ";"}},
			},
			{
				ID:    "fix-duplicate",
				Title: "insert semicolon again",
				Edits: []diag.TextEdit{{Span: span, NewText: ";"}},
			},
		},
	}}

	ctx := diag.FixBuildContext{FileSet: fs}
	candidates, skips := gatherCandidates(ctx, diagnostics)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if len(skips) != 1 {
		t.Fatalf("expected 1 skipped fix, got %d", len(skips))
	}

	skip := skips[0]
	if skip.ID != "fix-duplicate" {
		t.Fatalf("expected skipped fix id 'fix-duplicate', got %q", skip.ID)
	}
	if skip.Reason != "duplicate fix id" {
		t.Fatalf("expected duplicate fix reason, got %q", skip.Reason)
	}
}
