package vm_test

import (
	"strings"
	"testing"
)

// A task's result must outlive the activation that produced it.
//
// A poll function's return value is evaluated inside the poll frame and the
// frame is retired in the same terminator, so a value composite handed back
// from an async body names an arena that has already given its bytes up. The
// channel path solved this long ago -- a payload crossing a transport boundary
// is copied into storage the transport owns (`transport_storage.go`) -- and the
// TASK result was never wired to the same obligation, which is visible in the
// shutdown sweep itself: a resume value is released through `transportRelease`
// and a result through a bare `dropValue`.
//
// The two rows below differ in ONE condition, the width of the result:
//
//	composite: a struct owning a string. It does not fit anything but its own
//	           extent, so it is the value that names the retired arena.
//	scalar:    an int. It is a value, not a reference into any arena, so it
//	           was never exposed to this and must stay exactly as green as it
//	           was -- that is what makes the row above about the RESULT'S
//	           STORAGE rather than about async.
//
// The exit code alone is NOT the assertion here, and that is worth saying
// because it nearly cost this row its meaning. On the VM lane a program that
// dies of a VM error still reports `ExitCode` 0 -- `runVM` returns the code the
// VM holds alongside the error rather than instead of it -- so a row that reads
// only the code is green for a program that never reached its own checks. It
// was: against the unfixed tree this fixture exited 0 while the take raised
// `panic VM1999: storage: stale reference to type#4 at offset 0 (generation 1,
// arena is at 2)`. So each row asserts BOTH: an empty stderr, which only a
// program that ran can produce, and its own numbered check.
const runtimeV2VMTaskResultCompositeSource = `
type Box = { note: string, count: int };

async fn make_box(k: int) -> Box {
    let b: Box = { note = "carried past the producing activation", count = k };
    return b;
}

async fn run() -> int {
    let b: Box = compare make_box(7).await() {
        Success(v) => v;
        Cancelled() => { return 91; };
    };
    if b.count != 7 { return 2; }
    if b.note != "carried past the producing activation" { return 3; }
    print("vm-task-result-composite-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

const runtimeV2VMTaskResultScalarSource = `
async fn make_count(k: int) -> int {
    let n: int = k;
    return n;
}

async fn run() -> int {
    let c: int = compare make_count(7).await() {
        Success(v) => v;
        Cancelled() => { return 91; };
    };
    if c != 7 { return 2; }
    print("vm-task-result-scalar-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let t = spawn run();
    return compare t.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

// A1: a composite result read back after its producer's activation is gone.
func TestRuntimeV2VMTaskResultOutlivesItsProducer(t *testing.T) {
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			res := runProgramFromSource(t, runtimeV2VMTaskResultCompositeSource, runOptions{})
			if res.stderr != "" {
				t.Fatalf("composite task result did not survive its producer: the program reported\n%s", res.stderr)
			}
			if res.exitCode != 0 {
				t.Fatalf("composite task result did not survive its producer: exit %d\nstdout:\n%s\nstderr:\n%s",
					res.exitCode, res.stdout, res.stderr)
			}
			if backend == backendLLVM && !strings.Contains(res.stdout, "vm-task-result-composite-ok") {
				t.Fatalf("composite task result missing completion marker; stdout=%q", res.stdout)
			}
		})
	}
}

// A1-neg: the negative control. A scalar result was never carried by an arena,
// so this row is green on both sides of the fix; it fails only if the fix
// reached further than the storage the composite needed.
func TestRuntimeV2VMScalarTaskResultIsUnchanged(t *testing.T) {
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			res := runProgramFromSource(t, runtimeV2VMTaskResultScalarSource, runOptions{})
			if res.stderr != "" {
				t.Fatalf("scalar task result regressed: the program reported\n%s", res.stderr)
			}
			if res.exitCode != 0 {
				t.Fatalf("scalar task result regressed: exit %d\nstdout:\n%s\nstderr:\n%s",
					res.exitCode, res.stdout, res.stderr)
			}
			if backend == backendLLVM && !strings.Contains(res.stdout, "vm-task-result-scalar-ok") {
				t.Fatalf("scalar task result missing completion marker; stdout=%q", res.stdout)
			}
		})
	}
}
