package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// D4.8: the carrier-affinity acceptance at 2, 4 and 8 carriers (SURGE_SHARDS=1
// is the only topology with several carriers on one shard). Each program is a
// compiled borrow spawn in one of the shapes the plan names, and each run is
// read through the scheduler trace: the borrowing child is pinned, nothing
// pinned is left for an exiting carrier to cancel, and -- the assertion the
// runtime makes on every poll -- no pinned task was ever polled off its
// carrier (rt_async_poll.c panics, and the run would not answer).

// The parent suspends and resumes between the spawn and the join while the
// child still holds a pointer into the parent's activation: the borrowed
// place is resident storage, so the address the child reads after its own
// suspensions is the same one the parent wrote.
const runtimeV2CarrierAffineResidentSource = `async fn reader(x: &int) -> int {
    let mut i = 0;
    while i < 8 {
        checkpoint().await();
        i = i + 1;
    }
    return *x + 1;
}

async fn main_async() -> int {
    let v: int = 41;
    let t = spawn reader(&v);
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
    print("ok");
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

// The child yields many times, reading the borrowed place at every turn: each
// requeue must land it back on its carrier, and each read must see the
// parent's value.
const runtimeV2CarrierAffineYieldSource = `async fn reader(x: &int) -> int {
    let mut i = 0;
    let mut sum = 0;
    while i < 64 {
        checkpoint().await();
        sum = sum + *x;
        i = i + 1;
    }
    return sum;
}

async fn main_async() -> int {
    let v: int = 1;
    let t = spawn reader(&v);
    let r = t.await();
    let got = compare r {
        Success(n) => n;
        Cancelled() => -1;
    };
    if got != 64 {
        return 1;
    }
    print("ok");
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

// The parent cancels its borrowing child and joins it: the child answers
// Cancelled on its carrier, and nothing is left for the carrier's exit.
const runtimeV2CarrierAffineCancelSource = `async fn reader(x: &int) -> int {
    let mut i = 0;
    while i < 100000 {
        checkpoint().await();
        i = i + *x;
    }
    return i;
}

async fn main_async() -> int {
    let v: int = 1;
    let t = spawn reader(&v);
    checkpoint().await();
    t.cancel();
    let r = t.await();
    let cancelled = compare r {
        Success(_) => false;
        Cancelled() => true;
    };
    if !cancelled {
        return 1;
    }
    print("ok");
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

// A child that borrows nothing is not pinned: it takes its argument by value
// and may run on any carrier.
const runtimeV2CarrierIndependentSource = `async fn plus_one(x: int) -> int {
    checkpoint().await();
    return x + 1;
}

async fn main_async() -> int {
    let v: int = 41;
    let t = spawn plus_one(v);
    let r = t.await();
    let got = compare r {
        Success(n) => n;
        Cancelled() => -1;
    };
    if got != 42 {
        return 1;
    }
    print("ok");
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

func TestRuntimeV2CarrierAffineAcceptanceMatrix(t *testing.T) {
	ensureLLVMToolchain(t)
	skipTimeoutTests(t)
	rows := []struct {
		name   string
		source string
		pinned bool
	}{
		{name: "borrow-spawn", source: runtimeV2CarrierAffineBorrowSource, pinned: true},
		{name: "resident-across-parent-suspension", source: runtimeV2CarrierAffineResidentSource, pinned: true},
		{name: "yield-requeue-on-the-carrier", source: runtimeV2CarrierAffineYieldSource, pinned: true},
		{name: "cancel-of-a-borrowing-child", source: runtimeV2CarrierAffineCancelSource, pinned: true},
		{name: "independent-child-is-not-pinned", source: runtimeV2CarrierIndependentSource, pinned: false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			outputPath := buildLLVMProgramFromSource(t, row.source)
			for _, threads := range []int{2, 4, 8} {
				t.Run(fmt.Sprintf("threads-%d", threads), func(t *testing.T) {
					env := envWithStdlib(repoRoot(t))
					env = overrideEnvVar(env, "SURGE_SHARDS", "1")
					env = overrideEnvVar(env, "SURGE_THREADS", fmt.Sprintf("%d", threads))
					env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
					env = overrideEnvVar(env, "SURGE_SCHED_TRACE", "1")
					dur, res := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
					if res.exitCode != 0 {
						t.Fatalf("%s failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
							row.name, res.exitCode, dur, res.stdout, res.stderr)
					}
					if !strings.Contains(res.stdout, "ok") {
						t.Fatalf("unexpected stdout: %q", res.stdout)
					}
					trace := parseSchedTrace(t, res.stderr)
					if row.pinned && trace.carrierPinned < 1 {
						t.Fatalf("carrier_pinned=%d, want >= 1: the borrowing child was not pinned\n%s",
							trace.carrierPinned, res.stderr)
					}
					if !row.pinned && trace.carrierPinned != 0 {
						t.Fatalf("carrier_pinned=%d, want 0: a child that borrows nothing was pinned\n%s",
							trace.carrierPinned, res.stderr)
					}
					if trace.carrierShutdownCancelled != 0 {
						t.Fatalf("carrier_shutdown_cancelled=%d, want 0: a pinned task outlived its program\n%s",
							trace.carrierShutdownCancelled, res.stderr)
					}
				})
			}
		})
	}
}
