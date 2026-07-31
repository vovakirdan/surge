package sema

import "testing"

// An arm whose RESULT is one of its own payload bindings hands that binding
// onward, so it must not also owe a drop on it.
//
// The arm's result is a consuming position exactly like a block's tail
// expression or a `return` operand, but no walk observed the move there, so a
// payload binding could be both returned and freed. That is a double free on
// the native backend and an invalid read wherever the caller looks at the
// value first — from `compare v { Payload(s) => s; ... }`, which is the
// ordinary spelling, not a corner.
//
// This lives in sema rather than in the native e2e corpus on purpose. The e2e
// gates DID catch this, and they run only with SURGE_SKIP_TIMEOUT_TESTS=0 —
// `make check` sets it to 1 and skips every one of them, so the regression
// landed with a green gate. A sema-level assertion costs milliseconds and runs
// in the suite that actually gates a commit.
func TestCompareArmReturningItsPayloadOwesNoDrop(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
tag Payload(string);
tag Empty();
type Outcome = Payload(string) | Empty();

fn returns_payload(v: Outcome) -> string {
    return compare v {
        Payload(s) => s;
        Empty() => "";
    };
}

fn peek(x: &string) -> int {
    return 1;
}

fn uses_payload(v: Outcome) -> int {
    return compare v {
        Payload(s) => peek(&s);
        Empty() => 0;
    };
}

fn borrows_result(v: Outcome) -> int {
    return peek(compare v {
        Payload(s) => s;
        Empty() => "";
    });
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}

	// Three shapes, and the obligation turns on whether the payload LEAVES its
	// arm, not on what the receiver does with it:
	//
	//   returns_payload  the arm answers with the payload, so the compare's
	//                    value is the payload and the arm owes nothing;
	//   uses_payload     the payload never leaves its arm — the result is an
	//                    int computed from a borrow — so the arm owes it;
	//   borrows_result   the arm answers with the payload again, and the
	//                    compare's value is only BORROWED. The arm still owes
	//                    nothing; the COMPARE owns the value and releases it
	//                    when the statement ends.
	//
	// That last row is the whole point, and it is asserted from both sides. The
	// arm keeping the obligation used to be the answer here, and it freed the
	// payload while the borrower was still reading it. Simply dropping the
	// obligation is not the answer either — an earlier attempt did exactly that
	// and leaked one string per evaluation, which is why the compare's own
	// ownership is asserted rather than assumed.
	total := 0
	for expr, drops := range res.ArmDropsExpr {
		if len(drops) == 0 {
			continue
		}
		total += len(drops)
		if len(drops) != 1 {
			t.Fatalf("arm %v: expected at most one payload obligation, got %d", expr, len(drops))
		}
	}
	if total != 1 {
		t.Fatalf(
			"expected exactly the locally-used payload to owe a drop, got %d obligations across %d "+
				"arms; an arm that ANSWERS with its payload hands it to the compare and must not also "+
				"drop it, whether the result is consumed or only borrowed",
			total, len(res.ArmDropsExpr),
		)
	}

	// The other side of the same contract: the borrowed compare is the one
	// evaluation here nothing consumes, so it must be the one statement-end
	// release the snippet records. Every other candidate in it is consumed —
	// the arm literals by their compare, the compare in `returns_payload` by
	// the return — so a count of one IS that compare, and a count of zero is
	// the leak this fix must not reintroduce.
	if len(res.TempDrops) != 1 {
		t.Fatalf(
			"expected exactly one statement-end release — the borrowed compare's own value — got %d; "+
				"an arm that hands its payload to a compare nobody consumes leaks it unless the compare "+
				"owns it",
			len(res.TempDrops),
		)
	}
}
