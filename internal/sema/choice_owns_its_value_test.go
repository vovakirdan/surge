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
fn build() -> string {
    return "x";
}

fn peek(x: &string) -> int {
    return 1;
}

fn all_mint(cond: bool) -> int {
    return peek(cond ? build() : build());
}

fn one_forwards(cond: bool, a: string) -> int {
    return peek(cond ? build() : a);
}

fn nested_all_mint(cond: bool, other: bool) -> int {
    return peek(cond ? (other ? build() : build()) : build());
}

fn nested_inner_forwards(cond: bool, other: bool, a: string) -> int {
    return peek(cond ? (other ? build() : a) : build());
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
	// The ternaries here are BORROWED — `peek` only looks at the value — which
	// is the context this rule governs: nothing downstream owns the result, so
	// the choice must own it or nobody does. (A consumed result has an owner
	// already; a DISCARDED one is reclaimed branch by branch, which
	// TestDiscardedChoiceIsReclaimedBranchByBranch covers.)
	//
	// Three outcomes, and the test is that each shape lands in the right one:
	//
	//   every branch MINTS      owns its value, released UNCONDITIONALLY;
	//   branches DISAGREE       owns it, released under a GUARD the minting
	//                           branches raise, because the forwarded value is
	//                           not this expression's to free;
	//   every branch FORWARDS   owns nothing, releases nothing.
	//
	// all_mint and nested_all_mint are unconditional. one_forwards is guarded.
	// nested_inner_forwards is guarded TWICE: the inner mixed ternary keeps its
	// own guarded release rather than handing it up — the outer cannot know
	// which inner path ran — and the outer is mixed in turn, because a guarded
	// branch counts as forwarding. Claiming it instead is a double free, which
	// is what an earlier version of this did.
	guarded := 0
	for exprID := range res.TempDrops {
		if _, isGuarded := res.ChoiceReleaseGuards[exprID]; isGuarded {
			guarded++
		}
	}
	unconditional := len(res.TempDrops) - guarded
	if unconditional != 2 || guarded != 3 {
		t.Fatalf(
			"expected 2 unconditional and 3 guarded choice releases, got %d and %d across %d "+
				"statement-end temporaries; a branch that forwards a place — including a nested "+
				"choice that only sometimes mints — leaves that value someone else's to release",
			unconditional, guarded, len(res.TempDrops),
		)
	}
}
