package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
)

// A remote channel's element is asked, at the channel's creation, the same
// question a capture is asked: not "does it hold a counted block" but "can
// the walk make the block private before the value leaves". Every entry into
// the ring hands over a private reference now -- a far-select SEND payload is
// un-shared in the relinquishing operand, and an anchored body's
// `ch.send(own f)` gives away the capture's own reference, one the caller made
// private when the capture entered the state (sema holds the send to that
// shape: SemaAnchoredSendGiveAway) -- so a bare `float`, a union of structs
// carrying one, a fixed array of them, and a `@copy` union all mint a remote
// channel and ship.
//
// Red on the tree before the narrowing: every row here was refused with
// FUT7020 ("remote channel cannot carry ..."); the first two of them had
// been refused since the element gate was widened to unions on 2026-09-06.
//
// A row whose element is a dynamic array reads the emitted IR and demands the
// buffer walk. Compiling cleanly says the element gate let the shape through;
// it says nothing about what the send's relinquishing operand emits, and an
// un-share whose body is empty builds just as quietly.
func TestRefCountedScalarChannelElementsShip(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
		walk *bufferWalk
	}{
		{
			name: "remote channel with a float element, fed by an anchored send",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let a: float = 1.5;
    let f: float = a;
    let sent: TaskResult<nothing> = on ch { ch.send(own f); ret nothing; };
    print(a to string);
    return 0;
}
`,
		},
		{
			name: "remote channel with a float element, fed by a far-select send arm",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let a: float = 1.5;
    let won: int = select {
        ch.send(a) => 1;
    };
    print(a to string);
    return won;
}
`,
		},
		{
			name: "remote channel with a union element carrying a float",
			src: `
type P = { v: float };

tag Held(P);
tag Empty();
type U = Held(P) | Empty();

async fn go() -> int {
    let ch: far Channel<U> = channel_on::<U>(shard(0:ShardId), 4);
    let a: float = 2.5;
    let held: U = Held(P{ v: a });
    let won: int = select {
        ch.send(own held) => 1;
    };
    print(a to string);
    return won;
}
`,
		},
		{
			// Fed by a far-select arm: a fixed array is an owned user value to the
			// capture rule of an `on` body ("must be `@shard_movable`", the
			// pre-existing limit recorded on RV2-DEBT-038), so the anchored form
			// is not the one to exercise here.
			name: "remote channel with a fixed float array element",
			src: `
async fn go() -> int {
    let ch: far Channel<float[4]> = channel_on::<float[4]>(shard(0:ShardId), 4);
    let a: float = 1.5;
    let quad: float[4] = [a, a, a, a];
    let won: int = select {
        ch.send(own quad) => 1;
    };
    print(a to string);
    return won;
}
`,
		},
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
    let a: float = 2.5;
    let held: U = Held(P{ v: a });
    let sent: TaskResult<nothing> = on ch { ch.send(own held); ret nothing; };
    print(a to string);
    return 0;
}
`,
		},
		// The array's elements are one reference each into blocks `a` still
		// holds; the send's relinquishing operand hands the buffer to the
		// runtime's element walk, which makes each private before the ring
		// takes the array. Refused before that walk existed, with the buffer
		// named as the reason.
		{
			name: "remote channel with a float array element, fed by a far-select send arm",
			src: `
async fn go() -> int {
    let ch: far Channel<float[]> = channel_on::<float[]>(shard(0:ShardId), 4);
    let a: float = 1.5;
    let xs: float[] = [a];
    let won: int = select {
        ch.send(own xs) => 1;
    };
    print(a to string);
    return won;
}
`,
			walk: &bufferWalk{stride: 8, elem: []string{"call ptr @rt_bigfloat_unshare("}},
		},
		// Built empty, so the element union `Option<float>` is named by the
		// array's type and by nothing else in the function; the send's walk
		// switches on its tag per slot and reads a membership the module has
		// to publish from the type alone.
		{
			name: "remote channel with an element of optional floats, the array built empty and sent by a far-select arm",
			src: `
async fn go() -> int {
    let ch: far Channel<Option<float>[]> = channel_on::<Option<float>[]>(shard(0:ShardId), 4);
    let xs: Option<float>[] = [];
    let won: int = select {
        ch.send(own xs) => 1;
    };
    return won;
}
`,
			walk: &bufferWalk{
				stride: 16,
				elem:   []string{"switch i32", "call ptr @rt_bigfloat_unshare("},
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

// What no walk can make private stays refused at the channel's creation, in
// words that say why: a map's table is storage the sender keeps, and no
// per-element walk reaches it the way the array's buffer walk reaches a
// buffer.
func TestRefCountedScalarChannelElementsStayRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "remote channel with a map element valued by floats",
			src: `
