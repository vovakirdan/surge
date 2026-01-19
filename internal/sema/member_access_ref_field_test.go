package sema

import "testing"

func TestMemberAccessReferenceField(t *testing.T) {
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
	if semaBag.HasErrors() {
		t.Fatalf("unexpected semantic diagnostics: %s", diagnosticsSummary(semaBag))
	}
}
