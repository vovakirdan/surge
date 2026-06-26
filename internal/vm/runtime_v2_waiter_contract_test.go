//go:build runtime_v2_pending

package vm_test

import (
	"strings"
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

func TestRuntimeV2ChannelCloseWakesSendWaiters(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn wait_send(ch: own Channel<int>) -> int {
    ch.send(42);
    return 42;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let ch = make_channel::<int>(0);
        let send_ch = ch;
        let sender = spawn wait_send(send_ch);
        checkpoint().await();
        checkpoint().await();

        ch.close();

        let _ = sender.await();
        return 1;
    }).await();

    return compare r {
        Success(v) => v;
        Cancelled() => 99;
    };
}
`

	outputPath := buildLLVMProgramFromSource(t, source)
	dur, res := runBinaryWithTimeout(t, outputPath, mtEnv(t), 10*time.Second)
	if res.exitCode == 0 {
		t.Fatalf("expected send-on-closed panic, got success after %s\nstdout:\n%s\nstderr:\n%s",
			dur, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "send on closed channel") {
		t.Fatalf("expected send-on-closed panic after %s, got exit=%d\nstdout:\n%s\nstderr:\n%s",
			dur, res.exitCode, res.stdout, res.stderr)
	}
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

func TestRuntimeV2CancelledSelectCleansWaitKeysAndTimers(t *testing.T) {
	ensureLLVMToolchain(t)

	source := `async fn select_recv_or_sleep(ch: own Channel<int>) -> int {
    let selected = select {
        ch.recv() => 1;
        sleep(100).await() => 2;
    };
    return selected;
}

async fn recv_once(ch: own Channel<int>) -> int {
    let got = ch.recv();
    return compare got {
        Some(v) => v;
        nothing => -1;
    };
}

@entrypoint
fn main() -> int {
    let r = (async {
        let ch = make_channel::<int>(0);
        let select_ch = ch;
        let waiter = spawn select_recv_or_sleep(select_ch);
        checkpoint().await();
        checkpoint().await();

        waiter.cancel();
        let cancelled_res = waiter.await();
        let cancelled_ok = compare cancelled_res {
            Cancelled() => true;
            Success(_) => false;
        };
        if !cancelled_ok {
            return 1;
        }

        sleep(150).await();

        let recv_ch = ch;
        let receiver = spawn recv_once(recv_ch);
        checkpoint().await();
        checkpoint().await();
        ch.send(42);

        let recv_res = receiver.await();
        let recv_ok = compare recv_res {
            Success(v) => v == 42;
            Cancelled() => false;
        };
        if !recv_ok {
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
