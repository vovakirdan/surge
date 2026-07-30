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
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
type Held = { v: int };

fn consumed(cond: bool, a: Held, b: Held) -> Held {
    return cond ? a : b;
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}

	// `consumed` gives one branch away on each path, so each of its two
	// branches owes exactly one drop: the sibling's binding, which that path
	// did not take and which therefore has nobody else to reclaim it.
	//
	// Two branches owing one each is what makes this pin the SHAPE rather than
	// a count. A fix that moved both operands and stopped would leave zero here
	// and leak; one that moved neither — the state this test was written
	// against — would also leave zero, and double-free instead.
	total := 0
	for branch, drops := range res.ArmDropsExpr {
		if len(drops) != 1 {
			t.Fatalf("branch %v: expected exactly one compensating drop, got %d", branch, len(drops))
		}
		total += len(drops)
	}
	if total != 2 {
		t.Fatalf(
			"expected the two consumed ternary branches to owe one drop each, got %d across %d branches; "+
				"a branch that is not taken still holds its operand",
			total, len(res.ArmDropsExpr),
		)
	}
}
