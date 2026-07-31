package sema

import "testing"

// A choice expression whose branches all BUILT their value owns that value, so
// a statement-end drop has to reclaim it when nothing else takes it.
//
// `peek(cond ? build() : build())` leaked one allocation per evaluation:
// control-flow expressions were never flagged as producing an owned value, on
// the grounds that a branch may forward a PLACE instead of building — and
// releasing a forwarded place is a use-after-free through its owner. That is
// true of SOME branches, which is why the rule cannot be reversed either: the
// question has to be asked per branch.
//
// Being a pending temp candidate is necessary and NOT sufficient, so the flag
// alone would be unsound. A cast is flagged, yet it reads its source without
// consuming and an identity cast lowers to the same pointer, so
// `cond ? (a to float) : (b to float)` would look like two fresh values while
// both alias a live binding. The branch kind has to say the value was minted.
func TestChoiceOwnsItsValueOnlyWhenEveryBranchMints(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
type Held = { v: int };

fn make() -> Held {
    return Held { v = 1 };
}

fn every_branch_mints(cond: bool) -> int {
    cond ? make() : make();
    return 1;
}

fn one_branch_forwards(cond: bool, a: Held) -> int {
    cond ? make() : a;
    return 1;
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}

	// Exactly one of the two ternaries may own its value. The forwarding one
	// must NOT be flagged: `a` is still its caller's, and a drop synthesized
	// here would free it under them.
	//
	// The pair is the assertion. "At least one" would pass on a rule that
	// flags everything, which double-frees; "none" is the state this was
	// written against, which leaks.
	// TempDrops is the published set: the evaluations that produce an owned
	// value nothing consumes, which HIR wraps and MIR frees at the region end.
	// In this snippet only a ternary can be in it — every other owned value
	// here is consumed by a `return` or by the ternary itself — so its size is
	// the count of ternaries that own their value.
	if got := len(res.TempDrops); got != 1 {
		t.Fatalf(
			"expected exactly the all-minting ternary to own its value, got %d statement-end "+
				"temporaries; a branch that forwards a place leaves the value someone else's to release",
			got,
		)
	}
}
