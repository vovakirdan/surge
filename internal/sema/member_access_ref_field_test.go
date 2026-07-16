package sema

import (
	"strings"
	"testing"
)

// A reference stored in a struct field escapes borrow tracking (loans live
// only in local bindings), so the field could outlive the borrowed value and
// dangle. Aggregates hold owned values only; the view-struct pattern below
// must be rejected at the declaration.
func TestReferenceStructFieldRejected(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Buffer = { w: int }
type Frame = { buf: &mut Buffer }

fn draw(buf: &mut Buffer) -> nothing {
	return nothing;
}

fn use(fr: &mut Frame) -> nothing {
	draw(fr.buf);
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !semaBag.HasErrors() {
		t.Fatalf("expected the reference field to be rejected, got no diagnostics")
	}
	summary := diagnosticsSummary(semaBag)
	if !strings.Contains(summary, "SEM3138") {
		t.Fatalf("expected SEM3138 (reference stored in aggregate), got: %s", summary)
	}
}
