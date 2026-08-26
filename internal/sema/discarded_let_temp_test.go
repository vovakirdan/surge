package sema

import "testing"

// `let _ = e` names nobody, so nobody owns what `e` produced: it is a discarded
// result exactly like the statement `e;`, and it is released at the end of
// its statement the same way. It used to be consumed as if a binding had
// received it -- observeMove treats every initializer as handed to its
// binding -- and then dropped by nobody, so every owning value discarded
// through `_` leaked (measured 2026-08-26: a map's `let _ = m.remove(&k)`
// leaked the removed value, 42 bytes per Owned).
func TestDiscardedLetIsFlagged(t *testing.T) {
	count := tempDropCount(t, `
fn make() -> string {
    return "x";
}

fn f() -> nothing {
    let _ = make();
}
`)
	if count != 1 {
		t.Fatalf("expected the discarded initializer flagged once, got %d", count)
	}
}

// A binding that has a name receives the value, and the value is the
// binding's to drop -- nothing about the discard rule may reach it.
func TestNamedLetIsNotFlagged(t *testing.T) {
	count := tempDropCount(t, `
fn make() -> string {
    return "x";
}

fn f() -> nothing {
    let v = make();
}
`)
	if count != 0 {
		t.Fatalf("expected a named binding's initializer unflagged, got %d", count)
	}
}

// Discarding a PLACE moves nothing: `let _ = x` leaves `x` where it is, owned
// and dropped by its own binding, and flags no temporary -- a place read is
// never a producer.
func TestDiscardedPlaceIsNeitherFlaggedNorMoved(t *testing.T) {
	count := tempDropCount(t, `
fn make() -> string {
    return "x";
}

fn take(s: string) -> nothing {
}

fn f() -> nothing {
    let x = make();
    let _ = x;
    take(x);
}
`)
	if count != 0 {
		t.Fatalf("expected no temporary for a discarded place, got %d", count)
	}
}
