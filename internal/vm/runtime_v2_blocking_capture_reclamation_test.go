package vm_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// What a blocking job's captures cost, measured rather than assumed.
//
// A blocking submission packs its captures into a block compiled code reserves
// and the job holds. That block used to be described to the runtime by a
// pointer and two integers, and two integers can free storage but cannot
// destroy what is inside it: the release was `rt_free` over the block, so every
// capture still in it was abandoned, and a block of size ZERO -- a body that
// captures nothing -- was not freed at all, because the release was guarded on
// a nonzero size.
//
// The job owns the captures in an `rt_value_cell` now, bound to the type's own
// descriptor, and destroys them through it.
//
// WHAT THESE ROWS CAN AND CANNOT SEE. A body that CONSUMES its captures settles
// them itself, and that is the shape driven here, because it is the shape whose
// every block has a named owner today. A body that merely READS an owned
// capture still abandons it: blocking bodies record no drop obligations at all
// (`dropObligationsSuppressed`), so the body's local has no scope-exit release
// to run. That is the other half of RV2-DEBT-080 and it is not what these rows
// pin -- a row asserting zero over it would be asserting a fix nobody has made.

// One submission, no captures at all. The state block is a zero-sized
// allocation, which `rt_alloc` still answers with one byte, and nothing ever
// gave it back.
const runtimeV2BlockingCapturelessSource = `
async fn run() -> int {
    let job: Task<int> = blocking { ret 42; };
    return compare job.await() {
        Success(v) => v;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != 42 {
        print("FAIL blocking captureless answer");
        return 1;
    }
    print("blocking-captureless-witness");
    return 0;
}
`

// The RV2-DEBT-080 reproducer's shape: a `@shard_movable` struct carrying a
// two-hundred-character string, moved into a blocking body, at two different
// iteration counts. The row the debt records was 219 bytes in ONE block at both
// n=1 and n=8, which is why it was filed as "not a per-execution leak" and left
// unattributed; running the same program at both counts is what turns that into
// a fact about a named site rather than a number.
const runtimeV2BlockingCaptureSource = `
@shard_movable
type Note = { id: int, text: string };

// The body hands the capture ON, which is the whole point of moving one in, so
// this callee is what reclaims it. Nothing the job does may come back for it.
fn sink(n: own Note) -> int {
    return n.id;
}

fn wide() -> string {
    let mut s: string = "";
    let mut i: int = 0;
    while i < 20 {
        s = s + "0123456789";
        i = i + 1;
    }
    return s;
}

async fn run() -> int {
    let mut total: int = 0;
    let mut i: int = 0;
    while i < $ROUNDS$ {
        let note: Note = Note { id = 1, text = wide() };
        let job: Task<int> = blocking { ret sink(own note); };
        total = total + compare job.await() {
            Success(v) => v;
            Cancelled() => 0;
        };
        i = i + 1;
    }
    return total;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    let total: int = compare t.await() {
        Success(v) => v;
        Cancelled() => 90;
    };
    if total != $ROUNDS$ {
        print("FAIL blocking capture rounds");
        return 1;
    }
    print("blocking-capture-witness");
    return 0;
}
`

func blockingCaptureProgram(rounds int) string {
	return strings.ReplaceAll(runtimeV2BlockingCaptureSource, "$ROUNDS$", strconv.Itoa(rounds))
}

// runBlockingReclamationProgram builds one source, runs it under valgrind, and
// demands strict zero plus the completion marker. The marker matters as much as
// the leak count: a program that exited before the blocking body ran would leak
// nothing and prove nothing.
func runBlockingReclamationProgram(t *testing.T, source, marker string) {
	t.Helper()
	outputPath := buildRuntimeV2CrossingSource(t, source, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 180*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read) -- a state destroyed twice looks exactly like this\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("blocking capture reclamation e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, marker) {
		t.Fatalf("missing completion marker %q; stdout=%q", marker, stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf(
			"a blocking job abandoned its captured state: got %d bytes in %d blocks definitely lost, want strict zero\nstderr:\n%s",
			bytesLost, blocksLost, stderr,
		)
	}
}

// A body that captures nothing still gets a state block, and it was never
// freed: the release asked whether the recorded size was nonzero, and a
// zero-sized state answers no. The cell owns the block whatever its width, so
// there is nothing left to ask.
func TestRuntimeV2BlockingCapturelessStateIsFreed(t *testing.T) {
	runBlockingReclamationProgram(t, runtimeV2BlockingCapturelessSource, "blocking-captureless-witness")
}

// The same program at one iteration and at eight. Strict zero at both is what
// separates a per-execution loss from a constant one, which RV2-DEBT-080
// records as unprobed: at n=1 a per-execution leak and a one-off leak are the
// same number, and only the second count tells them apart.
func TestRuntimeV2BlockingCaptureValgrindZero(t *testing.T) {
	for _, rounds := range []int{1, 8} {
		t.Run("rounds="+strconv.Itoa(rounds), func(t *testing.T) {
			runBlockingReclamationProgram(t, blockingCaptureProgram(rounds), "blocking-capture-witness")
		})
	}
}
