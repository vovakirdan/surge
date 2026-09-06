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
// the source thread. Until the refusals are lifted, every path that would hand
// a second shard the same word is refused.
//
// These rows pin that closure so reopening it is a deliberate act rather than a
// silent regression. They split in two when the refusals are lifted: a shape
// whose blocks CAN be made private (a scalar, a struct, a tuple, a fixed array,
// a union of those) flips to "compiles", with the un-share clone count as the
// gate; a shape whose blocks cannot -- a dynamic array's buffer, a channel's
// ring, both reachable by their handle from the source -- stays refused with a
// message that says so.
//
// The owned `@shard_movable` MOVE used to be left out on the argument that a
// move transfers the references instead of sharing them. It transfers ONE
// reference — the value's own — and a sibling holder on the source shard keeps
// the block alive, so those rows are here too now. The one shape that is NOT
// here, on purpose: fixed-width `float64`, a machine word with no block behind
// it (TestFixedWidthFloatStillCrosses).
func TestRefCountedScalarCrossingsAreRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "bare float captured into a crossing body",
			src: `
async fn go(dst: Placement) -> int {
    let f: float = 1.5;
    let r: TaskResult<int> = on dst { let g: float = f; ret 1; };
    print(f to string);
    return 0;
}
`,
			contains: []string{"`float`", "counted heap block", "not", "safe to share"},
		},
		{
			name: "copy struct carrying a float field",
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
			contains: []string{"`P`", "arbitrary-precision"},
		},
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
		{
			name: "remote channel with a float element",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    return 0;
}
`,
			contains: []string{"remote channel cannot carry `float`", "sender's copy alive"},
		},
		// The owned-move rows. An owned `@shard_movable` value used to be exempt
		// on the argument that a move transfers the reference instead of sharing
		// it — true only while the block has exactly one holder. `own P{ v: a }`
		// retains `a`'s block into the field, so after the move the destination
		// shard holds a block the source shard still holds through `a`, and the
		// non-atomic count is raced from two threads. Refused until the operand
		// makes its counted leaves private (Epic 22 step 4).
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
			contains: []string{"`P`", "arbitrary-precision", "moving it"},
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
			contains: []string{"`P`", "arbitrary-precision", "moving it"},
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
			contains: []string{"`U`", "arbitrary-precision", "moving it"},
		},
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
			contains: []string{"arbitrary-precision", "moving it"},
		},
		// A far channel whose element is a union carrying a float. The element
		// gate asked ContainsRefCountedScalar, which stops at unions on purpose,
		// so `far Channel<Held(P) | Empty()>` was created, and a `send(own held)`
		// then moved a retained float block to the owner shard while the
		// sender's `a` kept holding it. Found by the G1 planning panel 06.09.
		{
			name: "remote channel with a union element carrying a float",
			src: `
type P = { v: float };

tag Held(P);
tag Empty();
type U = Held(P) | Empty();

async fn go() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    return 0;
}
`,
			contains: []string{"remote channel cannot carry `U`"},
		},
		// A LOCAL channel handle captured by Copy into a crossing body. Runtime
		// handles are skipped by ContainsRefCountedScalar and were skipped by the
		// stop-gap too, so a `Channel<float>` rode into the body as plain bits;
		// the body's local `ch.send(f)` then retained f's block into a ring the
		// creator's shard owns, and the creator's `recv` held that block on one
		// thread while the body dropped `f` on another -- a non-atomic count
		// under two threads with no float captured at all. Same panel.
		{
			name: "local channel of floats captured into a crossing body",
			src: `
async fn go(dst: Placement) -> int {
    let ch: Channel<float> = Channel::<float>::new(4:uint);
    let r: TaskResult<int> = on dst { let f: float = 1.5; ch.send(f); let g: float = f; ret 0; };
    return 0;
}
`,
			contains: []string{"`Channel<float>`", "arbitrary-precision"},
		},
		// A fixed array of floats is a nominal struct with no declared fields,
		// so the walk that asks a struct's members answered "nothing inside"
		// and `float[4]` shipped as plain bits -- four counted references in a
		// Copy value. Found by the G1 reviewers 06.09; red on the tree before
		// the ArrayFixedInfo arm (the program compiled).
		{
			name: "remote channel with a fixed float array element",
			src: `
async fn go() -> int {
    let ch: far Channel<float[4]> = channel_on::<float[4]>(shard(0:ShardId), 4);
    return 0;
}
`,
			contains: []string{"remote channel cannot carry", "arbitrary-precision"},
		},
		// Two shapes the first attempt at NARROWING the element gate admitted
		// and the reviewers caught (06.09): a `@copy` union carrying a float --
		// its anchored `send` hands the ring the union's bits with no retain --
		// and a far select whose two SEND arms are fed by ONE owned binding, so
		// the runtime stages the same block into two cells. Both are refused
		// on this tree; these rows pin that so a later narrowing cannot reopen
		// them silently.
		{
			name: "remote channel with a copy union element carrying a float",
			src: `
@copy
type P = { v: float };

tag Held(P);
tag Empty();
@copy
type U = Held(P) | Empty();

async fn go() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    return 0;
}
`,
			contains: []string{"remote channel cannot carry `U`"},
		},
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
			contains: []string{"remote channel cannot carry `U`"},
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
			contains: []string{"`P`", "arbitrary-precision", "moving it"},
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

// The refusal must not spill onto fixed-width floats: `float64` is a machine word
// with no heap block and no count, and it has always crossed. If this breaks,
// the widening reached a type it was never meant to.
func TestFixedWidthFloatStillCrosses(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	src := `
async fn go(dst: Placement) -> int {
    let f: float64 = 1.5;
    let r: TaskResult<float64> = on dst { let g: float64 = f; ret g; };
    return 0;
}
`
	path := filepath.Join(t.TempDir(), "main.sg")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	res, _ := Compile(context.Background(), &CompileRequest{
		TargetPath: path, Backend: BackendLLVM, MaxDiagnostics: 200,
	})
	if res.Diagnose == nil || res.Diagnose.Bag == nil {
		t.Fatal("missing diagnostics bag")
	}
	if found := findDiagnostic(res.Diagnose.Bag.Items(), diag.FutCrossingPayloadNotShippable); found != nil {
		t.Fatalf("float64 crossing was refused: %s", found.Message)
	}
	// Assert the program is CLEAN, not merely free of this one code: a source
	// that fails to compile for an unrelated reason would satisfy the check
	// above while proving nothing.
	for _, item := range res.Diagnose.Bag.Items() {
		if item.Severity == diag.SevError {
			t.Fatalf("float64 crossing did not compile cleanly: [%s] %s", item.Code, item.Message)
		}
	}
}
