package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A dynamic array crosses into an `on` body when its elements may travel
// (ON-CAP-V005). A VIEW answers that element question the same way and is still
// the wrong value to move: it is a window onto ANOTHER array's buffer, the array
// it windows stays on the origin shard, and the destination shard would write
// through the window into memory the origin still owns, reads and frees.
//
// The runtime says exactly that, by name, from `rt_array_unshare_walk`, which
// every crossing of a dynamic array reaches whatever its element type. THAT is
// the guarantee, and these rows do not carry it: a view none of them names still
// stops the program with a VM1003 panic.
//
// What these rows hold is the compile-time KINDNESS in front of it -- the value
// of being told at the `on` which binding is the window and what to write
// instead, rather than debugging a panic under a shard count. So a red here is a
// message that got worse, not a hole that opened, and it is worth exactly that
// much.
//
// What the gate can see is a BINDING whose value may be, or may HOLD, a window:
// a `let` bound to a slice, a name rebound to one on any path, an arm of a
// choice that is one, an element of an array or tuple literal, and a read back
// out of such a holder. A view laundered through a call's return value, a struct
// field or a parameter keeps no marker, and neither does one written into a
// holder by `xs.push(v)`, `xs[0] = v` or `pair.0 = v`; every one of those builds
// and dies at the registry instead, which is why they are not rows here.
//
// Every row below except the last two was measured RUNNING before it was
// written: each program compiled on the tree that opened this gate, crossed, and
// the destination shard wrote 777 (or 555) back into the origin shard's own
// `base`, at 2 and at 8 shards.
func TestCrossingArrayViewCapturesAreRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "a view of an int array captured into an on body",
			src: `
fn use(xs: own int[]) -> int { return xs[0]; }

async fn go(dst: Placement) -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let r: TaskResult<int> = on dst { ret use(own v); };
    return 0;
}
`,
			contains: []string{
				"`v` is a view of another array", "cannot cross a shard boundary",
				"build one holding a copy of every element in the window",
			},
		},
		// The marker follows the binding, not the expression: `w` never names a
		// slice, and crossing it would alias `base`'s buffer just the same.
		{
			name: "a view rebound to a second name and captured into an on body",
			src: `
fn use(xs: own int[]) -> int { return xs[0]; }

async fn go(dst: Placement) -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let w: int[] = v;
    let r: TaskResult<int> = on dst { ret use(own w); };
    return 0;
}
`,
			contains: []string{"`w` is a view of another array"},
		},
		// `spawn on` shares checkOnCaptures with `on`, so it shares this rule.
		{
			name: "a view of an int array moved into a spawn on body",
			src: `
fn use(xs: own int[]) -> int { return xs[0]; }

async fn start(dst: Placement) -> far Task<int> {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    return spawn on dst { ret use(own v); };
}
`,
			contains: []string{"`v` is a view of another array"},
		},
		// Every array's view meets TWO refusals -- this one at the gate and the
		// runtime's VM1003 if it ever got past -- and the float element is no
		// longer what makes the second one true. The gate is asked first, so the
		// reader gets a compile error with a way out rather than an abort.
		{
			name: "a view of a float array captured into an on body",
			src: `
fn use(xs: own float[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let a: float = 1.5;
    let base: float[] = [a, a, a];
    let v: float[] = base[[1..2]];
    let r: TaskResult<int> = on dst { ret use(own v); };
    print(a to string);
    return 0;
}
`,
			contains: []string{"`v` is a view of another array", "Cross an array of your own instead"},
		},
		// The marker is a MAY fact and an assignment on a branch that may not run
		// cannot withdraw it. With `flag` false this program compiled, crossed,
		// and printed `remote=1665 base1=777 base2=888` at 2 and at 8 shards --
		// the same numbers the view rule was written to stop.
		{
			name: "a view a not-taken branch reassigns is still a view at the capture",
			src: `
fn use(xs: own int[]) -> int { return xs[0]; }

async fn go(dst: Placement, flag: bool) -> int {
    let base: int[] = [1, 2, 3, 4];
    let mut v: int[] = base[[1..3]];
    if flag { v = [9, 9]; }
    let r: TaskResult<int> = on dst { ret use(own v); };
    return 0;
}
`,
			contains: []string{"`v` is a view of another array"},
		},
		// A view can be the VALUE of a choice without being its syntactic head.
		// Reading only the head answered "not a view" and the window crossed:
		// `remote=777 base1=777`, 2 and 8 shards.
		{
			name: "a view produced by every arm of a compare is a view at the capture",
			src: `
fn use(xs: own int[]) -> int { return xs[0]; }

async fn go(dst: Placement, flag: bool) -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = compare flag {
        true => base[[1..3]];
        false => base[[0..2]];
    };
    let r: TaskResult<int> = on dst { ret use(own v); };
    return 0;
}
`,
			contains: []string{"`v` is a view of another array"},
		},
		// ON-CAP-N008. The capture is a fresh owned array; its ELEMENT is the
		// window. V005 asks the element TYPE question, which `int[]` answers yes
		// to whether or not it owns its buffer, so this crossed and the
		// destination shard wrote through: `remote=777 base1=777 base2=888`, at 2
		// and at 8 shards. The refusal names the element the reader can replace.
		{
			name: "an array whose element is a view is refused, and names the element",
			src: `
fn use(xss: own int[][]) -> int { return xss[0][0]; }

async fn go(dst: Placement) -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let xs: int[][] = [v];
    let r: TaskResult<int> = on dst { ret use(own xs); };
    return 0;
}
`,
			contains: []string{"`xs` cannot cross a shard boundary", "its element `v` is a view of another array"},
		},
		// The same shape through `spawn on`, which shares the gate: measured
		// `remote=555 base1=555` at 2 and at 8 shards before this row.
		{
			name: "an array whose element is a view is refused in a spawn on body too",
			src: `
fn use(xss: own int[][]) -> int { return xss[0][0]; }

async fn start(dst: Placement) -> far Task<int> {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let xs: int[][] = [v];
    return spawn on dst { ret use(own xs); };
}
`,
			contains: []string{"`xs` cannot cross a shard boundary", "its element `v` is a view of another array"},
		},
		// A slice written straight into the literal has no name to quote, so the
		// refusal says where without pretending to say which.
		{
			name: "an array holding an unnamed slice says one of its elements is a view",
			src: `
fn use(xss: own int[][]) -> int { return xss[0][0]; }

async fn go(dst: Placement) -> int {
    let base: int[] = [1, 2, 3, 4];
    let xs: int[][] = [base[[1..3]]];
    let r: TaskResult<int> = on dst { ret use(own xs); };
    return 0;
}
`,
			contains: []string{"`xs` cannot cross a shard boundary", "one of its elements is a view of another array"},
		},
		// A view laundered through a TUPLE, with no call boundary anywhere: the
		// tuple literal records which position holds the window, and reading that
		// position back gives a view again. Measured crossing before this row:
		// `remote=777 base1=777 base2=888`, 2 and 8 shards.
		{
			name: "a view read back out of a tuple position is a view at the capture",
			src: `
fn use(xs: own int[]) -> int { return xs[0]; }

async fn go(dst: Placement) -> int {
    let base: int[] = [1, 2, 3, 4];
    let pair: (int[], int) = (base[[1..3]], 1);
    let v: int[] = own pair.0;
    let r: TaskResult<int> = on dst { ret use(own v); };
    return 0;
}
`,
			contains: []string{"`v` is a view of another array"},
		},
		// ORDERING. The view rules sit AFTER the element rule, and this row is
		// what says so: an array whose element cannot travel is refused for the
		// element even when the binding is a view, because "cross an array of your
		// own instead" would be a lie -- an owned `Channel<int>[]` does not cross
		// either. Move a view arm in front and this row goes red.
		{
			name: "a view of an array of channels is refused for its element, not for being a view",
			src: `
fn use(chs: own Channel<int>[]) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let base: Channel<int>[] = [];
    let v: Channel<int>[] = base[[0..0]];
    let r: TaskResult<int> = on dst { ret use(own v); };
    return 0;
}
`,
			contains: []string{"`Channel<int>`", "may not move between shards"},
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

// The other side of a MAY analysis: what it must NOT refuse.
//
// The view rules above widened twice -- a marker that is never withdrawn, and a
// view found inside a literal -- and either widening could have swallowed shapes
// that own every byte they carry. A table of refusals alone cannot notice that;
// these rows can, and each of them runs today: the nested array prints 10 and
// the placement array prints 2, at SURGE_SHARDS 2 and at 8, with valgrind
// reporting definitely lost: 0 bytes in 0 blocks.
func TestCrossingArrayCapturesThatHoldNoViewStillCross(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
	}{
		// An array of arrays, every element built from a literal. The holder rule
		// asks about the elements' provenance, not about their type.
		{
			name: "an array of arrays whose elements are literals",
			src: `
fn total(xss: own int[][]) -> int { return xss[0][0] + xss[1][1]; }

async fn go(dst: Placement) -> int {
    let xss: int[][] = [[1, 2], [3, 4]];
    let r: TaskResult<int> = on dst { ret total(own xss); };
    return 0;
}
`,
		},
		// A tuple that DOES hold a view, read at the position that does not. The
		// positions are recorded, so the doubt stops at the one window there is.
		{
			name: "the tuple position that is not the view",
			src: `
fn use(xs: own int[]) -> int { return xs[0]; }

async fn go(dst: Placement) -> int {
    let base: int[] = [1, 2, 3, 4];
    let pair: (int[], int[]) = (base[[1..3]], [7, 8, 9]);
    let w: int[] = own pair.1;
    let r: TaskResult<int> = on dst { ret use(own w); };
    return 0;
}
`,
		},
		// `Placement[]`. A `Placement` has crossed alone since Block 2 as a Copy
		// value, and core calls it "a Copy, shard-movable tagged word"; inside an
		// array it was refused, by a sentence telling the reader to mark
		// `@shard_movable` on a core `@intrinsic` type. Both legs of the axis now
		// answer what the language documents.
		{
			name: "an array of placements",
			src: `
fn use(ps: own Placement[]) -> int { return len(ps) to int; }

async fn go(dst: Placement) -> int {
    let p: Placement = shard(1:ShardId);
    let ps: Placement[] = [p, p];
    let r: TaskResult<int> = on dst { ret use(own ps); };
    return 0;
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compileCleanly(t, tc.src)
		})
	}
}

// A moved array read by the crossing body must be released by that body.
// Counted elements additionally require their exact element-drop callback.
func TestCrossingArrayCaptureIsReclaimedByTheBodyThatReadsIt(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name  string
		src   string
		fixed bool
	}{
		{
			name: "an on body that only reads its captured array",
			src: `
async fn go(dst: Placement) -> int {
    let xs: int[] = [11, 22, 33];
    let r: TaskResult<int> = on dst { let v: int = xs[0]; ret v; };
    return 0;
}
`,
		},
		{
			name: "a spawn on body that only reads its captured array",
			src: `
async fn start(dst: Placement) -> far Task<int> {
    let xs: int[] = [11, 22, 33];
    return spawn on dst { let v: int = xs[0]; ret v; };
}
`,
		},
		{name: "fixed64_on_body_frees_array", fixed: true, src: `
async fn go(dst: Placement) -> int64 {
    let xs: int64[] = [11, 22, 33];
    let r: TaskResult<int64> = on dst { let v: int64 = xs[0]; ret v; };
    return 0;
}
`},
		{name: "fixed64_spawn_on_body_frees_array", fixed: true, src: `
async fn start(dst: Placement) -> far Task<int64> {
    let xs: int64[] = [11, 22, 33];
    return spawn on dst { let v: int64 = xs[0]; ret v; };
}
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireCrossingBodyFreesAnArray(t, compileCleanly(t, tc.src), !tc.fixed)
		})
	}
}

