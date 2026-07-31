package sema

import "testing"

// A DISCARDED choice is reclaimed branch by branch, not as a whole.
//
// Its value goes nowhere, so there is no consumer to hand the drop candidacy
// to — and handing it to the choice itself is wrong the moment one branch
// FORWARDS a place, which is the ordinary `cond ? a : build()` spelling.
// Leaving each branch holding its own is what reclaims the minting side
// without touching the forwarding one, and the drop lands in the region where
// the value was materialized, so it is dominated.
//
// This is the only shape where a MIXED choice can be reclaimed without
// duplicating the consumer into the branches. A borrowed mixed choice still
// leaks, and needs that duplication.
func TestDiscardedChoiceIsReclaimedBranchByBranch(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn build() -> string {
    return "x";
}

fn discarded_mixed(cond: bool, a: string) -> int {
    cond ? build() : a;
    return 1;
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}

	// Exactly the minting branch owes a drop. Not the forwarding one, whose
	// value belongs to the caller, and not the ternary, which would free the
	// forwarded value on the path that takes it.
	if got := len(res.TempDrops); got != 1 {
		t.Fatalf(
			"expected exactly the minting branch of a discarded ternary to owe a drop, got %d "+
				"statement-end temporaries; the forwarding branch's value is not this expression's "+
				"to release",
			got,
		)
	}
}
