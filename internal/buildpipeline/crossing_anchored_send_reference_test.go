package buildpipeline

import (
	"testing"

	"surge/internal/diag"
)

// An index read is a REFERENCE, and a reference cannot go on a ring another
// shard reads. `xs[0]` where `xs: int[]` has type `&int`; the plain crossing has
// always said so (`ret xs[0]` is refused "cannot assign TaskResult<&int> to
// TaskResult<int>"), and this sink now says the same thing with the same code.
//
// `own` IS NOT A WAY ROUND IT, and the second half of this table is what says
// so. `own &T` is a reference too -- the plain crossing refuses `ret own xs[0]`
// with "cannot assign TaskResult<own &int> to TaskResult<int>", measured on this
// tree for `int` and for a `@copy` struct alike -- so a rule that asked only the
// payload's surface type left one keyword between a reader and the corruption.
//
// Red before the refusal, measured rather than argued, at SURGE_SHARDS/THREADS 2
// and at 8. Without `own`: the `int[]` row BUILT and printed a different 70-digit
// negative number on every run instead of 11 -- an address read as an
// arbitrary-precision integer; the `string[]` row died with "free(): double free
// detected in tcache 2"; the `Pair[]` row printed the same kind of garbage. With
// `own`: the `string[]` row double-freed at both widths; the `Pair[]` row printed
// 11 where its two fields sum to 33, a silent wrong answer with exit 0; the
// `int[]` row printed 11 and is refused all the same, because the plain crossing
// refuses that very program and a sink more permissive than the type system is
// the defect this table is about.
//
// TWO ROWS ARE NOT THIS LANE'S, and they are marked. Both go through a
// `@shard_movable` wrapper, whose capture `63ecd58b` accepted by an arm this lane
// never touched: `ch.send(b.a[0])` printed the 70-digit garbage there and
// `ch.send(own b.a[0])` printed 11 there. Asked of the payload's shape rather
// than of the capture, the refusal closes both routes as well.
//
// Three element families, because the three take DIFFERENT ways out and a
// diagnostic that offers the wrong one sends its reader to a second refusal. A
// Copy element that shares no counted block copies out under a name and sends
// bare; a Copy element that MAY share one (a `float`) copies out under a name and
// must then be GIVEN AWAY, or the give-away rule refuses it one build later; a
// `string` does not copy out under a name at all (`let v: string = xs[0];` is
// refused for the same reference), so its help names the whole-array give-away.
// Each of the three is measured printing the right answer at 2 shards and at 8.
func TestAnchoredSendOfABorrowedReadIsRefused(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
		want string
		help string
	}{
		{
			name: "an element of a captured int array",
			src: `
async fn go() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3];
    let sent: TaskResult<nothing> = on ch { ch.send(xs[0]); ret nothing; };
    return 0;
}
`,
			want: "`&int` is a borrowed read",
			help: "`let v: int = xs[0];` outside the block, then `ch.send(v)`",
		},
		{
			name: "an element of a captured string array",
			src: `
async fn go() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 4);
    let xs: string[] = ["ab", "cde"];
    let sent: TaskResult<nothing> = on ch { ch.send(xs[0]); ret nothing; };
    return 0;
}
`,
			want: "`&string` is a borrowed read",
			help: "does not copy out of the buffer under a name",
		},
		{
			name: "an element of a captured array of a shard-movable type",
			src: `
@shard_movable
type Pair = { a: int, b: int };

async fn go() -> int {
    let ch: far Channel<Pair> = channel_on::<Pair>(shard(0:ShardId), 4);
    let xs: Pair[] = [Pair{ a: 1, b: 2 }];
    let sent: TaskResult<nothing> = on ch { ch.send(xs[0]); ret nothing; };
    return 0;
}
`,
			want: "`&Pair` is a borrowed read",
			help: "does not copy out of the buffer under a name",
		},
		{
			// A counted Copy element: it copies out under a name, and then the
			// give-away rule wants the name given away. The help has to say
			// `own`, or the reader's next build is SEM3212.
			name: "an element of a captured float array",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let xs: float[] = [1.5, 2.5];
    let sent: TaskResult<nothing> = on ch { ch.send(xs[0]); ret nothing; };
    return 0;
}
`,
			want: "`&float` is a borrowed read",
			help: "`let v: float = xs[0];` outside the block, then `ch.send(own v)`",
		},
		{
			// Not this lane's shape: the wrapper crossed at 63ecd58b as well,
			// so this route into the same corruption predates the widening and
			// closes with it.
			name: "an element of an array inside a shard-movable capture",
			src: `
@shard_movable
type Box = { a: int[] };

async fn go() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let b: own Box = own Box{ a: [1, 2, 3] };
    let sent: TaskResult<nothing> = on ch { ch.send(b.a[0]); ret nothing; };
    return 0;
}
`,
			want: "`&int` is a borrowed read",
			help: "`let v: int = xs[0];` outside the block, then `ch.send(v)`",
		},
		{
			name: "an element of a captured int array, given away",
			src: `
