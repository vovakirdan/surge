package vm_test

import (
	"strings"
	"testing"
)

// `ret` gives an async/blocking body its value (owner ruling 2026-08-26,
// RV2-DEBT-161). The program nests every shape the ruling distinguishes — a
// `ret` from inside an `if` in a loop (leaves the whole body), a block
// expression's own `ret` inside the body (yields the block, not the body), a
// compare arm's block inside the body (yields the arm), a nested body with
// its own exit, a body with no `ret` at all (yields nothing and falls off its
// end), and a bare `ret;` from inside a loop.
//
// Each valued shape is checked INSIDE the program and returns its own row
// number, so a `ret` that left the wrong block is a non-zero exit code rather
// than a wrong line of output: the VM runner does not capture the program's
// stdout, and an assertion that reads it would be vacuous on that lane. The
// two bodies worth nothing are witnessed by the program terminating at all —
// `early` loops `while true` and only a `ret;` that leaves the body ends it.
// It runs on both lanes: the lowering is one MIR shape.
const runtimeV2RetInAsyncBodiesSource = `
fn unwrap(r: TaskResult<int>) -> int {
    return compare r {
        Success(v) => v;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    let outer: Task<int> = async {
        let inner: Task<int> = spawn async {
            // The block expression's ret yields the block: v = 5.
            let v: int = { ret 5; };
            checkpoint().await();
            // The body's ret yields the body: inner = 6.
            ret v + 1;
        };
        let x: int = unwrap(inner.await());
        // A ret inside an if inside a loop leaves the whole body.
        let mut i: int = 0;
        while i < 10 {
            if i == 3 {
                ret x * 10 + i;
            }
            i = i + 1;
        }
        ret 0;
    };
    let outer_v: int = unwrap(outer.await());
    print("outer=" + (outer_v to string));
    if outer_v != 63 {
        return 1;
    }

    // The arm's block keeps its own ret; the body decides afterwards.
    let picked: Task<int> = async {
        let t: Task<int> = spawn async { ret 21; };
        let doubled: int = compare t.await() {
            Success(v) => { ret v * 2; };
            Cancelled() => -1;
        };
        if doubled < 0 {
            ret -100;
        }
        ret doubled;
    };
    let picked_v: int = unwrap(picked.await());
    print("picked=" + (picked_v to string));
    if picked_v != 42 {
        return 2;
    }

    // No ret: the body yields nothing and reaches its exit by falling off.
    let quiet: Task<nothing> = async {
        print("quiet");
    };
    let _ = quiet.await();

    // A bare ret is nothing too, from inside a loop.
    let early: Task<nothing> = async {
        let mut n: int = 0;
        while true {
            n = n + 1;
            if n == 2 {
                print("early=" + (n to string));
                ret;
            }
        }
    };
    let _ = early.await();
    print("ret-in-async-bodies-witness");
    return 0;
}
`

const runtimeV2RetInAsyncBodiesWant = "outer=63\npicked=42\nquiet\nearly=2\nret-in-async-bodies-witness\n"

func TestRuntimeV2RetGivesAnAsyncBodyItsValue(t *testing.T) {
	res := runProgramFromSource(t, runtimeV2RetInAsyncBodiesSource, runOptions{})
	// The exit code IS the assertion on both lanes: the program returns the
	// row number of the first `ret` that carried the wrong value, and 0 only
	// after every row agreed. A fatal VM error is an exit code the runner
	// reports as a non-empty stderr, which is why both are checked.
	if res.exitCode != 0 || res.stderr != "" {
		t.Fatalf("a ret left the wrong block or carried the wrong value: row %d\nstdout:\n%s\nstderr:\n%s", res.exitCode, res.stdout, res.stderr)
	}
	// The printed lines are compared only where stdout exists: the VM runner
	// executes the module in-process and never captures it, so comparing it
	// there would fail on an empty string no matter what the program did.
	if testBackend(t) == backendLLVM && res.stdout != runtimeV2RetInAsyncBodiesWant {
		t.Fatalf("the printed values do not match\nstdout:\n%s\nwant:\n%s\nstderr:\n%s", res.stdout, runtimeV2RetInAsyncBodiesWant, res.stderr)
	}
}

// The blocking body has the same exit shape, on the lane that runs it: the
// VM refuses `blocking` before running (RV2-DEBT-162), so only the native
// lane can witness the value.
const runtimeV2RetInBlockingBodiesSource = `
fn unwrap(r: TaskResult<int>) -> int {
    return compare r {
        Success(v) => v;
        Cancelled() => -1;
    };
}

async fn run() -> int {
    let seed: int = 4;
    let job: Task<int> = blocking {
        let v: int = { ret seed + 1; };
        let mut i: int = 0;
        while i < 10 {
            if i == v {
                ret i * 100;
            }
            i = i + 1;
        }
        ret -1;
    };
    return unwrap(job.await());
}

@entrypoint
fn main() -> int {
    let got: int = compare run().await() {
        Success(v) => v;
        Cancelled() => -1;
    };
    print("blocking=" + (got to string));
    return 0;
}
`

func TestRuntimeV2RetGivesABlockingBodyItsValue(t *testing.T) {
	requireLLVMBackend(t)
	res := runProgramFromSource(t, runtimeV2RetInBlockingBodiesSource, runOptions{})
	if res.exitCode != 0 || res.stderr != "" {
		t.Fatalf("program exited %d\nstdout:\n%s\nstderr:\n%s", res.exitCode, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "blocking=500\n") {
		t.Fatalf("a ret left the wrong block or carried the wrong value\nstdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	}
}
