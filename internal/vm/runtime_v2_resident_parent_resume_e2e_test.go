package vm_test

import (
	"strings"
	"testing"
)

// RV2-DEBT-372 end to end. A child task that borrows a local of an async parent reads it
// through a location into the parent's state (the local is a `__resident$` field:
// internal/mir/async_resident_places.go). The VM moved that state into a fresh arena at
// every resume of the parent and zeroed the old bytes, so a child whose read came after a
// parent resume read zeros: `nothing` for a counted or handle-backed local, 0 or false --
// silently -- for a fixed-width integer or a bool. Native code reads the one heap frame and
// was always right. docs/RUNTIME_V2.md, owner ruling 2026-09-02, and section 7 of the
// storage model require the state to keep one address; these programs pin that it does.
//
// Every program exits 0 with nothing printed when the child reads the value the parent
// wrote. The rows differ in where the suspensions are, because the read is wrong exactly
// when a resume of the parent falls between the borrow and the read: the task is created
// cold (RV2-DEBT-370) and first runs at the parent's await.

const residentResumeMain = `
@entrypoint
fn main() -> int {
    let r = (async {
        ret compare run().await() {
            Success(n) => n;
            Cancelled() => 101;
        };
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

// r0: no suspension before the read -- the RV2-DEBT-369 shape; right before and after.
const residentResumeNeitherSource = `async fn sworker(x: &string) -> int {
    return len(x) to int;
}