// The array argument must be loaded from the same slot subsequently cleared
// before this body's reply. An unrelated drop-glue definition is insufficient.
func requireCrossingBodyFreesAnArray(t *testing.T, ir string, counted bool) {
	t.Helper()
	call := regexp.MustCompile(`call void @rt_array_free\(ptr (%[\w.]+), i64 8, i64 8\)`)
	if counted {
		call = regexp.MustCompile(`call void @rt_array_free_elems\(ptr (%[\w.]+), i64 8, i64 8, ptr @([\w.]+)\)`)
	}
	for _, section := range strings.Split(ir, "\ndefine ")[1:] {
		end := strings.Index(section, "\n}")
		if end < 0 {
			continue
		}
		body := section[:end]
		reply := strings.Index(body, "call void @rt_async_return(")
		if reply < 0 {
			continue
		}
		loc := call.FindStringSubmatchIndex(body[:reply])
		if loc == nil {
			continue
		}
		arg := body[loc[2]:loc[3]]
		load := regexp.MustCompile(regexp.QuoteMeta(arg) + ` = load ptr, ptr (%[\w.]+), align 8`).FindStringSubmatch(body[:loc[0]])
		if len(load) != 2 || !strings.Contains(body[loc[1]:reply], "store ptr null, ptr "+load[1]+", align 8") {
			continue
		}
		if counted {
			callback := body[loc[4]:loc[5]]
			requireCountedArrayDropCallback(t, ir, callback)
		}
		return
	}
	t.Fatalf("no crossing body frees and clears its array before reply (counted=%v):\n%s", counted, ir)
}

