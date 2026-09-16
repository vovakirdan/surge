package sema

import "testing"

// An explicit drop ends one owner's incarnation. Every borrow the environment
// still holds OF THAT BINDING expires, wherever it is held — another binding, an
// external cell, or a container backing — and nothing else moves: a sibling
// owner's borrow, an incoming parameter's contents and the predecessor
// environment all stay as they were.
//
// The last two assertions are the incarnation rule: a later assignment mints a
// fresh, unexpired root for the same binding, and the expired one cannot be
// revived by it (compareReturnOrigins keeps them distinct).
func TestReturnOriginDropExpiresOnlyTheDroppedOwner(t *testing.T) {
	dropped := returnOrigin{kind: returnOriginLocal, binding: 9, scope: 2}
	sibling := returnOrigin{kind: returnOriginLocal, binding: 8, scope: 2}
	incoming := returnOrigin{kind: returnOriginParam, param: 0}
	expired := dropped
	expired.expired = true

	env := newReturnOriginEnv().
		assign(1, 1, returnOriginValueOf(dropped)).
		assign(2, 1, returnOriginValueOf(sibling, incoming))
	env = env.withCell(0, returnOriginValueOf(dropped))
	env.backings[0] = returnOriginValueOf(dropped)

	out := env.expireBinding(9)

	requireReturnOriginRoot(t, out.value(1), expired)
	if len(out.value(1).roots) != 1 {
		t.Fatalf("the dropped owner's borrow gained or lost roots: %+v", out.value(1))
	}
	requireReturnOriginRoot(t, out.cell(0), expired)
	requireReturnOriginRoot(t, out.backing(0), expired)
	if !out.value(2).equal(returnOriginValueOf(sibling, incoming)) {
		t.Fatalf("a binding that never named the dropped owner changed: %+v", out.value(2))
	}
	if !env.value(1).equal(returnOriginValueOf(dropped)) {
		t.Fatalf("expiring mutated its predecessor environment: %+v", env.value(1))
	}

	next := out.assign(3, 1, returnOriginValueOf(dropped))
	requireReturnOriginRoot(t, next.value(3), dropped)
	if next.value(3).equal(next.value(1)) {
		t.Fatal("a new owner incarnation revived the dropped borrow")
	}
}

// A drop of a binding nothing borrows leaves the environment untouched, and an
// invalid or unreachable input answers itself.
func TestReturnOriginDropWithoutBorrowsChangesNothing(t *testing.T) {
	sibling := returnOrigin{kind: returnOriginLocal, binding: 8, scope: 2}
	env := newReturnOriginEnv().assign(1, 1, returnOriginValueOf(sibling))

	if out := env.expireBinding(9); !out.equal(env) {
		t.Fatalf("dropping an unborrowed owner changed the environment: %+v", out)
	}
	if out := env.expireBinding(0); !out.equal(env) {
		t.Fatalf("an invalid owner changed the environment: %+v", out)
	}
	if out := (returnOriginEnv{}).expireBinding(9); out.reachable {
		t.Fatal("an unreachable environment gained a continuation")
	}
}
