package sema

import "testing"

// An owned capture MOVES across a crossing boundary. That was true of the
// lowering long before it was true of the language: the state took the value
// and the body unpacked it, but the caller's binding stayed live, so the caller
// could still read it, or move it a second time, with no diagnostic at all —
// and it was the caller's scope-exit drop that happened to reclaim the capture,
// which is why giving the body the drop on its own produced a double free.
//
// The runtime half (who actually releases the value) is pinned by valgrind in
// the crossing e2e tests; these rows pin the language half.
//
// Uses the placement-crossing prelude from on_crossing_test.go, so `Task`,
// `TaskResult`, `distributed` and the tag constructors resolve. Without it the
// snippets do not type at all and every assertion here passes vacuously — which
// is exactly what the first version of this file did.
func TestOwnedCrossingCaptureIsAMove(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// The caller reads what it gave away.
			name: "read_after_capture",
			src: `fn f() -> int {
	let j: own Movable = own Movable{ id: 4 };
	let t: far Task<int> = spawn on distributed { ret j.id; };
	let after: int = j.id;
	let got: TaskResult<int> = t.await();
	return after + compare got { Success(x) => x; Cancelled() => 0; };
}`,
		},
		{
			// One owned value, moved twice.
			name: "second_move_after_capture",
			src: `fn consume(j: own Movable) -> int { return j.id; }
fn f() -> int {
	let j: own Movable = own Movable{ id: 4 };
	let t: far Task<int> = spawn on distributed { ret j.id; };
	let x: int = consume(own j);
	let got: TaskResult<int> = t.await();
	return x + compare got { Success(v) => v; Cancelled() => 0; };
}`,
		},
		{
			// One owned value handed to TWO crossings.
			name: "captured_by_two_crossings",
			src: `fn f() -> int {
	let j: own Movable = own Movable{ id: 4 };
	let a: far Task<int> = spawn on distributed { ret j.id; };
	let b: far Task<int> = spawn on distributed { ret j.id; };
	let ra: TaskResult<int> = a.await();
	let rb: TaskResult<int> = b.await();
	return compare ra { Success(x) => x; Cancelled() => 0; }
	     + compare rb { Success(x) => x; Cancelled() => 0; };
}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codes := onCrossingCodes(t, tc.src)
			if !codes["SEM3130"] {
				t.Fatalf("expected a use-after-move diagnostic for a captured binding, got %v", codes)
			}
		})
	}
}

// The must-still-be-accepted side. An implementation that marked EVERY capture
// moved would pass every row above for entirely the wrong reason, so each legal
// shape has to stay legal — and each asserts that sema reports NOTHING, not
// merely that it withholds one code. Checking for the absence of a single code
// is how a control like this rots into a test that cannot fail.
func TestCrossingCaptureMoveKeepsLegalShapesLegal(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// A Copy scalar does not move: the caller keeps its binding.
			name: "copy_scalar_capture_still_usable",
			src: `fn f() -> int {
	let n: int = 4;
	let t: far Task<int> = spawn on distributed { ret n; };
	let after: int = n + 1;
	let got: TaskResult<int> = t.await();
	return after + compare got { Success(x) => x; Cancelled() => 0; };
}`,
		},
		{
			// A Copy COMPOSITE, not just a scalar. An implementation keyed off
			// "is this a composite" rather than "is this an owned move" would
			// mark this moved and reject the reuse.
			name: "copy_composite_capture_still_usable",
			src: `@copy
type Point = { x: int, y: int };
fn f() -> int {
	let p: Point = Point{ x: 4, y: 2 };
	let t: far Task<int> = spawn on distributed { ret p.x; };
	let after: int = p.x + p.y;
	let got: TaskResult<int> = t.await();
	return after + compare got { Success(v) => v; Cancelled() => 0; };
}`,
		},
		{
			// The ordinary shape: the body reads its owned capture and the
			// caller never touches it again.
			name: "owned_capture_untouched_after",
			src: `fn f() -> int {
	let j: own Movable = own Movable{ id: 4 };
	let t: far Task<int> = spawn on distributed { ret j.id; };
	let got: TaskResult<int> = t.await();
	return compare got { Success(x) => x; Cancelled() => 0; };
}`,
		},
		{
			// The anchored form USES the destination handle inside the body it
			// was captured for. Far-handle captures are deliberately outside
			// the move, and this is the row that fails if they are folded in:
			// every anchored channel operation would be rejected.
			name: "anchored_far_channel_still_usable",
			src: `fn f(ch: far Channel<int>) -> nothing {
	let _ = on ch { ch.send(1); ret nothing; };
	return nothing;
}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codes := onCrossingCodes(t, tc.src)
			if len(codes) != 0 {
				t.Fatalf("a legal capture shape was rejected: %v", codes)
			}
		})
	}
}

// DEBT SENTINEL, not a semantic contract: this asserts behaviour that is WRONG,
// and exists only so the gap stays recorded and announces itself when it
// closes. Delete it — do not "fix" it — once Epic 24 steps 3 and 5 land.
//
// A field WRITE after a move is not caught. `j.id = 99` after the capture is
// accepted, and the two sides then disagree about one value: measured at caller
// 99 against body 406 across a shard boundary.
//
// It is not crossing-specific, which is why step 0b did not fix it: a field
// write after ANY move is accepted, including `consume(own j); j.id = 99;` with
// no crossing in sight. The moved-set is symbol-keyed and the write goes
// through a projection, so nothing consults it.
func TestCrossingCaptureFieldWriteIsNotYetCaught(t *testing.T) {
	codes := onCrossingCodes(t, `fn f() -> int {
	let mut j: own Movable = own Movable{ id: 4 };
	let t: far Task<int> = spawn on distributed { ret j.id; };
	j.id = 99;
	let got: TaskResult<int> = t.await();
	return compare got { Success(x) => x; Cancelled() => 0; };
}`)
	if codes["SEM3130"] {
		t.Fatalf("a field write after a capture is now diagnosed — good news, but this test "+
			"asserts the old gap and must be DELETED rather than fixed: %v", codes)
	}
}
