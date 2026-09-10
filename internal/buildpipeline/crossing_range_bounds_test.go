package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The crossing gate, asked about a `Range<T>`.
//
// A range is a runtime-owned handle, and every other handle whose payload may
// share a counted block is refused: a map's table and a channel's ring are
// storage the origin shard keeps and the handle merely names, so no walk can
// reach the blocks inside them. A range looked like one of those and was
// refused with their sentence -- "a map's table, a channel's ring" -- which was
// evidence enough on its own that the shape had never been considered.
//
// What makes it different is that its payload is TWO WORDS AT A FIXED OFFSET
// inside one object. The runtime can step them, and now does: the object
// carries a byte saying which of the three arbitrary-precision scalars its
// bounds are, so rt_range_unshare can pick an unshare, exactly as rt_range_free
// picks a release. That is the whole of why this shape crosses and a map's
// table still does not.
//
// Red on the tree before this lane: every accepted row here was refused with
// SEM3168, and the refused rows below were refused for the same reason as each
// other, which is what made the refusal look right.

// TestRangeWithCountedBoundsCrosses is the accepting half.
//
// Compiling cleanly is not the claim. The gate opening and the operand actually
// making the bounds private are two different facts, and a row that only
// compiled would call a missing walk green -- so each accepted row reads the
// emitted module and demands the call.
func TestRangeWithCountedBoundsCrosses(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name    string
		src     string
		unshare bool
	}{
		{
			// The subject. `float` is the one counted scalar today, so a
			// `Range<float>` is the one range whose bounds a sibling holder can
			// share -- `a` is still live on this side when the job is submitted.
			name: "range of floats captured into a blocking body, its bounds made private",
			src: `
async fn go() -> int {
    let a: float = 1.5;
    let r: Range<float> = a..2.5;
    let job: Task<int> = blocking { let s: Range<float> = r; ret 1; };
    let x: TaskResult<int> = job.await();
    print(a to string);
    return 0;
}
`,
			unshare: true,
		},
		{
			// A range inside a struct. The walk reaches it through the member
			// enumeration rather than at the top, which is the arm that would
			// still be missing if the predicate alone had been widened.
			name: "struct holding a range of floats captured into a blocking body",
			src: `
type Span = { r: Range<float> };

fn use(s: own Span) -> int { return 1; }

async fn go() -> int {
    let a: float = 1.5;
    let s: Span = Span { r: a..2.5 };
    let job: Task<int> = blocking { ret use(own s); };
    let x: TaskResult<int> = job.await();
    print(a to string);
    return 0;
}
`,
			unshare: true,
		},
		{
			// `Range<int>` carries no counted block on this tree, so nothing is
			// armed for it and it crosses as it always did. The row is here so
			// that the day `int` becomes a counted scalar, the first row above
			// keeps holding and this one moves rather than silently changing
			// meaning.
			name: "range of ints crosses with nothing to make private",
			src: `
async fn go() -> int {
    let r: Range<int> = 1..4;
    let job: Task<int> = blocking { let s: Range<int> = r; ret 1; };
    let x: TaskResult<int> = job.await();
    return 0;
}
`,
			unshare: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ir := compileCleanly(t, tc.src)
			got := strings.Contains(ir, "call void @rt_range_unshare(ptr ")
			if got != tc.unshare {
				t.Fatalf("the module calls rt_range_unshare = %v, want %v "+
					"(a declaration alone is not a walk):\n%s", got, tc.unshare, ir)
			}
		})
	}
}

// TestRangeBehindAHandleStaysRefused is the other half, and it is what makes
// the lifting narrow rather than general.
//
// The reason a range crosses is that the runtime can be handed the SLOT holding
// its handle. Put that handle inside a map's table, a channel's ring or a task's
// result slot and there is no slot to hand over: the storage stays on the
// origin shard and nothing can reach the range inside it, let alone its bounds.
// So these are refused for exactly the reason the bare range no longer is, and
// a lifting done by deleting the handle arm instead of preceding it would let
// every one of them through.
//
// The last two rows are a different refusal on a different axis, kept here so
// the lane's reach reads as one list: `on` wants a `@shard_movable` marker, and
// no range of any bound type has ever carried one.
func TestRangeBehindAHandleStaysRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "map valued by a range of floats captured into a blocking body",
			src: `
fn use(m: own Map<int, Range<float>>) -> int { return 1; }

async fn go() -> int {
    let m: Map<int, Range<float>> = {};
    let job: Task<int> = blocking { ret use(own m); };
    let x: TaskResult<int> = job.await();
    return 0;
}

@entrypoint
fn main() -> int { return 0; }
`,
			contains: []string{"a map's table, a channel's ring"},
		},
		{
			name: "channel of ranges of floats captured into a blocking body",
			src: `
fn use(c: own Channel<Range<float>>) -> int { return 1; }

async fn go() -> int {
    let c: Channel<Range<float>> = Channel::<Range<float>>::new(4:uint);
    let job: Task<int> = blocking { ret use(own c); };
    let x: TaskResult<int> = job.await();
    return 0;
}

@entrypoint
fn main() -> int { return 0; }
`,
			contains: []string{"a map's table, a channel's ring"},
		},
		{
			// The `on` boundary asks a SECOND question this lane does not
			// answer, and the row is here so the lane's reach is written down
			// rather than assumed: `on` requires the captured type to be marked
			// `@shard_movable`, and `Range<T>` is a core runtime handle with
			// nowhere to carry that marker. `Range<int>` -- which shares no
			// counted block at all and passes every gate this lane touched --
			// is refused here too, which is what says the refusal is the
			// marker's and not the bounds'.
			name: "range of ints captured into an on body is refused for the marker, not for its bounds",
			src: `
async fn go(dst: Placement) -> int {
    let r: Range<int> = 1..4;
    let x: TaskResult<int> = on dst { let s: Range<int> = r; ret 1; };
    return 0;
}

@entrypoint
fn main() -> int { return 0; }
`,
			contains: []string{"is not shard-movable"},
		},
		{
			name: "range of floats captured into an on body is refused for the same marker",
			src: `
async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let r: Range<float> = a..2.5;
    let x: TaskResult<int> = on dst { let s: Range<float> = r; ret 1; };
    print(a to string);
    return 0;
}

@entrypoint
fn main() -> int { return 0; }
`,
			contains: []string{"is not shard-movable"},
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
