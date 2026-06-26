//go:build runtime_v2_pending

package vm_test

import (
	"testing"
	"time"
)

func TestRuntimeV2CancelledRecvWaiterDoesNotConsumeNextWake(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn wait_recv(ch: own Channel<int>) -> int {
    let v = ch.recv();
    return compare v {
        Some(x) => x;
        nothing => -1;
    };
}

@entrypoint
fn main() -> int {
    let r = (async {
        let ch = make_channel::<int>(0);
        let first_ch = ch;
        let second_ch = ch;
        let first = spawn wait_recv(first_ch);
        checkpoint().await();
        checkpoint().await();
        let second = spawn wait_recv(second_ch);
        checkpoint().await();
        checkpoint().await();

        first.cancel();
        let first_res = first.await();
        let first_cancelled = compare first_res {
            Success(_) => false;
            Cancelled() => true;
        };
        if !first_cancelled {
            return 1;
        }

        ch.send(42);
        let second_res = second.await();
        let second_ok = compare second_res {
            Success(v) => v == 42;
            Cancelled() => false;
        };
        if !second_ok {
            return 2;
        }

        print("ok");
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

func TestRuntimeV2CancelledSendWaiterDoesNotConsumeNextRecv(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn wait_send(ch: own Channel<int>, value: int) -> int {
    ch.send(value);
    return value;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let ch = make_channel::<int>(0);
        let first_ch = ch;
        let second_ch = ch;
        let first = spawn wait_send(first_ch, 11);
        checkpoint().await();
        checkpoint().await();
        let second = spawn wait_send(second_ch, 42);
        checkpoint().await();
        checkpoint().await();

        first.cancel();
        let first_res = first.await();
        let first_cancelled = compare first_res {
            Success(_) => false;
            Cancelled() => true;
        };
        if !first_cancelled {
            return 1;
        }

        let got = ch.recv();
        let got_ok = compare got {
            Some(v) => v == 42;
            nothing => false;
        };
        if !got_ok {
            return 2;
        }

        let second_res = second.await();
        let second_ok = compare second_res {
            Success(v) => v == 42;
            Cancelled() => false;
        };
        if !second_ok {
            return 3;
        }

        print("ok");
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

func TestRuntimeV2ChannelCloseWakesRecvWaiters(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn wait_recv(ch: own Channel<int>) -> int {
    let v = ch.recv();
    return compare v {
        Some(_) => 1;
        nothing => 0;
    };
}

@entrypoint
fn main() -> int {
    let r = (async {
        let ch = make_channel::<int>(0);
        let first_ch = ch;
        let second_ch = ch;
        let first = spawn wait_recv(first_ch);
        let second = spawn wait_recv(second_ch);
        checkpoint().await();
        checkpoint().await();

        ch.close();

        let first_res = first.await();
        let second_res = second.await();
        let first_ok = compare first_res {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        let second_ok = compare second_res {
            Success(v) => v == 0;
            Cancelled() => false;
        };
        if !first_ok || !second_ok {
            return 1;
        }

        print("ok");
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

func TestRuntimeV2SelectTimeoutCleansLosingChannelWaiter(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn timeout_then_recv(ch: own Channel<int>) -> int {
    let selected = select {
        ch.recv() => 100;
        sleep(5).await() => 5;
    };
    if selected != 5 {
        return selected;
    }

    let next = ch.recv();
    return compare next {
        Some(v) => v;
        nothing => -1;
    };
}

@entrypoint
fn main() -> int {
    let r = (async {
        let ch = make_channel::<int>(0);
        let task_ch = ch;
        let waiter = spawn timeout_then_recv(task_ch);

        sleep(20).await();
        ch.send(42);

        let res = waiter.await();
        let ok = compare res {
            Success(v) => v == 42;
            Cancelled() => false;
        };
        if !ok {
            return 1;
        }

        print("ok");
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
