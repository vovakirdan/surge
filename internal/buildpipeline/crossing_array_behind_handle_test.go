package buildpipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A dynamic array carries a fact no type holds: whether the header in hand is a
// VIEW into somebody else's buffer. The runtime's view registry holds it, and a
// crossing reaches the registry by handing the array's slot to
// rt_array_unshare_walk in the relinquishing operand.
//
// That works wherever the walk can reach the array. It cannot reach one stored
// in a map's table, a channel's ring or a task's result slot: those keep their
// entries at offsets no per-element callback is ever handed. So the array goes
// across with nothing looking at it.
//
// Measured on this tree at 273ca202, before the rows below: a
// `Map<int, int[]>` whose one value was `base[[1..3]]`, captured into
// `blocking`, printed `7770777` at SURGE_SHARDS/THREADS 2 and 8 -- the worker
// thread took the view out of the table and wrote 777 through it into slot 1
// of the base the origin shard was still reading. A `Channel<int[]>` whose ring
// held the same view printed the same number by the same route. Both compiled
// with zero `rt_array_unshare_walk` call sites in the module.
//
// The answer for storage the walk cannot reach is a REFUSAL, and it is the one
// the counted-block half has always given at this same stop: `Map<int, float>`
// and `Channel<float>` have been refused here for the floats behind them. The
// rows below are the array half of that sentence.
func TestArrayBehindAHandleIsRefusedAtEveryCrossingGate(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name     string
		src      string
		contains []string
	}{
		{
			name: "map of arrays captured into a blocking body",
			src: `
fn use(m: own Map<int, int[]>) -> int { return 1; }

async fn go() -> int {
    let m: Map<int, int[]> = Map::<int, int[]>.new();
    let job: Task<int> = blocking { ret use(own m); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
			contains: []string{
				"cannot be captured into `blocking`",
				"holds a dynamic array in storage this thread keeps",
				"map's table",
			},
		},
		{
			name: "local channel of arrays captured into a blocking body",
			src: `
fn use(c: own Channel<int[]>) -> int { return 1; }

async fn go() -> int {
    let ch: Channel<int[]> = Channel::<int[]>::new(4:uint);
    let job: Task<int> = blocking { ret use(own ch); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
			contains: []string{
				"`Channel<[int]>` cannot be captured into `blocking`",
				"never shown that array's header",
			},
		},
		{
			name: "struct that merely holds a map of arrays captured into a blocking body",
			src: `
type Box = { m: Map<int, int[]>, n: int };

fn use(b: own Box) -> int { return 1; }

async fn go() -> int {
    let m: Map<int, int[]> = Map::<int, int[]>.new();
    let b: Box = Box{ m: own m, n: 5 };
    let job: Task<int> = blocking { ret use(own b); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
			contains: []string{"`Box` cannot be captured into `blocking`", "map's table"},
		},
		{
			name: "map of arrays moved into an on body",
			src: `
fn use(m: own Map<int, int[]>) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let m: Map<int, int[]> = Map::<int, int[]>.new();
    let r: TaskResult<int> = on dst { ret use(own m); };
    return 0;
}
`,
			contains: []string{
				"cannot cross a shard boundary",
				"holds a dynamic array in storage this shard keeps",
			},
		},
		{
			name: "remote channel whose element is a map of arrays",
			src: `
async fn go() -> int {
    let ch: far Channel<Map<int, int[]>> = channel_on::<Map<int, int[]>>(shard(0:ShardId), 4);
    return 0;
}
`,
			contains: []string{"a remote channel cannot carry", "never shown that array's header"},
		},
		{
			name: "remote channel whose element is a channel of arrays",
			src: `
async fn go() -> int {
    let ch: far Channel<Channel<int[]>> = channel_on::<Channel<int[]>>(shard(0:ShardId), 4);
    return 0;
}
`,
			contains: []string{"a remote channel cannot carry", "channel's ring"},
		},
		// A channel handle is Copy, so this capture never met the owned rule
		// that turns a map away -- it rode into the body as plain bits, ring
		// and all. Measured building at 273ca202.
		{
			name: "local channel of arrays captured by copy into an on body",
			src: `
async fn go(dst: Placement) -> int {
    let ch: Channel<int[]> = Channel::<int[]>::new(4:uint);
    let r: TaskResult<int> = on dst { let c2: Channel<int[]> = ch; ret 1; };
    return 0;
}
`,
			contains: []string{
				"`Channel<Array<int>>` cannot cross a shard boundary",
				"never shown that array's header",
			},
		},
		// The mirror of it: the handle made on the FAR shard, riding the reply
		// home with the far shard's arrays in its ring. Also building at
		// 273ca202, and refused with the reason rather than "not plain-copy
		// data" -- a handle word is plain-copy data, which is why that sentence
		// would have been false.
		{
			name: "channel of arrays riding a crossing reply",
			src: `
async fn go(dst: Placement) -> int {
    let r: TaskResult<Channel<int[]>> = on dst {
        let ch: Channel<int[]> = Channel::<int[]>::new(4:uint);
        ret ch;
    };
    return 0;
}
`,
			contains: []string{
				"the crossing result `Channel<Array<int>>` cannot cross a shard boundary yet",
				"channel's ring",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireRefusalMentioning(t, tc.src, tc.contains)
		})
	}
}

// The refusal above names ONE cause, and these rows are how that claim is
// tested: each is the same program with the array taken out of the container,
// and each compiles. A gate that turned away maps, channels or `blocking`
// itself would fail here, and a false refusal on this feature is worse than
// the hole it closes -- an owned array crossing a boundary is the ordinary
// case, and the runtime already refuses the view among them by name.
func TestContainersWithoutAnArrayBehindThemStillCross(t *testing.T) {
	t.Setenv("SURGE_STDLIB", testRepoRoot(t))
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "map of ints captured into a blocking body",
			src: `
fn use(m: own Map<int, int>) -> int { return 1; }

async fn go() -> int {
    let m: Map<int, int> = Map::<int, int>.new();
    let job: Task<int> = blocking { ret use(own m); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
		},
		{
			name: "map of strings captured into a blocking body",
			src: `
fn use(m: own Map<int, string>) -> int { return 1; }

async fn go() -> int {
    let m: Map<int, string> = Map::<int, string>.new();
    let job: Task<int> = blocking { ret use(own m); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
		},
		{
			name: "local channel of ints captured into a blocking body",
			src: `
fn use(c: own Channel<int>) -> int { return 1; }

async fn go() -> int {
    let ch: Channel<int> = Channel::<int>::new(4:uint);
    let job: Task<int> = blocking { ret use(own ch); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
		},
		{
			name: "the array itself, taken out of the map and captured into a blocking body",
			src: `
fn use(xs: own int[]) -> int { return 1; }

async fn go() -> int {
    let xs: int[] = [1, 2, 3];
    let job: Task<int> = blocking { ret use(own xs); };
    let r: TaskResult<int> = job.await();
    return 0;
}
`,
		},
		{
			name: "a struct holding the array directly, moved into an on body",
			src: `
@shard_movable
type Holder = { xs: int[] };

fn use(h: own Holder) -> int { return 1; }

async fn go(dst: Placement) -> int {
    let xs: int[] = [1, 2, 3];
    let h: Holder = Holder{ xs: own xs };
    let r: TaskResult<int> = on dst { ret use(own h); };
    return 0;
}
`,
		},
		{
			name: "remote channel whose element is the array itself",
			src: `
async fn go() -> int {
    let ch: far Channel<int[]> = channel_on::<int[]>(shard(0:ShardId), 4);
    return 0;
}
`,
		},
		{
			name: "local channel of ints captured by copy into an on body",
			src: `
async fn go(dst: Placement) -> int {
    let ch: Channel<int> = Channel::<int>::new(4:uint);
    let r: TaskResult<int> = on dst { let c2: Channel<int> = ch; ret 1; };
    return 0;
}
`,
		},
		{
			name: "channel of ints riding a crossing reply",
			src: `
async fn go(dst: Placement) -> int {
    let r: TaskResult<Channel<int>> = on dst {
        let ch: Channel<int> = Channel::<int>::new(4:uint);
        ret ch;
    };
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

// requireRefusalMentioning compiles one program that must be refused and finds
// the diagnostic saying every phrase asked for. The phrases are checked on ONE
// diagnostic, never spread across the bag: a program that says half the reason
// in one message and half in another has not explained itself.
func requireRefusalMentioning(t *testing.T, src string, contains []string) {
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
	for _, item := range res.Diagnose.Bag.Items() {
		matched := true
		for _, want := range contains {
			if !strings.Contains(item.Message, want) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("no diagnostic mentioning %v; got %s", contains, summarizeCodes(res.Diagnose.Bag.Items()))
}
