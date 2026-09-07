package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// An arbitrary-precision scalar is Copy, so copying its bits duplicates a
// reference into a counted heap block without touching the count — and the
// count is deliberately NON-ATOMIC, which is only sound while a block stays on
// one shard. The barrier is an un-share in the relinquishing operand: before a
// value crosses, the compiler makes every counted block it carries private on
// the source thread.
//
// The rows split by what that walk can reach. A CAPTURE whose counted blocks
// live inline -- a scalar, a struct, a tuple, a fixed array, a union of those
// -- is un-shared in the operand and crosses; those shapes are in
// TestRefCountedScalarCapturesCross below, and the e2e row that counts the
// clones is the gate (internal/vm, unshare_clones). A shape whose blocks the
// walk cannot reach stays refused with a message that says so: a dynamic
// array's buffer and a channel's ring are both storage this shard keeps and
// the handle merely names. The channel ELEMENT is asked the same question at
// the channel's creation (crossing_refcounted_scalar_channel_test.go); the
// reply is refused on its own gate, untouched here (step 5, S2).
//
// The one shape that is NOT here, on purpose: fixed-width `float64`, a machine
// word with no block behind it (TestFixedWidthFloatStillCrosses).
func TestRefCountedScalarCrossingsAreRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "float riding the reply",
			src: `
async fn go(dst: Placement) -> int {
    let t: far Task<float> = spawn on dst { ret 1.5; };
    let r: TaskResult<float> = t.await();
    return 0;
}
`,
			contains: []string{"`float`", "cannot cross a shard boundary yet"},
		},
		// A dynamic array's elements are counted blocks in a buffer the handle
		// names and this shard keeps; an owned move hands over one reference
		// per element while every pusher keeps its own, and the relinquishing
		// walk has no buffer walk to make them private. Refused for that
		// reason, in those words.
		{
			name: "owned float array moved into an on body",
			src: `
fn use(xs: own float[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let xs: own float[] = own [a];
    let r: TaskResult<int> = on dst { ret use(own xs); };
    print(a to string);
    return 0;
}
`,
			contains: []string{"`Array<float>`", "cannot be made private"},
		},
		{
			name: "float array captured into a blocking body",
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
			contains: []string{"`[float]`", "cannot be made private"},
		},
		// A LOCAL channel handle captured by Copy into a crossing body. Runtime
		// handles are skipped by ContainsRefCountedScalar and were skipped by the
		// stop-gap too, so a `Channel<float>` rode into the body as plain bits;
		// the body's local `ch.send(f)` then retained f's block into a ring the
		// creator's shard owns, and the creator's `recv` held that block on one
		// thread while the body dropped `f` on another -- a non-atomic count
		// under two threads with no float captured at all. Same panel. The
		// walk cannot reach the ring through the handle, so this stays refused.
		{
			name: "local channel of floats captured into a crossing body",
			src: `
async fn go(dst: Placement) -> int {
    let ch: Channel<float> = Channel::<float>::new(4:uint);
    let r: TaskResult<int> = on dst { let f: float = 1.5; ch.send(f); let g: float = f; ret 0; };
    return 0;
}
`,
			contains: []string{"`Channel<float>`", "cannot be made private"},
		},
		// A far select whose two SEND arms are fed by ONE owned binding, so the
		// runtime would stage the same block into two cells: the reviewers'
		// counterexample of 06.09, refused by sema since 2026-09-07
		// (SemaSelectSendPayloadGivenTwice, RV2-DEBT-338). That is the refusal
		// that survived the element gate's narrowing -- the gate never looked at
		// the two arms, which is how the same program with a `string` element
		// built and double-freed. The channel-element rows that flipped when the
		// gate narrowed live in crossing_refcounted_scalar_channel_test.go.
		{
			name: "one owned union fed to two far-select send arms",
			src: `
type P = { v: float };

tag Held(P);
tag Empty();
type U = Held(P) | Empty();

async fn go() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    let ch2: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    let a: float = 2.5;
    let held: U = Held(P{ v: a });
    let won: int = select {
        ch.send(own held) => 1;
        ch2.send(own held) => 2;
    };
    print(a to string);
    return won;
}
`,
			contains: []string{"'held' is given away by two arms"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.sg")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}
			res, _ := Compile(context.Background(), &CompileRequest{
				TargetPath: path, Backend: BackendLLVM, MaxDiagnostics: 200,
			})
			if res.Diagnose == nil || res.Diagnose.Bag == nil {
				t.Fatal("missing diagnostics bag")
			}
			var message string
			for _, item := range res.Diagnose.Bag.Items() {
				for _, want := range tc.contains {
					if !strings.Contains(item.Message, want) {
						goto next
					}
				}
				message = item.Message
			next:
			}
			if message == "" {
				t.Fatalf("no diagnostic mentioning %v; got %s",
					tc.contains, summarizeCodes(res.Diagnose.Bag.Items()))
			}
		})
	}
}

// compileCleanly builds one program through the LLVM pipeline, all the way
// to the MIR validators, and fails on any error: a source that fails for an
// unrelated reason would satisfy a check for one code while proving nothing.
// The program is given an entrypoint so the pipeline runs past sema.
func compileCleanly(t *testing.T, src string) {
	t.Helper()
	src += "\n@entrypoint\nfn main() -> int { return 0; }\n"
	path := filepath.Join(t.TempDir(), "main.sg")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	res, err := Compile(context.Background(), &CompileRequest{
		TargetPath: path, Backend: BackendLLVM, MaxDiagnostics: 200,
	})
	if res.Diagnose == nil || res.Diagnose.Bag == nil {
		t.Fatalf("missing diagnostics bag (err=%v)", err)
	}
	if found := findDiagnostic(res.Diagnose.Bag.Items(), diag.FutCrossingPayloadNotShippable); found != nil {
		t.Fatalf("crossing was refused: %s", found.Message)
	}
	for _, item := range res.Diagnose.Bag.Items() {
		if item.Severity == diag.SevError {
			t.Fatalf("did not compile cleanly: [%s] %s", item.Code, item.Message)
		}
	}
	// The MIR validators report through the error, not the bag: a capture
	// that reached its boundary without an un-share is refused there.
	if err != nil {
		t.Fatalf("did not compile cleanly: %v", err)
	}
}

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
func TestRefCountedScalarCapturesCross(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { compileCleanly(t, tc.src) })
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
