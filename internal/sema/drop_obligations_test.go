package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
)

// runSemaOnSnippetResult is runSemaOnSnippet plus the sema Result, so
// obligation-map rows can assert what the walker recorded.
func runSemaOnSnippetResult(t *testing.T, src string) (*diag.Bag, *diag.Bag, *Result) {
	t.Helper()
	builder, fileID, parseBag := parseSnippet(t, src)
	semaBag := diag.NewBag(32)
	if parseBag.Len() != 0 {
		return parseBag, semaBag, nil
	}
	symRes := symbols.ResolveFile(builder, fileID, &symbols.ResolveOptions{
		Reporter: &diag.BagReporter{Bag: semaBag},
	})
	res := Check(context.Background(), builder, fileID, Options{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Symbols:  &symRes,
	})
	return parseBag, semaBag, &res
}

func requireNoSemaErrors(t *testing.T, parseBag, semaBag *diag.Bag) {
	t.Helper()
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
}

func TestPartialPathMoveOnIfWithoutElseSynthesizesDrop(t *testing.T) {
	// Per-arm drop synthesis: a value moved only in the then-branch is
	// freed on the fall-through via a synthesized else — accepted, not
	// rejected (the friendly resolution of a partial-path move).
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn eat(s: string) -> nothing {
}

fn ok(c: bool) -> nothing {
    let s: string = "gone";
    if c {
        eat(s);
    }
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	if len(res.IfSyntheticElseDrops) == 0 {
		t.Fatalf("expected a synthesized-else drop for the fall-through, got %v", res.IfSyntheticElseDrops)
	}
}

func TestPartialPathMoveOnUnevenElseSynthesizesArmDrop(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn eat(s: string) -> nothing {
}

fn ok(c: bool) -> nothing {
    let s: string = "gone";
    if c {
        eat(s);
    } else {
        let n: int = 1;
    }
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	if len(res.ArmDropsStmt) == 0 {
		t.Fatalf("expected an arm-drop recorded for the else branch, got %v", res.ArmDropsStmt)
	}
}

func TestPartialPathMoveThenUseAfterMergeStillErrors(t *testing.T) {
	// The correctness boundary: per-arm drops free a value only when it
	// is NOT read after the join; reading it stays a use-of-moved error.
	parseBag, semaBag := runSemaOnSnippet(t, `
fn eat(s: string) -> nothing {
}

fn read(s: &string) -> int {
    return s.__len() to int;
}

fn bad(c: bool) -> int {
    let s: string = "gone";
    if c {
        eat(s);
    }
    return read(&s);
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("expected %v for use after a partial-path move, got %s", diag.SemaUseAfterMove, diagnosticsSummary(semaBag))
	}
}

func TestBothBranchMovesAccepted(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn eat(s: string) -> nothing {
}

fn ok(c: bool) -> nothing {
    let s: string = "gone";
    if c {
        eat(s);
    } else {
        eat(s);
    }
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
}

func TestEarlyExitBranchMoveAccepted(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn eat(s: string) -> nothing {
}

fn ok(c: bool) -> int {
    let s: string = "gone";
    if c {
        eat(s);
        return 1;
    }
    eat(s);
    return 0;
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
}

func TestBranchLocalMoveDoesNotTripJoin(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn eat(s: string) -> nothing {
}

fn ok(c: bool) -> nothing {
    if c {
        let s: string = "local";
        eat(s);
    }
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
}

func TestLoopBackEdgeMoveRejected(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn eat(s: string) -> nothing {
}

fn bad(n: int) -> nothing {
    let s: string = "gone";
    let mut i: int = n;
    while i > 0 {
        eat(s);
        i = i - 1;
    }
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("expected %v diagnostic, got %s", diag.SemaUseAfterMove, diagnosticsSummary(semaBag))
	}
}

func TestLoopBodyLocalMoveAccepted(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
fn eat(s: string) -> nothing {
}

fn ok(n: int) -> nothing {
    let mut i: int = n;
    while i > 0 {
        let s: string = "fresh";
        eat(s);
        i = i - 1;
    }
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
}

func TestScopeEndDropsRecordLiveBindingsAndParams(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn eat(s: string) -> nothing {
}

fn f(p: string) -> nothing {
    let a: string = "a";
    let b: string = "b";
    eat(b);
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	// One block records drops for f's body: live local `a` then param
	// `p`; the moved `b` and the callee-side `s` accounting differ.
	found := false
	for _, drops := range res.ScopeEndDrops {
		if len(drops) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a scope-end record with [a, p], got %v", res.ScopeEndDrops)
	}
}

func TestReturnRecordsEarlyExitDrops(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn eat2(s: string) -> nothing {
}

fn f(c: bool) -> int {
    let s: string = "held";
    if c {
        return 1;
    }
    eat2(s);
    return 0;
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	if len(res.EarlyExitDrops) == 0 {
		t.Fatalf("expected early-exit drops for the return inside the if, got %v", res.EarlyExitDrops)
	}
	found := false
	for _, drops := range res.EarlyExitDrops {
		if len(drops) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a return carrying exactly the live 's', got %v", res.EarlyExitDrops)
	}
}

func TestReturnedBindingIsNotDroppedAtReturn(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn f() -> string {
    let s: string = "escapes";
    return s;
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	for _, drops := range res.EarlyExitDrops {
		if len(drops) != 0 {
			t.Fatalf("returned binding must not appear in exit drops, got %v", res.EarlyExitDrops)
		}
	}
	for _, drops := range res.ScopeEndDrops {
		if len(drops) != 0 {
			t.Fatalf("returned binding must not appear in scope-end drops, got %v", res.ScopeEndDrops)
		}
	}
}

func TestReassignRecordsOldDropAndSuppressesOnSelfMove(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn wrap(s: string) -> string {
    return s;
}

fn f() -> nothing {
    let mut a: string = "old";
    a = "new";
    let mut b: string = "old";
    b = wrap(b);
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	// `a = "new"` records the old-value drop; `b = wrap(b)` moved b
	// into the RHS, so its old-drop is suppressed: exactly one record.
	if len(res.ReassignOldDrops) != 1 {
		t.Fatalf("expected exactly one reassign old-drop record, got %v", res.ReassignOldDrops)
	}
}

func TestBreakAndContinueRecordLoopScopedDrops(t *testing.T) {
	parseBag, semaBag, res := runSemaOnSnippetResult(t, `
fn eat3(s: string) -> nothing {
}

fn f(n: int) -> nothing {
    let outer: string = "outer";
    let mut i: int = n;
    while i > 0 {
        let inner: string = "inner";
        if i == 1 {
            break;
        }
        if i == 2 {
            continue;
        }
        i = i - 1;
    }
    eat3(outer);
}
`)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil {
		t.Fatal("no sema result")
	}
	// break and continue each record exactly the loop-body-scoped
	// `inner` — never the outer binding.
	count := 0
	for _, drops := range res.EarlyExitDrops {
		if len(drops) != 1 {
			t.Fatalf("break/continue must carry exactly the body-local, got %v", res.EarlyExitDrops)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("expected records for break and continue, got %d", count)
	}
}
