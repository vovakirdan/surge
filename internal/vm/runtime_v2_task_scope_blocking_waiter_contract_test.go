//go:build runtime_v2_pending

package vm_test

import (
	"testing"
	"time"
)

func TestRuntimeV2CancelledJoinWaiterDoesNotConsumeTaskCompletionWake(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn delayed_value() -> int {
    sleep(20).await();
    return 42;
}

async fn await_task(task: Task<int>) -> int {
    let res = task.await();
    return compare res {
        Success(v) => v;
        Cancelled() => -1;
    };
}

@entrypoint
fn main() -> int {
    let r = (async {
        let target = spawn delayed_value();
        let first_target = target.clone();
        let first = spawn await_task(first_target);
        checkpoint().await();
        checkpoint().await();

        first.cancel();
        let first_res = first.await();
        let first_cancelled = compare first_res {
            Cancelled() => true;
            Success(_) => false;
        };
        if !first_cancelled {
            return 1;
        }

        let second_target = target.clone();
        let second = spawn await_task(second_target);
        let second_res = second.await();
        let second_ok = compare second_res {
            Success(v) => v == 42;
            Cancelled() => false;
        };
        if !second_ok {
            return 2;
        }

        let target_res = target.await();
        let target_ok = compare target_res {
            Success(v) => v == 42;
            Cancelled() => false;
        };
        if !target_ok {
            return 3;
        }

        print("ok", "\n");
        return 0;
    }).await();

    return compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
}
`

	runMTSource(t, source, 10*time.Second)
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
            return 2;
        };

        fast.cancel();
        let fast_res = fast.await();
        let fast_cancelled = compare fast_res {
            Cancelled() => true;
            Success(_) => false;
        };
        if !fast_cancelled {
            return 1;
        }

        let slow_res = slow.await();
        let slow_cancelled = compare slow_res {
            Cancelled() => true;
            Success(_) => false;
        };
        if !slow_cancelled {
            return 2;
        }

        return 0;
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
            return busy_loop(50000);
        };
        let res = task.await();
        let ok = compare res {
            Success(_) => true;
            Cancelled() => false;
        };
        if !ok {
            return 1;
        }
        print("ok", "\n");
        return 0;
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
            return busy_loop(500000);
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
            return 1;
        }

        let block_res = blocker.await();
        let block_ok = compare block_res {
            Success(_) => true;
            Cancelled() => false;
        };
        if !block_ok {
            return 2;
        }

        print("ok", "\n");
        return 0;
    }).await();

    return compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
}
`

	runMTSource(t, source, 10*time.Second)
}
