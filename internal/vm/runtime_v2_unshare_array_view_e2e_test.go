package vm_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A view of an array at a crossing, and what stops it.
//
// A view is a fresh 24-byte header whose `data` points straight INTO the
// base's buffer. Its slots ARE the base's slots, so an un-share that rewrote
// them in place would hand the destination shard a private block by writing
// over storage a holder on the origin shard keeps reading. Sema cannot see
// this: a view's surface type is `T[]` whatever it was sliced from, and the
// checker's view markers do not survive into the crossing gates. So the
// refusal is the runtime's, taken under the array view registry lock by
// rt_array_unshare_walk and reported by name.
//
// The rows here are a matched pair: one shows the program dying on the
// refusal, and one shows the SAME program, built with the check cut, running
// to completion and walking the view's slots in the base's buffer. Without the
// second, the first would be satisfied by a program that simply never reached
// the walk at all.
//
// The refusal has a second half -- a BASE crossing while one of its views is
// still alive -- and it has rows here too, because a Surge program reaches it.
// Sema catches only the slice the mover can still see by name: measured on
// this tree, `let v: float[] = xs[[0..2]];` in the same frame as
// `blocking { ret total3(own xs); }` answers `error SEM3020 cannot move 'xs'
// while it is shared-borrowed`. The borrow behind that diagnostic ends with
// the frame that took it, so a slice handed BACK by a callee draws no
// diagnostic at all -- and neither does one carried out in a struct field,
// which reaches the same refusal at the same shard counts. The callee spelling
// is the one carried below, because it is the shorter of the two and they
// answer identically.

// Row (e-i): a view given to a blocking capture. It COMPILES -- sema sees an
// owned `float[]` -- and the process dies when the walk reads the header.
const runtimeV2UnshareArrayViewSource = `
fn total2(xs: own float[]) -> float {
    return xs[0] + xs[1];
}

async fn run() -> int {
    let a: float = 1.5;
    let xs: float[] = [a, a, a];
    let v: float[] = xs[[0..2]];
    let job: Task<float> = blocking { ret total2(own v); };
    let s: float = compare job.await() { Success(x) => x; Cancelled() => 0.0; };
    if s == 3.0 {
        print("array-unshare-ok");
        print(a to string);
        return 0;
    }
    print("FAIL s=");
    print(s to string);
    return 1;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

// Row (e-iii): the view is taken and dropped inside a callee, so it is gone by
// the time the base crosses. The refusal is about a view that is LIVE, not
// about a buffer that was ever sliced, and the base still walks.
const runtimeV2UnshareArrayAfterItsViewIsGoneSource = `
fn total3(xs: own float[]) -> float {
    return xs[0] + xs[1] + xs[2];
}

fn peek(xs: &float[]) -> int {
    let v: float[] = xs[[0..2]];
    return (v.__len() to int);
}