async fn go() -> int {
    let ch: far Channel<Map<int, float>> = channel_on::<Map<int, float>>(shard(0:ShardId), 4);
    return 0;
}
`,
			contains: []string{"remote channel cannot carry `", "map's table"},
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
			found := findDiagnostic(res.Diagnose.Bag.Items(), diag.FutCrossingPayloadNotShippable)
			if found == nil {
				t.Fatalf("the element was not refused; got %s", summarizeCodes(res.Diagnose.Bag.Items()))
			}
			for _, want := range tc.contains {
				if !strings.Contains(found.Message, want) {
					t.Fatalf("refusal does not say %q: %s", want, found.Message)
				}
			}
		})
	}
}

// The anchored send of a counted scalar is held to `ch.send(own f)`: the
// ring takes the only reference and the body cannot keep or make another
// (a parked send re-enters the body from the top). The three shapes below
// compiled before the element gate narrowed only because the channel itself
// was refused; each is refused on its own now, with the way out.
func TestAnchoredSendOfACountedScalarMustGiveTheBindingAway(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
		code diag.Code
		want string
	}{
		{
			name: "a plain read of the capture",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let f: float = 1.5;
    let sent: TaskResult<nothing> = on ch { ch.send(f); ret nothing; };
    return 0;
}
`,
			code: diag.SemaAnchoredSendGiveAway,
			want: "must give a captured binding away",
		},
		{
			name: "a literal built inside the body",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let sent: TaskResult<nothing> = on ch { ch.send(1.5); ret nothing; };
    return 0;
}
`,
			code: diag.SemaAnchoredSendGiveAway,
			want: "must give a captured binding away",
		},
		{
			name: "the capture read after it was given away",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let f: float = 1.5;
    let sent: TaskResult<nothing> = on ch { ch.send(own f); print(f to string); ret nothing; };
    return 0;
}
`,
			code: diag.SemaUseAfterMove,
			want: "use of moved value 'f'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireSendPayloadDiagnostic(t, tc.src, tc.code, tc.want)
		})
	}
}

// The anchored send of a captured DYNAMIC ARRAY is held to the same shape, by
// the second arm of the same rule. `[int]` shares no counted block, so the
// element question says nothing about it -- while the body owes the capture's
// header and its buffer a drop from the moment the capture moved in
// (registerCrossingBodyOwnership). A payload that is not that binding, given
// away, therefore leaves the ring and the body's scope exit owning one buffer.
//
// Red before the arm, measured rather than argued: the first row below BUILT
// and died with "free(): double free detected in tcache 2" at SURGE_SHARDS and
// THREADS 2 and at 8, and under valgrind reported 2 invalid frees, 6 invalid
// reads and an array header freed twice. Its `float[]` twin was already clean,
// because a float element makes the FIRST arm fire.
//
// Every row here is refused only because the capture gate admits a bare `[T]`
// at all: at 63ecd58b all four failed to compile one line earlier, at the
// capture, with SEM3168 "this owned value is not shard-movable" -- re-measured,
// including the `own` window, rather than carried over.
func TestAnchoredSendOfACapturedArrayMustGiveTheBindingAway(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
		code diag.Code
		want string
	}{
		{
			name: "a plain read of the capture",
			src: `
async fn go() -> int {
    let ch: far Channel<int[]> = channel_on::<int[]>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3];
    let sent: TaskResult<nothing> = on ch { ch.send(xs); ret nothing; };
    return 0;
}
`,
			code: diag.SemaAnchoredSendGiveAway,
			want: "must give a captured binding away",
		},
		{
			// The shape that would hand the ring a window into a buffer the
			// body frees on its way out.
			name: "a window sliced out of the capture",
			src: `
async fn go() -> int {
    let ch: far Channel<int[]> = channel_on::<int[]>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3, 4];
    let sent: TaskResult<nothing> = on ch { ch.send(xs[[1..3]]); ret nothing; };
    return 0;
}
`,
			code: diag.SemaAnchoredSendGiveAway,
			want: "must give a captured binding away",
		},
		{
			// The same window with `own` in front of it. `own` names a place
			// here rather than a whole binding, and the shape rule says so --
			// this is the row that keeps that arm of the message alive now that
			// `own xss[0]` is answered a step earlier, for its reference.
			name: "a window sliced out of the capture and given away",
			src: `
async fn go() -> int {
    let ch: far Channel<int[]> = channel_on::<int[]>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3, 4];
    let sent: TaskResult<nothing> = on ch { ch.send(own xs[[1..3]]); ret nothing; };
    return 0;
}
`,
			code: diag.SemaAnchoredSendGiveAway,
			want: "must name a whole binding the block captured",
		},
		{
			name: "the capture read after it was given away",
			src: `
