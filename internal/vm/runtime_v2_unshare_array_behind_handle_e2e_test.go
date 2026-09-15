package vm_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"surge/internal/buildpipeline"
	"surge/internal/diag"
)

// The two view routes the buffer walk cannot serve, and what happens to them
// now.
//
// The rows next door hand the runtime an array's slot and let its view registry
// answer. That works wherever the walk can address the array. It cannot address
// one stored in a MAP'S TABLE or a CHANNEL'S RING: those keep their entries at
// offsets no per-element callback is ever handed, so the crossing emitted
// nothing for them and the registry was never asked.
//
// Measured on this tree at 273ca202, with the walk already landed: the two
// programs below COMPILED, and each printed 7770777 at SURGE_SHARDS/THREADS 2
// and 8 -- the worker thread took the view out of the container and wrote 777
// through it into slot 1 of the base the origin shard was still reading, which
// the second half of that number is the origin reading back. Neither module
// contained a single `rt_array_unshare_walk` call site.
//
// They are refused at COMPILE time now, which is the split the owner chose for
// this feature: the runtime is fail-closed where it can see the header, and the
// compiler is fail-closed where nothing ever will. So these rows cannot have
// the negative control the int rows next door have -- that control cuts the
// runtime's registry check, and there is no run left to cut. The control here
// is the pairing below: the same programs with the array taken OUT of the
// container still build, still cross, and still arrive with their values.
// Counted int now meets the earlier counted-payload barrier. The original
// programs remain refusal witnesses; explicit int64 counterparts below isolate
// the array barrier and preserve the executable controls without an array.

