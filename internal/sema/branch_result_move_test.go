package sema

import "testing"

// A many-branch expression hands ONE branch's value onward, so consuming it
// must move that branch's operand — and leave every other branch holding what
// it still has.
//
// `let picked = cond ? a : b` moved nothing at all: a ternary is not a place,
// so observeMove on it resolved nothing and both bindings kept a scope-exit
// obligation one of them had already given away. Measured as a double free and
// a read of freed memory. A compare arm returning a binding declared OUTSIDE
// the compare had the same hole.
//
// Marking both moved and stopping there is not the fix either: only one branch
// runs, so the other's binding would be abandoned. Each branch owes what the
// others gave away, and this asserts both halves at once — the moves land, and
// the compensating drops land on the branches that need them.
func TestTernaryBranchesMoveAndCompensate(t *testing.T) {
	// `Held` owns a string so that it owes a drop at all: a struct of plain
	// scalars is bits under the OwnsHeap axis (ownership_axes.go), and the
	// compensating drops this test counts exist only for a value with
	// something to reclaim.
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
type Held = { v: string };

fn consumed(cond: bool, a: Held, b: Held) -> Held {
    return cond ? a : b;
}

fn take(x: Held) -> Held {
    return x;
}

fn moved_while_typed(cond: bool, a: Held, b: Held) -> Held {
    return cond ? take(a) : b;
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}

	// Two functions, two ways a branch can give something away, one drop owed
	// per branch in both:
	//
	//   consumed          the branch RESULT is handed onward when the ternary
	//                     is consumed, so each branch owes the sibling's
	//                     binding — the one that path did not take and that
	//                     therefore has nobody else to reclaim it;
	//   moved_while_typed the true branch moves `a` while it is EVALUATED, so
	//                     the false branch owes `a`; the false branch hands `b`
	//                     onward, so the true branch owes `b`.
	//
	// The second is the shape a review caught: the branches used to be typed
	// without isolating their moved-sets, so the false branch inherited the
	// true branch's move of `a` and nobody reclaimed it on the path where
	// `take` never ran.
	//
	// One drop per branch is what makes this pin the SHAPE rather than a count.
	// A fix that moves nothing leaves zero here and double-frees; one that
	// moves everything and compensates nothing also leaves zero, and leaks.
	total := 0
	for branch, drops := range res.ArmDropsExpr {
		if len(drops) != 1 {
			t.Fatalf("branch %v: expected exactly one compensating drop, got %d", branch, len(drops))
		}
		total += len(drops)
	}
	if total != 4 {
		t.Fatalf(
			"expected four ternary branches to owe one drop each, got %d across %d branches; "+
				"a branch that is not taken still holds its operand, whether the sibling gave it away "+
				"by being evaluated or by being handed onward",
			total, len(res.ArmDropsExpr),
		)
	}
}