func requireCountedArrayDropCallback(t *testing.T, ir, callback string) {
	t.Helper()
	start := strings.Index(ir, "define void @"+callback+"(ptr %slot) {")
	if start < 0 {
		t.Fatalf("missing element-drop callback %s", callback)
	}
	body := ir[start:]
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatalf("unterminated element-drop callback %s", callback)
	}
	body = body[:end]
	// Capture SSA identities to link the released pointer to its low-bit and
	// null guards. The heap branch must contain the release of that pointer.
	read := regexp.MustCompile(`(%[\w.]+) = load ptr, ptr %slot, align 8`).FindStringSubmatch(body)
	if len(read) != 2 {
		t.Fatalf("%s does not read its element slot", callback)
	}
	value := regexp.QuoteMeta(read[1])
	bits := regexp.MustCompile(`(%[\w.]+) = ptrtoint ptr ` + value + ` to i64`).FindStringSubmatch(body)
	if len(bits) != 2 {
		t.Fatalf("%s does not inspect the numeric tag", callback)
	}
	tag := regexp.MustCompile(`(%[\w.]+) = and i64 ` + regexp.QuoteMeta(bits[1]) + `, 1`).FindStringSubmatch(body)
	if len(tag) != 2 {
		t.Fatalf("%s does not mask the numeric tag", callback)
	}
	heap := regexp.MustCompile(`(%[\w.]+) = icmp eq i64 ` + regexp.QuoteMeta(tag[1]) + `, 0`).FindStringSubmatch(body)
	nonnull := regexp.MustCompile(`(%[\w.]+) = icmp ne ptr ` + value + `, null`).FindStringSubmatch(body)
	if len(heap) != 2 || len(nonnull) != 2 {
		t.Fatalf("%s lacks heap/null guards", callback)
	}
	guard := regexp.MustCompile(`(%[\w.]+) = and i1 ` + regexp.QuoteMeta(nonnull[1]) + `, ` + regexp.QuoteMeta(heap[1])).FindStringSubmatch(body)
	if len(guard) != 2 {
		t.Fatalf("%s does not combine heap/null guards", callback)
	}
	branch := regexp.MustCompile(`br i1 ` + regexp.QuoteMeta(guard[1]) + `, label %([\w.]+), label %[\w.]+`).FindStringSubmatch(body)
	if len(branch) != 2 {
		t.Fatalf("%s does not branch on its heap guard", callback)
	}
	blockStart := strings.Index(body, branch[1]+":\n")
	if blockStart < 0 {
		t.Fatalf("%s lacks its heap branch", callback)
	}
	block := body[blockStart:]
	if next := regexp.MustCompile(`\n[\w.]+:`).FindStringIndex(block); next != nil {
		block = block[:next[0]]
	}
	if !strings.Contains(block, "call void @rt_bigint_release(ptr "+read[1]+")") {
		t.Fatalf("%s does not release the counted element on its heap branch", callback)
	}
}