async fn go() -> int {
    let ch: far Channel<int[]> = channel_on::<int[]>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3];
    let sent: TaskResult<nothing> = on ch { ch.send(own xs); print(xs[0] to string); ret nothing; };
    return 0;
}
`,
			code: diag.SemaUseAfterMove,
			want: "use of moved value 'xs'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireSendPayloadDiagnostic(t, tc.src, tc.code, tc.want)
		})
	}
}

// The admission the four refusals above are worth having: the shape the rule
// names compiles, for the element types ON-CAP-V005 admits. Without this row a
// rule that refused every array payload would pass the refusal rows too.
//
// The `float[]` row is the control on the ORDER of the two arms: its element
// shares a counted block, so the first arm answers it, and the second must not
// answer it again.
func TestAnchoredSendOfACapturedArrayIsAccepted(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, tc := range []struct{ name, elem, literal string }{
		{"an int array", "int[]", "[1, 2, 3]"},
		{"an array of arrays", "int[][]", "[[1, 2], [3, 4]]"},
		{"a string array", "string[]", `["ab", "cde"]`},
		{"a float array, answered by the counted arm", "float[]", "[1.5, 2.5]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compileCleanly(t, `
async fn go() -> int {
    let ch: far Channel<`+tc.elem+`> = channel_on::<`+tc.elem+`>(shard(0:ShardId), 4);
    let xs: `+tc.elem+` = `+tc.literal+`;
    let sent: TaskResult<nothing> = on ch { ch.send(own xs); ret nothing; };
    return 0;
}
`)
		})
	}
}

// The other half of that rule's gate: the array question is asked only when the
// ELEMENT is an array, because only then is there a buffer in the ring for the
// body and the ring to own between them.
//
// A VALUE read out of a captured array and sent is that half's business only in
// the sense that it is none of it: the body drops the array exactly as it always
// did, and the ring keeps nothing of it. Each row here is a value -- computed by
// a callee, computed in place, bound to a name before the block, or read out of
// an element's FIELD -- and each one, run, prints the number it was given at
// SURGE_SHARDS/THREADS 2 and at 8.
//
// The last row is why the element half exists at all, and it is measured rather
// than argued: `ch.send(xs[0].a)` on a captured `Pair[]` over a far
// `Channel<int>` is a value whose place resolves to the captured array, and with
// the element question deleted from anchoredSendIsOfACapturedArray this program
// is refused SEM3212. It prints 11 at both widths as it stands.
//
// The shape that is NOT here is `ch.send(xs[0])`, with or without `own`. An
// index read is a BORROW, not a value, and it is refused a step earlier now; the
// table below owns it.
func TestAnchoredSendOfAValueReadOutOfACapturedArrayIsNotRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, tc := range []struct{ name, src string }{
		{
			name: "a value computed from the captured array",
			src: `
fn total(xs: own int[]) -> int { return xs[0] + xs[1] + xs[2]; }

async fn go() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3];
    let sent: TaskResult<nothing> = on ch { ch.send(total(own xs)); ret nothing; };
    return 0;
}
`,
		},
		{
			name: "a value computed in place from the captured array",
			src: `
async fn go() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3];
    let sent: TaskResult<nothing> = on ch { ch.send(xs[0] + 0); ret nothing; };
    return 0;
}
`,
		},
		{
			name: "an element copied out under a name before the block",
			src: `
async fn go() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3];
    let v: int = xs[0];
    let sent: TaskResult<nothing> = on ch { ch.send(v); ret nothing; };
    return 0;
}
`,
		},
		{
			name: "a field read out of an element of the captured array",
			src: `
@shard_movable
type Pair = { a: int, b: int };

async fn go() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let xs: Pair[] = [Pair{ a: 1, b: 2 }];
    let sent: TaskResult<nothing> = on ch { ch.send(xs[0].a); ret nothing; };
    return 0;
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compileCleanly(t, tc.src)
		})
	}
}

// requireSendPayloadDiagnostic compiles src and demands one diagnostic with
// the given code whose message says want.
func requireSendPayloadDiagnostic(t *testing.T, src string, code diag.Code, want string) {
	t.Helper()
	requireSendPayloadDiagnosticWithHelp(t, src, code, want, "")
}

// requireSendPayloadDiagnosticWithHelp is the same demand plus the way out.
// A refusal whose help is not asserted is a refusal whose help can rot into
// advice that does not compile, which is what a reader meets first.
func requireSendPayloadDiagnosticWithHelp(t *testing.T, src string, code diag.Code, want, help string) {
	t.Helper()
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
	found := findDiagnostic(res.Diagnose.Bag.Items(), code)
	if found == nil {
		t.Fatalf("no %s; got %s", code.ID(), summarizeCodes(res.Diagnose.Bag.Items()))
	}
	if !strings.Contains(found.Message, want) {
		t.Fatalf("message does not say %q: %s", want, found.Message)
	}
	if help == "" {
		return
	}
	var offered []string
	for _, note := range found.Help {
		offered = append(offered, note.Msg)
		if strings.Contains(note.Msg, help) {
			return
		}
	}
	t.Fatalf("no help says %q; the diagnostic offers %q", help, offered)
}