// Row (xii): a view stored in a map's table, the map captured into `blocking`.
const runtimeV2UnshareIntArrayViewInAMapSource = `
fn bump_map(m: own Map<int, int[]>) -> int {
    let mut mm: Map<int, int[]> = own m;
    let k: int = 0;
    return compare mm.remove(&k) {
        Some(a) => { let mut zs: int[] = own a; zs[0] = 777; ret zs[0]; };
        nothing => 0 - 3;
    };
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let mut m: Map<int, int[]> = Map::<int, int[]>.new();
    let k: int = 0;
    let _ = m.insert(k, own v);
    let job: Task<int> = blocking { ret bump_map(own m); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + base[1]) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// Row (xiii): a view put in a local channel's ring by a same-shard send -- no
// crossing there, nothing to check -- and then the channel HANDLE captured into
// `blocking`.
const runtimeV2UnshareIntArrayViewInAChannelSource = `
fn drain(c: own Channel<int[]>) -> int {
    let mut cc: Channel<int[]> = own c;
    return compare cc.try_recv() {
        Some(a) => { let mut zs: int[] = own a; zs[0] = 777; ret zs[0]; };
        nothing => 0 - 3;
    };
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let ch: Channel<int[]> = Channel::<int[]>::new(4:uint);
    let _ = ch.try_send(own v);
    let job: Task<int> = blocking { ret drain(own ch); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + base[1]) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// The original control for row (xii): the SAME crossing, map, and worker
// -- with the array's value in the table instead of the array. Its int64 twin prints
// 20002: the worker read 2 out of the table, and `base[1]` is still the 2 the
// origin put there.
const runtimeV2UnshareIntMapStillCrossesSource = `
fn read_map(m: own Map<int, int>) -> int {
    let mut mm: Map<int, int> = own m;
    let k: int = 0;
    return compare mm.remove(&k) { Some(x) => x; nothing => 0 - 3; };
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let mut m: Map<int, int> = Map::<int, int>.new();
    let k: int = 0;
    let _ = m.insert(k, v[0]);
    let job: Task<int> = blocking { ret read_map(own m); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + base[1]) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// The original control for row (xiii), by the same construction through a
// channel's ring. Its int64 twin prints the same 20002.
const runtimeV2UnshareIntChannelStillCrossesSource = `
fn drain_ints(c: own Channel<int>) -> int {
    let mut cc: Channel<int> = own c;
    return compare cc.try_recv() { Some(x) => x; nothing => 0 - 3; };
}

async fn run() -> int {
    let base: int[] = [1, 2, 3, 4];
    let v: int[] = base[[1..3]];
    let ch: Channel<int> = Channel::<int>::new(4:uint);
    let seen: int = v[0];
    let _ = ch.try_send(seen);
    let job: Task<int> = blocking { ret drain_ints(own ch); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + base[1]) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// Explicit plain payload counterparts isolate the array-header barrier from
// the earlier counted-leaf barrier, and keep the no-array crossings executable.
const runtimeV2UnshareInt64ArrayViewInAMapSource = `
fn bump_map(m: own Map<int64, int64[]>) -> int {
    let mut mm: Map<int64, int64[]> = own m;
    let k: int64 = 0;
    return compare mm.remove(&k) {
        Some(a) => { let mut zs: int64[] = own a; zs[0] = 777; ret zs[0] to int; };
        nothing => 0 - 3;
    };
}

async fn run() -> int {
    let base: int64[] = [1, 2, 3, 4];
    let v: int64[] = base[[1..3]];
    let mut m: Map<int64, int64[]> = Map::<int64, int64[]>.new();
    let k: int64 = 0;
    let _ = m.insert(k, own v);
    let job: Task<int> = blocking { ret bump_map(own m); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + (base[1] to int)) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

const runtimeV2UnshareInt64ArrayViewInAChannelSource = `
fn drain(c: own Channel<int64[]>) -> int {
    let mut cc: Channel<int64[]> = own c;
    return compare cc.try_recv() {
        Some(a) => { let mut zs: int64[] = own a; zs[0] = 777; ret zs[0] to int; };
        nothing => 0 - 3;
    };
}

async fn run() -> int {
    let base: int64[] = [1, 2, 3, 4];
    let v: int64[] = base[[1..3]];
    let ch: Channel<int64[]> = Channel::<int64[]>::new(4:uint);
    let _ = ch.try_send(own v);
    let job: Task<int> = blocking { ret drain(own ch); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + (base[1] to int)) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

const runtimeV2UnshareInt64MapStillCrossesSource = `
fn read_map(m: own Map<int64, int64>) -> int {
    let mut mm: Map<int64, int64> = own m;
    let k: int64 = 0;
    return compare mm.remove(&k) { Some(x) => x to int; nothing => 0 - 3; };
}

async fn run() -> int {
    let base: int64[] = [1, 2, 3, 4];
    let v: int64[] = base[[1..3]];
    let mut m: Map<int64, int64> = Map::<int64, int64>.new();
    let k: int64 = 0;
    let _ = m.insert(k, v[0]);
    let job: Task<int> = blocking { ret read_map(own m); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + (base[1] to int)) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

const runtimeV2UnshareInt64ChannelStillCrossesSource = `
fn drain_ints(c: own Channel<int64>) -> int {
    let mut cc: Channel<int64> = own c;
    return compare cc.try_recv() { Some(x) => x to int; nothing => 0 - 3; };
}

async fn run() -> int {
    let base: int64[] = [1, 2, 3, 4];
    let v: int64[] = base[[1..3]];
    let ch: Channel<int64> = Channel::<int64>::new(4:uint);
    let seen: int64 = v[0];
    let _ = ch.try_send(seen);
    let job: Task<int> = blocking { ret drain_ints(own ch); };
    let r: int = compare job.await() { Success(x) => x; Cancelled() => 0 - 2; };
    print("array-unshare-ok");
    print(((r * 10000) + (base[1] to int)) to string);
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`

// assertCrossingRefusedAtCompileTime requires the program to be turned away by
// sema, with a diagnostic that names the inaccessible stored value. Compiling it and
// finding no binary would say the same thing for a typo, so the message is
// pinned along with the code.
func assertCrossingRefusedAtCompileTime(t *testing.T, source string, wantPhrases []string) {
	t.Helper()
	root := repoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	sourcePath := filepath.Join(t.TempDir(), "refused.sg")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write refused source: %v", err)
	}
	result, err := buildpipeline.Compile(context.Background(), &buildpipeline.CompileRequest{
		TargetPath:     sourcePath,
		BaseDir:        root,
		MaxDiagnostics: 200,
	})
	if err == nil {
		t.Fatal("the program compiled; expected the container's crossing to be refused")
	}
	if result.Diagnose == nil || result.Diagnose.Bag == nil {
		t.Fatalf("compile failed without diagnostics: %v", err)
	}
	errors, matches := 0, 0
	var got []string
	for _, item := range result.Diagnose.Bag.Items() {
		if item.Severity != diag.SevError {
			continue
		}
		errors++
		got = append(got, fmt.Sprintf("%v %s", item.Code, item.Message))
		if item.Code != diag.SemaCrossNotShardMovable {
			continue
		}
		matched := true
		for _, want := range wantPhrases {
			if !strings.Contains(item.Message, want) {
				matched = false
				break
			}
		}
		if matched {
			matches++
		}
	}
	if errors != 1 || matches != 1 {
		t.Fatalf("want exactly one SEM3168 saying %v; got %d errors:\n%s", wantPhrases, errors, strings.Join(got, "\n"))
	}
}

// assertCrossingStillArrives builds a control and runs it at both shard counts,
// requiring the exact number the program prints. The number is
// `remote * 10000 + base[1]`, so a row cannot pass on the remote half alone and
// the origin's own buffer is read after the crossing either way.
func assertCrossingStillArrives(t *testing.T, source string, wantCode int) {
	t.Helper()
	outputPath := buildRuntimeV2CrossingSource(t, source, nil)
	want := strconv.Itoa(wantCode)
	for _, shards := range []int{2, 8} {
		env := envWithStdlib(repoRoot(t))
		env = overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(shards))
		env = overrideEnvVar(env, "SURGE_THREADS", strconv.Itoa(shards))
		duration, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
		if result.exitCode != 0 {
			t.Fatalf("shards=%d: exit = %d, want 0 -- the widening refused a crossing that carries no view (duration=%s)\nstdout:\n%s\nstderr:\n%s",
				shards, result.exitCode, duration, result.stdout, result.stderr)
		}
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("shards=%d: stdout does not carry %s (remote*10000 + base[1])\nstdout:\n%s",
				shards, want, result.stdout)
		}
	}
}

func TestRuntimeV2UnshareOfAnIntArrayViewInAMapIsRefusedAtCompileTime(t *testing.T) {
	t.Run("plain64_array_refused", func(t *testing.T) {
		assertCrossingRefusedAtCompileTime(t, runtimeV2UnshareInt64ArrayViewInAMapSource, []string{
			"`Map<int64, [int64]>` cannot be captured into `blocking`",
			"holds a dynamic array in storage this thread keeps", "map's table",
		})
	})
	assertCrossingRefusedAtCompileTime(t, runtimeV2UnshareIntArrayViewInAMapSource, []string{
		"`Map<int, [int]>` cannot be captured into `blocking`",
		"arbitrary-precision `int` at `key`",
	})
}

func TestRuntimeV2UnshareOfAnIntArrayViewInAChannelIsRefusedAtCompileTime(t *testing.T) {
	t.Run("plain64_array_refused", func(t *testing.T) {
		assertCrossingRefusedAtCompileTime(t, runtimeV2UnshareInt64ArrayViewInAChannelSource, []string{
			"`Channel<[int64]>` cannot be captured into `blocking`",
			"never shown that array's header",
		})
	})
	assertCrossingRefusedAtCompileTime(t, runtimeV2UnshareIntArrayViewInAChannelSource, []string{
		"`Channel<[int]>` cannot be captured into `blocking`",
		"arbitrary-precision `int` at `payload[0].element`",
	})
}

func TestRuntimeV2UnshareInt64MapWithoutAnArrayStillCrosses(t *testing.T) {
	t.Run("counted_int_refused", func(t *testing.T) {
		assertCrossingRefusedAtCompileTime(t, runtimeV2UnshareIntMapStillCrossesSource, []string{
			"`Map<int, int>` cannot be captured into `blocking`",
			"arbitrary-precision `int` at `key`",
		})
	})
	assertCrossingStillArrives(t, runtimeV2UnshareInt64MapStillCrossesSource, 20002)
}

func TestRuntimeV2UnshareInt64ChannelWithoutAnArrayStillCrosses(t *testing.T) {
	t.Run("counted_int_refused", func(t *testing.T) {
		assertCrossingRefusedAtCompileTime(t, runtimeV2UnshareIntChannelStillCrossesSource, []string{
			"`Channel<int>` cannot be captured into `blocking`",
			"arbitrary-precision `int` at `payload[0]`",
		})
	})
	assertCrossingStillArrives(t, runtimeV2UnshareInt64ChannelStillCrossesSource, 20002)
}
