package sema

import "testing"

// Statement-end temporaries: the classification rows. The final set must
// contain exactly the owned evaluations nothing consumes — a missed
// consumer is a use-after-free through the consumer, a phantom producer
// is a double free.

func tempDropCount(t *testing.T, src string) int {
	t.Helper()
	parseBag, semaBag, res := runSemaOnSnippetResult(t, src)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	return len(res.TempDrops)
}

func TestConcatOperandsAreFlagged(t *testing.T) {
	// __add borrows both operands; the literals and nothing else dangle
	// (the concat result is consumed by the let).
	count := tempDropCount(t, `
extern<string> {
    @intrinsic fn __add(self: &string, other: &string) -> string;
}

fn f() -> nothing {
    let s: string = "a" + "b";
}
`)
	if count != 2 {
		t.Fatalf("expected the two literal operands flagged, got %d", count)
	}
}

func TestDiscardedResultIsFlagged(t *testing.T) {
	count := tempDropCount(t, `
fn make() -> string {
    return "x";
}

fn f() -> nothing {
    make();
}
`)
	if count != 1 {
		t.Fatalf("expected the discarded result flagged, got %d", count)
	}
}

func TestConsumedValuesAreNotFlagged(t *testing.T) {
	// let RHS, return value, by-value arg: all consumed, nothing dangles.
	count := tempDropCount(t, `
fn make() -> string {
    return "x";
}

fn eat(s: string) -> nothing {
}

fn f() -> string {
    let a: string = make();
    eat(make());
    return make();
}
`)
	if count != 0 {
		t.Fatalf("expected no flags for consumed values, got %d", count)
	}
}

func TestControlFlowArmsTransferToOuterExpression(t *testing.T) {
	// The arm results transfer into the ternary's own value; the let
	// then consumes the outer expression — nothing dangles.
	count := tempDropCount(t, `
fn make() -> string {
    return "x";
}

fn f(c: bool) -> nothing {
    let s: string = c ? make() : make();
}
`)
	if count != 0 {
		t.Fatalf("expected arm transfers to suppress flags, got %d", count)
	}
}

func TestExplicitBorrowOperandEscapesUnflagged(t *testing.T) {
	// An explicit & can outlive the statement (here into a binding):
	// leak over dangle — the operand must not drop at statement end.
	count := tempDropCount(t, `
fn f() -> nothing {
    let r: &string = &"x";
}
`)
	if count != 0 {
		t.Fatalf("expected explicit borrow operand unflagged, got %d", count)
	}
}

func TestAggregatePositionsConsume(t *testing.T) {
	count := tempDropCount(t, `
type Pair = { a: string, b: string }

fn make() -> string {
    return "x";
}

fn f() -> nothing {
    let p: Pair = Pair { a: make(), b: make() };
    let arr: string[] = [make(), make()];
}
`)
	if count != 0 {
		t.Fatalf("expected aggregate positions to consume, got %d", count)
	}
}

func TestSuspensionTaintsStatement(t *testing.T) {
	// The STATEMENT containing the spawn flags nothing (temp lifetimes
	// across a suspension point belong to the crossing vertical), while
	// statements INSIDE the async body without their own suspension
	// execute atomically between suspends — their temps still reclaim.
	// The bare snippet harness lacks core's Task type, so only the flag
	// set is asserted (the taint fires on expression KIND, before types).
	_, _, res := runSemaOnSnippetResult(t, `
fn make() -> string {
    return "x";
}

fn f() -> nothing {
    let t = spawn async {
        let s: string = "a" + "b";
        return 1;
    };
}
`)
	if res == nil {
		t.Fatal("no sema result")
	}
	if len(res.TempDrops) != 2 {
		t.Fatalf("expected exactly the async-body concat operands flagged, got %v", res.TempDrops)
	}
}
