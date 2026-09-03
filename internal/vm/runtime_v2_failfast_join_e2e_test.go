package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RV2-DEBT-261, the program-level row. Four @failfast blocks per round, eight
// rounds, on both lanes, at one worker and at four. The property under test
// is fail-fast propagation itself: a block resolves Cancelled exactly when at
// least one member answered Cancelled, and Success exactly when every member
// answered Success. Each block reports through a witness channel which of the
// two it saw from the inside, and the driver holds the block's own answer
// against that witness.
//
// The first two blocks race a cancel against children that need only a few
// checkpoints (the shapes that gave TestMTStructuredConcurrency its exit 12
// and exit 13 under pinned CPUs). The race can go either way: on an
// oversubscribed host the block's owner is descheduled between spawn and
// cancel, the children run to completion first, and the cancel is refused
// against a committed result (the 2026-08 ruling: a cancel never revokes an
// answer already committed). Then no member answered Cancelled, nothing owes
// fail-fast, and the block MUST resolve Success. The previous version of this
// row asserted that the cancel always wins that race; the 2026-09-03 campaign
// read that as exit 12/13 in about one run in seventy on the shared runner,
// and every one of those was a child that had finished before its cancel.
//
// The last two blocks take the schedule out of the question. Two children park
// on channels nobody sends into, so a cancel always finds them alive: the
// block must resolve Cancelled. The control block lets two sleeping children
// finish untouched: the block must resolve Success. A runtime that drops the
// fail-fast flag turns the parked row into exit 15 on every run, which is what
// makes this row a judge rather than a witness of one schedule.
//
// Exit codes: 12/13 a racing block resolved Success although a member answered
// Cancelled; 14 a racing block resolved Cancelled although no member did; 15
// the parked block resolved Success or a parked child answered Success; 16 the
// control block resolved Cancelled; 17 a witness went missing; 99 the driver.
const runtimeV2FailfastJoinSource = `async fn spin(count: int) -> int {
    let mut i = 0;
    while i < count {
        checkpoint().await();
        i = i + 1;
    }
    return count;
}

async fn parked(stop: Channel<int>) -> int {
    let _ = stop.recv();
    return 1;
}

async fn napping() -> int {
    sleep(1).await();
    return 1;
}

fn is_cancelled(r: TaskResult<int>) -> bool {
    return compare r {
        Cancelled() => true;
        Success(_) => false;
    };
}

fn take_witness(w: Channel<int>) -> int {
    let seen: Option<int> = w.try_recv();
    return compare seen { Some(v) => v; nothing => 0 - 1; };
}

async fn main_async() -> int {
    let witness = Channel::<int>::new(8:uint);
    let mut round = 0;
    let mut race_cancelled = 0;
    while round < 8 {
        // Race 1: witness 1 = a member answered Cancelled; 0 = both answered
        // Success; 2 = fast answered Success and the block returned early,
        // cancelling slow through rt_scope_cancel_all, so slow's answer is
        // whatever that cancel found and the block may resolve either way.
        let ff = (@failfast async {
            let slow = spawn async {
                let _ = spin(200).await();
                ret 1;
            };
            let fast = spawn async {
                checkpoint().await();
                ret 2;
            };
            fast.cancel();
            let fast_cancelled = is_cancelled(fast.await());
            if !fast_cancelled {
                witness.try_send(2);
                ret 10;
            }
            let slow_cancelled = is_cancelled(slow.await());
            if fast_cancelled || slow_cancelled {
                witness.try_send(1);
            } else {
                witness.try_send(0);
            }
            ret 0;
        }).await();
        let w1 = take_witness(witness);
        let ff_cancelled = is_cancelled(ff);
        if w1 == 1 {
            race_cancelled = race_cancelled + 1;
            if !ff_cancelled {
                return 12;
            }
        } else if w1 == 0 {
            if ff_cancelled {
                return 14;
            }
        } else if w1 != 2 {
            return 17;
        }

        // Race 2: both children cancelled right after spawn; witness 1 = at
        // least one answered Cancelled, 0 = both had already finished.
        let ff2 = (@failfast async {
            let a = spawn async {
                let _ = spin(50).await();
                ret 1;
            };
            let b = spawn async {
                let _ = spin(50).await();
                ret 2;
            };
            a.cancel();
            b.cancel();
            let a_cancelled = is_cancelled(a.await());
            let b_cancelled = is_cancelled(b.await());
            if a_cancelled || b_cancelled {
                witness.try_send(1);
            } else {
                witness.try_send(0);
            }
            ret 0;
        }).await();
        let w2 = take_witness(witness);
        let ff2_cancelled = is_cancelled(ff2);
        if w2 == 1 {
            race_cancelled = race_cancelled + 1;
            if !ff2_cancelled {
                return 13;
            }
        } else if w2 == 0 {
            if ff2_cancelled {
                return 14;
            }
        } else {
            return 17;
        }

        // Parked: the children wait on channels nobody sends into, so the
        // cancel always finds them alive. Witness 1 = both answered Cancelled.
        let stop_a = Channel::<int>::new(0:uint);
        let stop_b = Channel::<int>::new(0:uint);
        let ff3 = (@failfast async {
            let a = spawn parked(stop_a);
            let b = spawn parked(stop_b);
            checkpoint().await();
            a.cancel();
            b.cancel();
            let a_cancelled = is_cancelled(a.await());
            let b_cancelled = is_cancelled(b.await());
            if a_cancelled && b_cancelled {
                witness.try_send(1);
            } else {
                witness.try_send(0);
            }
            ret 0;
        }).await();
        let w3 = take_witness(witness);
        if w3 != 1 || !is_cancelled(ff3) {
            return 15;
        }

        // Control: nobody is cancelled, so fail-fast must stay quiet and the
        // block must resolve Success.
        let ff4 = (@failfast async {
            let a = spawn napping();
            let b = spawn napping();
            let a_cancelled = is_cancelled(a.await());
            let b_cancelled = is_cancelled(b.await());
            if a_cancelled || b_cancelled {
                witness.try_send(1);
            } else {
                witness.try_send(0);
            }
            ret 0;
        }).await();
        let w4 = take_witness(witness);
        if w4 != 0 || is_cancelled(ff4) {
            return 16;
        }
        round = round + 1;
    }
    print("failfast-join-ok race-cancelled=" + (race_cancelled to string));
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

func TestRuntimeV2FailfastJoinAnswersCancelled(t *testing.T) {
	t.Run("llvm", func(t *testing.T) {
		outputPath := buildLLVMProgramFromSource(t, runtimeV2FailfastJoinSource)
		baseEnv := envWithStdlib(repoRoot(t))
		for _, threads := range []string{"1", "4"} {
			t.Run("threads-"+threads, func(t *testing.T) {
				env := overrideEnv(baseEnv, threads)
				dur, res := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
				assertFailfastJoinRun(t, "llvm", threads, res.exitCode, dur, res.stdout, res.stderr)
			})
		}
	})
	t.Run("vm", func(t *testing.T) {
		root := repoRoot(t)
		surge := buildSurgeBinary(t, root)
		srcPath := filepath.Join(t.TempDir(), "failfast_join.sg")
		if err := os.WriteFile(srcPath, []byte(runtimeV2FailfastJoinSource), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}
		baseEnv := envWithStdlib(root)
		for _, threads := range []string{"1", "4"} {
			t.Run("threads-"+threads, func(t *testing.T) {
				env := overrideEnv(baseEnv, threads)
				start := time.Now()
				stdout, stderr, code := runSurgeWithEnv(t, root, surge, env, "run", "--backend=vm", srcPath)
				assertFailfastJoinRun(t, "vm", threads, code, time.Since(start), stdout, stderr)
			})
		}
	})
}

func assertFailfastJoinRun(t *testing.T, lane, threads string, exitCode int, dur time.Duration, stdout, stderr string) {
	t.Helper()
	reasons := map[int]string{
		12: "the first racing @failfast block resolved Success although a member answered Cancelled",
		13: "the second racing @failfast block resolved Success although a member answered Cancelled",
		14: "a racing @failfast block resolved Cancelled although no member answered Cancelled",
		15: "the parked @failfast block resolved Success, or a parked child answered Success despite its cancel",
		16: "the control @failfast block resolved Cancelled although nobody was cancelled",
		17: "a block's witness went missing from the witness channel",
		99: "main_async itself resolved Cancelled",
	}
	if exitCode != 0 {
		reason := reasons[exitCode]
		if reason == "" {
			reason = "the program did not run to its verdict"
		}
		t.Fatalf("%s SURGE_THREADS=%s: exit=%d -- %s (dur=%s)\nstdout:\n%s\nstderr:\n%s",
			lane, threads, exitCode, reason, dur, stdout, stderr)
	}
	if !strings.Contains(stdout, "failfast-join-ok") {
		t.Fatalf("%s SURGE_THREADS=%s: missing completion marker\nstdout:\n%s\nstderr:\n%s",
			lane, threads, stdout, stderr)
	}
}
