//go:build runtime_v2_pending

package vm_test

// A `timeout` budget over a task parked on a socket has to be paid in real
// time. It used to be paid in virtual milliseconds: the executor considered
// itself idle while a task waited on the network, jumped its clock to the next
// deadline, and a sixty-second budget expired in six.
//
// The differential below is what proved the defect and therefore what proves
// the fix — one program, two budgets. A clock that is not time makes both
// budgets die together; a clock that is time makes the long one outlive the
// short one.

import (
	"errors"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// exitCodeOf reads the process's own exit code out of a Wait error, so a test
// can assert on the number the program chose rather than on "it failed".
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

const timeoutWallBudgetSource = `
import stdlib/net as net;

async fn accept_budget(listener: own TcpListener, budget: uint) -> int {
    let accept_task = net.accept(&listener);
    let accept_res = timeout(accept_task, budget);
    compare accept_res {
        Success(net_res) => {
            compare net_res {
                Success(conn) => {
                    let _ = net.close_conn(own conn);
                    return 0;
                }
                err => { let _ = err; return 1; }
            };
        }
        Cancelled() => { return 7; }
    };
    return 9;
}

@entrypoint("argv")
fn main(port: uint, budget: uint) -> int {
    let r = net.listen("127.0.0.1", port);
    compare r {
        Success(l) => {
            // Awaited in place rather than spawned: a TcpListener is @nosend,
            // and what is under test is the budget, not where the task runs.
            let done = accept_budget(own l, budget).await();
            compare done {
                Success(code) => { return code; }
                Cancelled() => { return 8; }
            };
        }
        err => { let _ = err; return 2; }
    };
    return 3;
}
`

// The budget nobody pays: no client ever connects, so the only way out is the
// timeout. Exit 7 is the Cancelled arm.
const timeoutWallBudgetCancelledExit = 7

func startTimeoutWallBudgetServer(t *testing.T, binary string, port, budget int) (*exec.Cmd, chan error) {
	t.Helper()
	cmd := exec.Command(binary, strconv.Itoa(port), strconv.Itoa(budget))
	cmd.Env = mtEnv(t)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server (budget=%d): %v", budget, err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	return cmd, waitCh
}

func TestRuntimeV2TimeoutOverNetWaitSurvivesItsWallBudget(t *testing.T) {
	skipTimeoutTests(t)
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	binary := buildLLVMProgramFromSource(t, timeoutWallBudgetSource)

	// A one-second budget must take about a second — not the milliseconds a
	// jumped clock used to charge for it.
	startedAt := time.Now()
	_, shortWait := startTimeoutWallBudgetServer(t, binary, pickFreePort(t), 1000)
	select {
	case err := <-shortWait:
		elapsed := time.Since(startedAt)
		code := exitCodeOf(err)
		if code != timeoutWallBudgetCancelledExit {
			t.Fatalf("short budget: exit=%d want %d (err=%v)", code, timeoutWallBudgetCancelledExit, err)
		}
		if elapsed < 800*time.Millisecond {
			t.Fatalf("a 1000ms budget expired in %v: the clock is running ahead of the wall again", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("short budget: server outlived its 1000ms timeout by ten seconds")
	}

	// The same program with sixty times the budget must still be alive when the
	// short one was long gone. This is the half that fails on the unfixed tree,
	// where both budgets die together in single-digit milliseconds.
	longCmd, longWait := startTimeoutWallBudgetServer(t, binary, pickFreePort(t), 60000)
	select {
	case err := <-longWait:
		t.Fatalf("long budget: server exited after less than 3s (err=%v); a 60000ms budget cannot be spent that fast", err)
	case <-time.After(3 * time.Second):
	}
	if err := longCmd.Process.Kill(); err != nil {
		t.Fatalf("kill long-budget server: %v", err)
	}
	<-longWait
}

// The bound applies only while something outside the process can still make a
// task runnable. A program that merely sleeps has nobody to wait for, so its
// virtual clock must still fast-forward — otherwise this fix would have made
// every sleeping test pay real seconds.
func TestRuntimeV2SleepWithoutNetWaiterStillAdvancesInstantly(t *testing.T) {
	skipTimeoutTests(t)
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	source := `
@entrypoint
fn main() -> int {
    let t = (async {
        sleep(5000:uint).await();
        return 42;
    }).await();
    compare t {
        Success(v) => { return v; }
        Cancelled() => { return 1; }
    };
}
`
	binary := buildLLVMProgramFromSource(t, source)
	cmd := exec.Command(binary)
	cmd.Env = mtEnv(t)
	startedAt := time.Now()
	err := cmd.Run()
	elapsed := time.Since(startedAt)

	if code := exitCodeOf(err); code != 42 {
		t.Fatalf("sleep program exit=%d want 42 (err=%v)", code, err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("sleep(5000) took %v of real time: the idle fast-forward was lost", elapsed)
	}
}