async fn go() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let xs: int[] = [1, 2, 3];
    let sent: TaskResult<nothing> = on ch { ch.send(own xs[0]); ret nothing; };
    return 0;
}
`,
			want: "`own &int` is a borrowed read",
			help: "`let v: int = xs[0];` outside the block, then `ch.send(v)`",
		},
		{
			name: "an element of a captured string array, given away",
			src: `
async fn go() -> int {
    let ch: far Channel<string> = channel_on::<string>(shard(0:ShardId), 4);
    let xs: string[] = ["ab", "cde"];
    let sent: TaskResult<nothing> = on ch { ch.send(own xs[0]); ret nothing; };
    return 0;
}
`,
			want: "`own &string` is a borrowed read",
			help: "does not copy out of the buffer under a name",
		},
		{
			name: "an element of a captured array of a shard-movable type, given away",
			src: `
@shard_movable
type Pair = { a: int, b: int };

async fn go() -> int {
    let ch: far Channel<Pair> = channel_on::<Pair>(shard(0:ShardId), 4);
    let xs: Pair[] = [Pair{ a: 1, b: 2 }];
    let sent: TaskResult<nothing> = on ch { ch.send(own xs[0]); ret nothing; };
    return 0;
}
`,
			want: "`own &Pair` is a borrowed read",
			help: "does not copy out of the buffer under a name",
		},
		{
			// The nested array. Its refusal used to be the shape rule's, which
			// could only reach it because the reference question was not asked
			// underneath the `own`; the shape rule still owns `own xs[[1..3]]`,
			// which is a window and not a reference.
			name: "an element of a captured array of arrays, given away",
			src: `
async fn go() -> int {
    let ch: far Channel<int[]> = channel_on::<int[]>(shard(0:ShardId), 4);
    let xss: int[][] = [[1, 2], [3, 4]];
    let sent: TaskResult<nothing> = on ch { ch.send(own xss[0]); ret nothing; };
    return 0;
}
`,
			want: "`own &[int]` is a borrowed read",
			help: "does not copy out of the buffer under a name",
		},
		{
			// Not this lane's shape either: `ch.send(own b.a[0])` printed 11 at
			// 63ecd58b, at 2 shards and at 8, on a capture that tree accepted.
			name: "an element of an array inside a shard-movable capture, given away",
			src: `
@shard_movable
type Box = { a: int[] };

async fn go() -> int {
    let ch: far Channel<int> = channel_on::<int>(shard(0:ShardId), 4);
    let b: own Box = own Box{ a: [1, 2, 3] };
    let sent: TaskResult<nothing> = on ch { ch.send(own b.a[0]); ret nothing; };
    return 0;
}
`,
			want: "`own &int` is a borrowed read",
			help: "`let v: int = xs[0];` outside the block, then `ch.send(v)`",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireAnchoredSendDiagnosticWithHelp(t, tc.src, diag.SemaTypeMismatch, tc.want, tc.help)
		})
	}
}

// The help each of those refusals offers, compiled. A refusal whose advice does
// not build is worse than no advice, and the `float` row above is exactly that
// mistake caught: its help said `ch.send(v)` for one build, and following it
// landed on SEM3212. Every row here is the advice from the row above it,
// written out and compiled; each also RUNS and prints the right answer at
// SURGE_SHARDS/THREADS 2 and at 8 (`11`, `11`, `5`, `33`, `11` in order).
func TestAnchoredSendRefusalHelpCompiles(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	for _, tc := range []struct{ name, src string }{
		{
			name: "the int element's help: bind it out and send the name",
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
			name: "the float element's help: bind it out and give the name away",
			src: `
async fn go() -> int {
    let ch: far Channel<float> = channel_on::<float>(shard(0:ShardId), 4);
    let xs: float[] = [1.5, 2.5];
    let v: float = xs[0];
    let sent: TaskResult<nothing> = on ch { ch.send(own v); ret nothing; };
    return 0;
}
`,
		},
		{
			name: "the string element's help: send the whole array",
			src: `
async fn go() -> int {
    let ch: far Channel<string[]> = channel_on::<string[]>(shard(0:ShardId), 4);
    let xs: string[] = ["ab", "cde"];
    let sent: TaskResult<nothing> = on ch { ch.send(own xs); ret nothing; };
    return 0;
}
`,
		},
		{
			name: "the shard-movable element's help: send the whole array",
			src: `
@shard_movable
type Pair = { a: int, b: int };

async fn go() -> int {
    let ch: far Channel<Pair[]> = channel_on::<Pair[]>(shard(0:ShardId), 4);
    let xs: Pair[] = [Pair{ a: 1, b: 2 }];
    let sent: TaskResult<nothing> = on ch { ch.send(own xs); ret nothing; };
    return 0;
}
`,
		},
		{
			name: "the nested array's help: send the whole array",
			src: `
async fn go() -> int {
    let ch: far Channel<int[][]> = channel_on::<int[][]>(shard(0:ShardId), 4);
    let xss: int[][] = [[1, 2], [3, 4]];
    let sent: TaskResult<nothing> = on ch { ch.send(own xss); ret nothing; };
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
