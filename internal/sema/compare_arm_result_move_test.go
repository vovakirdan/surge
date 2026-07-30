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

	// Three shapes, and the obligation turns on WHO ends up owning the value,
	// not on how the arm is written:
	//
	//   returns_payload  the `return` consumes the compare, so the payload has
	//                    a new owner and the arm owes nothing;
	//   uses_payload     the payload never leaves its arm, so the arm owes it;
	//   borrows_result   the compare's value is only BORROWED by peek, so
	//                    nobody downstream owns the payload and the arm still
	//                    owes it — the case a review caught after the first
	//                    version of this fix moved unconditionally and leaked
	//                    one string per evaluation here.
	//
	// The two owing arms are also what stops this from passing vacuously: an
	// assertion of "no obligations" would hold just as well if arm obligations
	// stopped being recorded at all, which is the leak the machinery exists to
	// prevent.
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
	if total != 2 {
		t.Fatalf(
			"expected exactly the locally-used and the borrowed-result payloads to owe a drop, got %d "+
				"obligations across %d arms; an arm that RETURNS its payload into a CONSUMING context must "+
				"not also drop it, and one whose result is only borrowed must",
			total, len(res.ArmDropsExpr),
		)
	}
}
