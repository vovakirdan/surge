//go:build runtime_v2_pending

package vm_test

import (
	"testing"
	"time"
)

func TestRuntimeV2CancelledJoinWaiterDoesNotConsumeTaskCompletionWake(t *testing.T) {
	t.Run("positive", func(t *testing.T) {
		binPath := buildRuntimeV2CancelledJoinWaiterHarness(t, false)
		stdout, stderr, exitCode := runRuntimeV2CancelledJoinWaiterHarness(t, binPath)
		if exitCode != 0 {
			t.Fatalf("cancelled join waiter proof failed (code=%d)\nstdout:\n%s\nstderr:\n%s",
				exitCode, stdout, stderr)
		}
	})
	t.Run("negative-control", proveRuntimeV2CancelledJoinWaiterRegistrationNegativeControl)
	t.Run("static-boundary", checkRuntimeV2CancelledJoinWaiterSyncPointStaticBoundary)
}

func TestRuntimeV2FailfastScopeCancellationWakesOwner(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

@entrypoint
fn main() -> int {
    let r = (@failfast async {
        let slow = spawn spin(200);
        let fast = spawn async {
            checkpoint().await();
            ret 2;
        };

        fast.cancel();
        let fast_res = fast.await();
        let fast_cancelled = compare fast_res {
            Cancelled() => true;
            Success(_) => false;
        };
        if !fast_cancelled {
            ret 1;
        }

        let slow_res = slow.await();
        let slow_cancelled = compare slow_res {
            Cancelled() => true;
            Success(_) => false;
        };
        if !slow_cancelled {
            ret 2;
        }

        ret 0;
    }).await();

    let cancelled_ok = compare r {
        Cancelled() => true;
        Success(_) => false;
    };
    if !cancelled_ok {
        return 3;
    }

    print("ok", "\n");
    return 0;
}
`

	runMTSource(t, source, 10*time.Second)
}

func TestRuntimeV2BlockingCompletionWakesAwaiter(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `fn busy_loop(iter: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < iter {
        acc = acc + (i % 2);
        i = i + 1;
    }
    return acc;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let task = blocking {
            ret busy_loop(50000);
        };
        let res = task.await();
        let ok = compare res {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            ret 1;
        }
        print("ok", "\n");
        ret 0;
    }).await();

    return compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
}
`

	runMTSource(t, source, 10*time.Second)
}

func TestRuntimeV2CancelledBlockingWaiterDoesNotConsumeCompletionWake(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `fn busy_loop(iter: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < iter {
        acc = acc + (i % 2);
        i = i + 1;
    }
    return acc;
}

async fn await_task(task: Task<int>) -> int {
    let res = task.await();
    return compare res {
        Success(_) => 1;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    let r = (async {
        let blocker = blocking {
            ret busy_loop(500000);
        };
        let waiter_task = blocker.clone();
        let waiter = spawn await_task(waiter_task);
        checkpoint().await();
        checkpoint().await();

        waiter.cancel();
        let waiter_res = waiter.await();
        let waiter_cancelled = compare waiter_res {
            Cancelled() => true;
            Success(_) => false;
        };
        if !waiter_cancelled {
            ret 1;
        }

        let block_res = blocker.await();
        let block_ok = compare block_res {
            Success(_) => true;
            Cancelled() => false;
        };
        if !block_ok {
            ret 2;
        }

        print("ok", "\n");
        ret 0;
    }).await();

    return compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
}
`

	runMTSource(t, source, 10*time.Second)
}