async fn run() -> int {
    let a: float = 1.5;
    let xs: float[] = [a, a, a];
    let seen: int = peek(&xs);
    let job: Task<float> = blocking { ret total3(own xs); };
    let s: float = compare job.await() { Success(x) => x; Cancelled() => 0.0; };
    if seen == 2 {
        if s == 4.5 {
            print("array-unshare-ok");
            print(a to string);
            return 0;
        }
    }
    print("FAIL seen=");
    print(seen to string);
    print(" s=");
    print(s to string);
    return 1;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

// Row (e-iv): the base crosses while a view of it is still alive. The slice is
// taken inside a callee and HANDED BACK, so the borrow sema tracks dies with
// the callee's frame and no diagnostic fires -- but the view header outlives
// it, is still registered against the base, and is still reading the buffer
// the crossing would rewrite. Only the runtime can see that, and it does.
const runtimeV2UnshareArrayWithALiveViewSource = `
fn total3(xs: own float[]) -> float {
    return xs[0] + xs[1] + xs[2];
}

fn slice2(xs: &float[]) -> float[] {
    return xs[[0..2]];
}

async fn run() -> int {
    let a: float = 1.5;
    let xs: float[] = [a, a, a];
    let v: float[] = slice2(&xs);
    let job: Task<float> = blocking { ret total3(own xs); };
    let s: float = compare job.await() { Success(x) => x; Cancelled() => 0.0; };
    let n: int = v.__len() to int;
    if s == 4.5 {
        if n == 2 {
            print("array-unshare-ok");
            print(s to string);
            return 0;
        }
    }
    print("FAIL s=");
    print(s to string);
    return 1;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

// runUnshareProgramExpectingRefusal runs a built program that is EXPECTED TO
// DIE and hands back what it said. The shared runUnshareProgram cannot serve
// here: it fatals on a non-zero exit, which is the outcome this row is about.
func runUnshareProgramExpectingRefusal(t *testing.T, outputPath string, shards int) (int, string, string) {
	t.Helper()
	env := envWithStdlib(repoRoot(t))
	env = overrideEnvVar(env, "SURGE_SHARDS", strconv.Itoa(shards))
	env = overrideEnvVar(env, "SURGE_THREADS", strconv.Itoa(shards))
	env = overrideEnvVar(env, "SURGE_TRACE_EXEC", "1")
	_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
	return result.exitCode, result.stdout, result.stderr
}

// The panic's own line carries a source location taken from the innermost
// Surge frame, and the temp path in it changes every run -- so the row pins
// that a location was printed, and pins the message itself word for word.
var runtimeV2ArrayViewPanicLocation = regexp.MustCompile(`(?m)^at .*\.sg:[0-9]+:[0-9]+$`)

const runtimeV2ArrayViewRefusalText = "panic VM1003: array view cannot cross a shard boundary: " +
	"its elements live in the base's buffer, which the origin shard keeps; cross an owned array instead"

const runtimeV2ArrayLiveViewRefusalText = "panic VM1003: array with a live view cannot cross a shard " +
	"boundary: a view on this shard still reads its buffer"

// assertUnshareArrayRefusal requires the program to die on the named refusal at
// both shard counts, with nothing on stdout: a refusal that let the body run
// first would have already handed the destination shard the storage it was
// meant to protect.
func assertUnshareArrayRefusal(t *testing.T, source, wantText string) {
	t.Helper()
	outputPath := buildRuntimeV2CrossingSource(t, source, nil)
	for _, shards := range []int{2, 8} {
		exitCode, stdout, stderr := runUnshareProgramExpectingRefusal(t, outputPath, shards)
		if exitCode != 1 {
			t.Fatalf("shards=%d: exit = %d, want 1 (the crossing must not be allowed to happen)\nstdout:\n%s\nstderr:\n%s",
				shards, exitCode, stdout, stderr)
		}
		if strings.Contains(stdout, "array-unshare-ok") {
			t.Fatalf("shards=%d: the body ran and printed its marker, so the crossing happened\nstdout:\n%s", shards, stdout)
		}
		if !strings.Contains(stderr, wantText) {
			t.Fatalf("shards=%d: stderr does not carry the refusal by name\nwant substring:\n%s\ngot:\n%s",
				shards, wantText, stderr)
		}
		if !runtimeV2ArrayViewPanicLocation.MatchString(stderr) {
			t.Fatalf("shards=%d: the refusal named no source location\nstderr:\n%s", shards, stderr)
		}
	}
}

func TestRuntimeV2UnshareOfAnArrayViewIsRefusedByTheRuntime(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareArrayViewSource, runtimeV2ArrayViewRefusalText)
}

func TestRuntimeV2UnshareOfABaseWithALiveViewIsRefusedByTheRuntime(t *testing.T) {
	assertUnshareArrayRefusal(t, runtimeV2UnshareArrayWithALiveViewSource, runtimeV2ArrayLiveViewRefusalText)
}

// The Rule-13 red for the two rows above, and the whole reason to trust them.
// The negative control cuts the registry CHECK and leaves the walk, so the same
// programs survive: each prints its marker and reports the clones its walk
// cost, taken IN THE BASE'S BUFFER while a holder on this shard was still
// reading it. That is the work the refusals prevent; without these rows, a
// program that died on its way to the walk for any other reason would look
// identical. The two counts differ because the two programs cross different
// things -- the view's own two slots, and all three of the base's.
func TestRuntimeV2UnshareOfAnArrayViewNegativeControl(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL", "RV2_ARRAY_UNSHARE_WALK_NEGATIVE_CONTROL")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareArrayViewSource, nil)
	values, line := runUnshareProgram(t, outputPath, 2)
	t.Logf("%s", line)
	if clones := unshareClonesField(t, values, line); clones != 2 {
		t.Fatalf("negative control: unshare_clones = %d, want 2 (the view's two slots, walked in the "+
			"base's buffer with the registry check cut):\n%s", clones, line)
	}
}

func TestRuntimeV2UnshareOfABaseWithALiveViewNegativeControl(t *testing.T) {
	t.Setenv("SURGE_INTERNAL_RUNTIME_NEGATIVE_CONTROL", "RV2_ARRAY_UNSHARE_WALK_NEGATIVE_CONTROL")
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareArrayWithALiveViewSource, nil)
	values, line := runUnshareProgram(t, outputPath, 2)
	t.Logf("%s", line)
	if clones := unshareClonesField(t, values, line); clones != 3 {
		t.Fatalf("negative control: unshare_clones = %d, want 3 (the base's three slots, walked with the "+
			"registry check cut while the callee's view still points at them):\n%s", clones, line)
	}
}

func TestRuntimeV2UnshareOfABaseWalksAfterItsViewIsGone(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2UnshareArrayAfterItsViewIsGoneSource, nil)
	for _, shards := range []int{2, 8} {
		values, line := runUnshareProgram(t, outputPath, shards)
		t.Logf("shards=%d %s", shards, line)
		if clones := unshareClonesField(t, values, line); clones != 3 {
			t.Fatalf("shards=%d: unshare_clones = %d, want 3 (the base walks once its view has died "+
				"with the callee's frame):\n%s", shards, clones, line)
		}
		if underflows := values["underflows"]; underflows != 0 {
			t.Fatalf("shards=%d: %d releases outran their acquires:\n%s", shards, underflows, line)
		}
	}
}