async fn run() -> int {
    let l: string = "abcdef";
    let t = sworker(&l);
    return compare t.await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
` + residentResumeMain

// r1: the parent suspends and resumes before its await; the child reads at once when it runs.
const residentResumeParentOnlySource = `async fn sworker(x: &string) -> int {
    return len(x) to int;
}

async fn run() -> int {
    let l: string = "abcdef";
    let t = sworker(&l);
    checkpoint().await();
    return compare t.await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
` + residentResumeMain

// r2: only the child suspends; the parent is parked on the join and never resumes before the read.
const residentResumeChildOnlySource = `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

async fn run() -> int {
    let l: string = "abcdef";
    let t = sworker(&l);
    return compare t.await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
` + residentResumeMain

// r3: the measured program: both suspend. The VM stopped with VM1003 in `len::<string>`.
const residentResumeBothSource = `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

async fn run() -> int {
    let l: string = "abcdef";
    let t = sworker(&l);
    checkpoint().await();
    return compare t.await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}
` + residentResumeMain

// r4: a fixed-width integer: the zeroed cell reads 0 and the program exits 7, with no fault.
const residentResumeInt64Source = `async fn iworker(x: &int64) -> int64 {
    checkpoint().await();
    return *x;
}

async fn run() -> int {
    let l: int64 = 7;
    let t = iworker(&l);
    checkpoint().await();
    return compare t.await() {
        Success(n) => 7 - (n to int);
        Cancelled() => 100;
    };
}
` + residentResumeMain

// r5: a bool: the zeroed cell reads false and the program exits 5, with no fault.
const residentResumeBoolSource = `async fn bworker(x: &bool) -> int {
    checkpoint().await();
    if *x {
        return 0;
    }
    return 5;
}

async fn run() -> int {
    let l: bool = true;
    let t = bworker(&l);
    checkpoint().await();
    return compare t.await() {
        Success(n) => n;
        Cancelled() => 100;
    };
}
` + residentResumeMain

// r6: runtimeV2CarrierAffineResidentSource (runtime_v2_carrier_affine_acceptance_e2e_test.go),
// the resident row that has only ever run natively, in the form that builds on this line: its
// `spawn` is a plain call (the spawn form is N-TASK-16's) and its closing print is gone.
const residentResumeCarrierAffineSource = `async fn reader(x: &int) -> int {
    let mut i = 0;
    while i < 8 {
        checkpoint().await();
        i = i + 1;
    }
    return *x + 1;
}

async fn main_async() -> int {
    let v: int = 41;
    let t = reader(&v);
    let mut j = 0;
    while j < 8 {
        checkpoint().await();
        j = j + 1;
    }
    let r = t.await();
    let got = compare r {
        Success(n) => n;
        Cancelled() => -1;
    };
    if got != 42 {
        return 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let r = main_async().await();
    let code = compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
    return code;
}
`

// r8: a child that writes through `&mut` after the parent resumed; the write must reach the
// parent's local. Conditional: the row stands only while the task check admits the borrow.
const residentResumeMutSource = `async fn mworker(x: &mut int64) -> int {
    checkpoint().await();
    *x = 9;
    return 0;
}

async fn run() -> int {
    let mut l: int64 = 7;
    let t = mworker(&mut l);
    checkpoint().await();
    let done = compare t.await() {
        Success(n) => n;
        Cancelled() => 100;
    };
    return done + 9 - (l to int);
}
` + residentResumeMain

// r9: the parent is cancelled from outside -- by the timeout around it -- while it is parked
// on the join and the child is parked in its sleep. The cancel wakes the parent first; it
// ends Cancelled at its yield and its state is released while the child still holds `&l`.
// The child, woken by the same cancel, reaches its own yield holding a location into
// storage that is gone and must end Cancelled too: holding the location is not a use of it.
// The child sleeps rather than checkpoints on purpose: a checkpoint yields without parking,
// so the child would be polled, and would end, before its parent.
const residentResumeParentCancelledSource = `async fn sworker(x: &string) -> int {
    sleep(1000:uint).await();
    return len(x) to int;
}

async fn run() -> int {
    let l: string = "abcdef";
    let t = sworker(&l);
    return compare t.await() {
        Success(n) => n - 6;
        Cancelled() => 100;
    };
}

@entrypoint
fn main() -> int {
    let r = (async {
        ret compare timeout(run(), 5:uint) {
            Success(n) => 20 + n;
            Cancelled() => 0;
        };
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 102;
    };
}
`

var residentResumeRows = []struct {
	name, source string
}{
	{"r0_neither", residentResumeNeitherSource},
	{"r1_parent_checkpoint_only", residentResumeParentOnlySource},
	{"r2_child_checkpoint_only", residentResumeChildOnlySource},
	{"r3_both", residentResumeBothSource},
	{"r4_int64_silent", residentResumeInt64Source},
	{"r5_bool_silent", residentResumeBoolSource},
	{"r6_carrier_affine_resident", residentResumeCarrierAffineSource},
	{"r8_mut_write_reaches_the_parent", residentResumeMutSource},
	{"r9_parent_cancelled_child_parked", residentResumeParentCancelledSource},
}

func checkResidentResumeRow(t *testing.T, source string) {
	t.Helper()
	res := runProgramFromSource(t, source, runOptions{captureStdout: true})
	if res.exitCode != 0 || res.stdout != "" || res.stderr != "" {
		t.Fatalf("PVMR-ROW exit=%d stdout=%q stderr=%q (want exit 0 and no output)",
			res.exitCode, res.stdout, strings.TrimSpace(res.stderr))
	}
}

func TestRuntimeV2ResidentBorrowSurvivesParentResume(t *testing.T) {
	for _, row := range residentResumeRows {
		t.Run(row.name, func(t *testing.T) {
			skipTimeoutTests(t)
			checkResidentResumeRow(t, row.source)
		})
	}
}

// The same programs on both backends in one run, whichever backend the roster selects:
// native reads the one heap frame, and the VM must now agree with it.
func TestRuntimeV2ResidentBorrowParityBothBackends(t *testing.T) {
	for _, row := range residentResumeRows {
		switch row.name {
		case "r3_both", "r4_int64_silent", "r6_carrier_affine_resident", "r9_parent_cancelled_child_parked":
		default:
			continue
		}
		for _, backend := range []string{backendVM, backendLLVM} {
			t.Run(row.name+"/"+backend, func(t *testing.T) {
				skipTimeoutTests(t)
				t.Setenv(backendEnvVar, backend)
				checkResidentResumeRow(t, row.source)
			})
		}
	}
}
