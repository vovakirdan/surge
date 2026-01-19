package sema

import "testing"

func TestMoveTrackingIfConsumesInBothBranches(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn consume(s: string) -> nothing {
	return nothing;
}

fn main() -> nothing {
	let s = "hi";
	if true {
		consume(s);
	} else {
		consume(s);
	}
	return nothing;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if semaBag.HasErrors() {
		t.Fatalf("unexpected semantic diagnostics: %s", diagnosticsSummary(semaBag))
	}
}
