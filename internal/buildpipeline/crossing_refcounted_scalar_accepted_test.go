package buildpipeline

import (
	"testing"
)

// The other half of the crossing-capture table: the shapes that CROSS.
//
// It lives beside crossing_refcounted_scalar_test.go rather than in it because
// the two ask opposite questions of the same gate and each is long enough to
// read on its own. The refusals are there; what an accepted capture then owes
// the runtime -- the buffer walk, with the element stride it walks by -- is
// here.
//
// The captures whose counted blocks live inline: each is un-shared in the
// relinquishing operand and crosses. Every program keeps a sibling holder of
// the block alive on the source side (`a` is printed after the crossing), so
// what these rows admit is exactly the shape the stop-gap refused: the block
// has two holders at the boundary, and the operand's un-share is what makes
// the shipped one private. The count of those clones is asserted end to end
// in internal/vm (unshare_clones on the TRACE_RESIDENT exit line); here the
// rows pin that the gate no longer turns the shape away.
//
// Red on the tree before the narrowing: every row here was refused with
// SEM3168 (TestRefCountedScalarCrossingsAreRefused held them).
//
// A row whose NAME claims the buffer walk reads the emitted IR and demands
// it. Compiling cleanly says the gate let the shape through; it says nothing
// about what the relinquishing operand emits. Cut the emitter's dynamic-array
// leaf arm out and the walk body is empty, the module still builds, and a row
// that only compiled would call that green.
func TestRefCountedScalarCapturesCross(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
		walk *bufferWalk
	}{
		{
			name: "bare float captured by copy into an on body",
			src: `
async fn go(dst: Placement) -> int {
    let f: float = 1.5;
    let r: TaskResult<int> = on dst { let g: float = f; ret 1; };
    print(f to string);
    return 0;
}
`,
		},
		{
			name: "copy struct carrying a float field captured into an on body",
			src: `
@copy
type P = { v: float };

async fn go(dst: Placement) -> int {
    let p: P = P { v: 1.5 };
    let r: TaskResult<int> = on dst { let q: P = p; ret 1; };
    print(p.v to string);
    return 0;
}
`,
		},
		{
			name: "owned struct carrying a float field moved into an on body",
			src: `
@shard_movable
type P = { v: float };

async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let p: own P = own P{ v: a };
    let r: TaskResult<int> = on dst { let x: float = p.v; ret 1; };
    print(a to string);
    return 0;
}
`,
		},
		{
			name: "owned struct carrying a float field moved into a spawn on body",
			src: `
@shard_movable
type P = { v: float };

fn use(p: own P) -> int { return 1; }

async fn start(dst: Placement) -> far Task<int> {
    let a: float = 1.5;
    let p: own P = own P{ v: a };
    return spawn on dst { ret use(own p); };
}
`,
		},
		{
			name: "owned union carrying a float payload moved into an on body",
			src: `
@shard_movable
type P = { v: float };

tag Held(P);
tag Empty();
@shard_movable
type U = Held(P) | Empty();

fn use(u: own U) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let held: U = Held(P{ v: a });
    let u: own U = own held;
    let r: TaskResult<int> = on dst { ret use(own u); };
    print(a to string);
    return 0;
}
`,
		},
		// The moved local had its address handed to a child task first, so the
		// async split rewrites it into a RESIDENT field of the frame. The
		// un-share was emitted on the bare local before the split and the
		// post-split shape rule reads the resident field as that local. Found
		// by the refuter of 2026-09-06: refused with the validator's own text
		// ("reaches the boundary as Move L44.__resident$p$3") instead of
		// building.
		{
			name: "owned struct borrowed by a child task, then moved into a spawn on body",
			src: `
@shard_movable
type P = { v: float };

fn use(p: own P) -> int { return 1; }

async fn peek(p: &P) -> int { return 0; }

async fn run(dst: Placement) -> int {
    let a: float = 1.5;
    let p: own P = own P{ v: a };
    let t: Task<int> = spawn peek(&p);
    let seen: int = compare t.await() { Success(x) => x; Cancelled() => 0 - 2; };
    let task: far Task<int> = spawn on dst { ret use(own p); };
    let b: float = a;
    let r: TaskResult<int> = task.await();
    print(b to string);
    return seen;
}
`,
		},
		{
			name: "struct borrowed by a child task, then moved into a blocking body",
			src: `
type Q = { v: float };

fn sink(q: own Q) -> int { return 1; }

async fn peek(q: &Q) -> int { return 0; }

async fn run() -> int {
    let c: float = 3.5;
    let q: Q = Q{ v: c };
    let t: Task<int> = spawn peek(&q);
    let seen: int = compare t.await() { Success(x) => x; Cancelled() => 0 - 2; };
    let job: Task<int> = blocking { ret sink(own q); };
    let r: TaskResult<int> = job.await();
    print(c to string);
    return seen;
}
`,
		},
		{
			name: "struct carrying a float field moved into a blocking body",
			src: `
type P = { v: float };

fn sink(p: own P) -> int { return 1; }

async fn go() -> int {
    let a: float = 1.5;
    let p: P = P{ v: a };
    let job: Task<int> = blocking { ret sink(own p); };
    let r: TaskResult<int> = job.await();
    print(a to string);
    return 0;
}
`,
		},
		{
			name: "bare float captured into a blocking body",
			src: `
async fn go() -> int {
    let a: float = 1.5;
    let job: Task<int> = blocking { let b: float = a; ret 1; };
    let r: TaskResult<int> = job.await();
    print(a to string);
    return 0;
}
`,
		},
		// The `on` boundary, where the array was refused twice over: first for
		// the buffer no walk reached, then -- once the walk existed -- for a
		// `@shard_movable` marker `Array<T>` has nowhere to carry. It crosses
		// now because its ELEMENTS travel, and the row demands the walk itself
		// rather than a clean compile: the gate opening is not the same claim
		// as the operand making each element's block private.
		{
			name: "float array captured into an on body, its elements travelling with it",
			src: `
fn use(xs: own float[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let xs: float[] = [a];
    let r: TaskResult<int> = on dst { ret use(own xs); };
    print(a to string);
    return 0;
}
`,
			walk: &bufferWalk{stride: 8, elem: []string{"call ptr @rt_bigfloat_unshare("}},
		},
		// The same array through `spawn on`, which shares the capture gate with
		// `on` (both call checkOnCaptures) -- stated by the call graph, pinned
		// here.
		{
			name: "float array moved into a spawn on body",
			src: `
fn use(xs: own float[]) -> int { return 1; }

async fn start(dst: Placement) -> far Task<int> {
    let a: float = 1.5;
    let xs: float[] = [a];
    return spawn on dst { ret use(own xs); };
}
`,
			walk: &bufferWalk{stride: 8, elem: []string{"call ptr @rt_bigfloat_unshare("}},
		},
		// An `int[]` reaches the boundary with no counted block anywhere, so
		// there is no un-share to emit at all -- the handle simply moves. The
		// row is here because the gate that lets it through is the same one,
		// and a rule that only ever ran on `float[]` would not be the rule the
		// element question describes.
		{
			name: "int array moved into an on body, with no counted block to make private",
			src: `
fn use(xs: own int[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let xs: int[] = [1, 2, 3];
    let r: TaskResult<int> = on dst { ret use(own xs); };
    return 0;
}
`,
		},
		// The array's elements are counted blocks in a buffer the handle names,
		// one reference per element while `a` keeps its own; the runtime walks
		// that buffer in the relinquishing operand and makes each private.
		// Refused before the walk existed, with the buffer named as the reason.
		{
			name: "float array captured into a blocking body, its buffer walked element by element",
			src: `
fn use(xs: own float[]) -> int { return 1; }

async fn go() -> int {
    let a: float = 1.5;
    let xs: float[] = [a];
    let job: Task<int> = blocking { ret use(own xs); };
    let r: TaskResult<int> = job.await();
    print(a to string);
    return 0;
}
`,
			walk: &bufferWalk{stride: 8, elem: []string{"call ptr @rt_bigfloat_unshare("}},
		},
		// The array is built EMPTY, so no expression in the function names its
		// element union: `Option<float>` reaches the module through the array's
		// type alone. The walk switches on that union's tag per slot and needs
		// its membership published; sema admitted this shape while the emitter
		// refused it as a build error naming sema's predicate, until the
		// lowering looked through the handle to the payload it holds.
		{
			name: "array of optional floats built empty and captured into a blocking body, its element union read from the type alone",
			src: `
fn use(xs: own Option<float>[]) -> int { return 1; }

async fn go() -> int {
    let xs: Option<float>[] = [];
    let job: Task<int> = blocking { ret use(own xs); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
			walk: &bufferWalk{
				stride: 16,
				elem:   []string{"switch i32", "call ptr @rt_bigfloat_unshare("},
			},
		},
		// The element union's own arm holds a dynamic array, so the walk the
		// runtime is handed hands a second buffer back to it: the outer body
		// steps the union's 16-byte slots, and the union's body steps the
		// float's 8-byte ones. That nesting drains through the one worklist.
		{
			name: "array of a user union with a float array arm, built empty and captured into a blocking body",
			src: `
tag HeldArr(float[]);
tag HeldNone();
type U = HeldArr(float[]) | HeldNone();

fn use(xs: own U[]) -> int { return 1; }

async fn go() -> int {
    let xs: U[] = [];
    let job: Task<int> = blocking { ret use(own xs); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
			walk: &bufferWalk{
				stride: 16,
				elem: []string{
					"switch i32",
					"call void @rt_array_unshare_walk(",
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ir := compileCleanly(t, tc.src)
			if tc.walk != nil {
				requireBufferWalk(t, ir, *tc.walk)
			}
		})
	}
}

// The refusal must not spill onto fixed-width floats: `float64` is a machine word
// with no heap block and no count, and it has always crossed. If this breaks,
// the widening reached a type it was never meant to.
func TestFixedWidthFloatStillCrosses(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	compileCleanly(t, `
async fn go(dst: Placement) -> int {
    let f: float64 = 1.5;
    let r: TaskResult<float64> = on dst { let g: float64 = f; ret g; };
    return 0;
}
`)
}
