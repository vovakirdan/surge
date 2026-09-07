package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/backend/llvm"
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
// -- is un-shared in the operand and crosses, and so is a dynamic array: the
// runtime walks its buffer element by element in the same operand. Those
// shapes are in TestRefCountedScalarCapturesCross below, and the e2e row that
// counts the clones is the gate (internal/vm, unshare_clones). A shape whose
// blocks no walk reaches stays refused with a message that says so: a map's
// table and a channel's ring are storage this shard keeps and the handle
// merely names. The channel ELEMENT is asked the same question at the
// channel's creation (crossing_refcounted_scalar_channel_test.go), and the
// crossing RESULT at its reply (crossing_refcounted_scalar_reply_test.go).
//
// A crossing INTO an `on` body is asked a second question, and the order
// between them is what several rows here exist to hold. For a dynamic ARRAY the
// ELEMENT answers first -- `Channel<float>[]`, `Channel<int>[]`, an array of an
// unmarked struct, an array of a `@nosend` type -- and the message names the
// element, because `Array<T>` is a spelling and carries no marker for anyone to
// add. For everything else the counted block answers: a bare `Channel<float>`
// and a `Map<int, float>` keep the ring / table words.
//
// That order is a correction, and the reason is that the counted-block wording
// carries a way OUT the array cannot take. Asked first it told the reader of a
// `Channel<float>[]` to "use a fixed-width type (`float64`) for the values it
// holds", and `Channel<float64>[]` is refused all over again -- what refuses an
// array of channels is that NO array of channels crosses, whatever the payload.
// The element is the declaration a reader can change, so the element answers.
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
		// An array of float CHANNELS answers TWO questions and must answer the
		// element one. Its ring holds counted values, so the counted-block arm
		// has something true to say about it -- but its way out, "use `float64`
		// for the values it holds", produces `Channel<float64>[]`, which this
		// gate refuses again. Move the element arm back behind the counted-block
		// arm and this row goes red on the ring wording.
		{
			name: "array of float channels captured into an on body is refused for its element, not for the ring",
			src: `
fn use(chs: own Channel<float>[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let chs: Channel<float>[] = [];
    let r: TaskResult<int> = on dst { ret use(own chs); };
    return 0;
}
`,
			contains: []string{"`Array<Channel<float>>`", "`Channel<float>`", "may not move between shards"},
		},
		// The way out the ring wording used to offer, taken. It has to be
		// refused too, or the reordering above would have been a matter of taste
		// rather than of the reader reaching a fix.
		{
			name: "array of float64 channels is refused for its element as well, so the ring's way out was no way out",
			src: `
fn use(chs: own Channel<float64>[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let chs: Channel<float64>[] = [];
    let r: TaskResult<int> = on dst { ret use(own chs); };
    return 0;
}
`,
			contains: []string{"`Array<Channel<float64>>`", "`Channel<float64>`", "may not move between shards"},
		},
		// A bare channel is NOT an array, so the counted-block arm still answers
		// for it, in the ring's words. The contrast with the two rows above is
		// the whole point of the ordering: where the reader can act on the
		// element, the element speaks; where the value IS the handle whose ring
		// no walk enters, the ring does.
		{
			name: "an owned float channel captured into an on body keeps the ring wording",
			src: `
fn use(ch: own Channel<float>) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let ch: Channel<float> = Channel::<float>::new(4:uint);
    let r: TaskResult<int> = on dst { ret use(own ch); };
    return 0;
}
`,
			contains: []string{"`Channel<float>`", "channel's ring", "cannot be made private"},
		},
		// An array whose element cannot travel is refused HERE, and names the
		// element. `Channel<int>` holds no counted scalar at all, so nothing
		// above this arm has anything to say about it; before the element rule
		// it fell to the unmarked-owned-value default and was told to mark
		// `Array<Channel<int>>` -- a spelling nobody can mark.
		{
			name: "array of int channels captured into an on body names the element it cannot move",
			src: `
fn use(chs: own Channel<int>[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let chs: Channel<int>[] = [];
    let r: TaskResult<int> = on dst { ret use(own chs); };
    return 0;
}
`,
			contains: []string{"`Array<Channel<int>>`", "`Channel<int>`", "may not move between shards"},
		},
		// The unmarked-owned-value rule keeps its MEANING for arrays: an
		// element nobody marked still cannot travel, and the refusal now points
		// at the type whose declaration the reader can actually change.
		{
			name: "array of an unmarked user struct captured into an on body",
			src: `
type P = { id: int };

fn use(ps: own P[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let ps: P[] = [];
    let r: TaskResult<int> = on dst { ret use(own ps); };
    return 0;
}
`,
			contains: []string{"`Array<P>`", "`P`", "may not move between shards"},
		},
		{
			name: "array of a nosend type captured into an on body",
			src: `
@nosend
type L = { id: int };

fn use(ls: own L[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let ls: L[] = [];
    let r: TaskResult<int> = on dst { ret use(own ls); };
    return 0;
}
`,
			contains: []string{"`Array<L>`", "`L`", "may not move between shards"},
		},
		// A map keyed or valued by a counted scalar shares like an array does,
		// and its table has no per-element walk: refused for that reason, in
		// those words.
		{
			name: "map keyed by int and valued by float moved into an on body",
			src: `
fn use(m: own Map<int, float>) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let m: Map<int, float> = Map::<int, float>.new();
    let r: TaskResult<int> = on dst { ret use(own m); };
    return 0;
}
`,
			contains: []string{"`Map<int, float>`", "map's table", "cannot be made private"},
		},
		// An array answers for its element: the buffer walk reaches each
		// channel handle, but nothing reaches the ring behind it.
		{
			name: "array of float channels captured into a blocking body",
			src: `
fn use(chs: own Channel<float>[]) -> int { return 1; }

async fn go() -> int {
    let chs: Channel<float>[] = [];
    let job: Task<int> = blocking { ret use(own chs); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
			contains: []string{"cannot be captured into `blocking`", "channel's ring"},
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
// through the MIR validators and the LLVM emitter, and fails on any error: a
// source that fails for an unrelated reason would satisfy a check for one
// code while proving nothing. The program is given an entrypoint so the
// pipeline runs past sema. It returns the emitted IR so a row can pin what
// the module names.
func compileCleanly(t *testing.T, src string) string {
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
	// The emitter is the last judge: a shape sema admits and the walk cannot
	// serve is a build failure there, and a row that stopped at MIR would
	// call it green.
	ir, emitErr := llvm.EmitModule(res.MIR, res.Diagnose.Sema.TypeInterner, res.Diagnose.Symbols.Table, res.Diagnose.FileSet)
	if emitErr != nil {
		t.Fatalf("did not emit cleanly: %v", emitErr)
	}
	return ir
}
