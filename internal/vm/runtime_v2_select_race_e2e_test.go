package vm_test

import (
	"slices"
	"strings"
	"testing"
)

// N-TASK-20/21 end to end, on the backend SURGE_BACKEND names. Each row asserts only what the model promises
// (docs/RUNTIME_V2.md l.1171-1181: publication does not promise a first poll, so no row requires a cancelled task's
// body to leave, or not to leave, a witness it could leave before its first suspension). No row may assert that an
// unscanned `select` loser runs at the join: that is an artifact of the head temp no MIR drop releases (design F-2).
// No row has a grandchild that borrows a race loser's resident local (RV2-DEBT-373).

const selectRaceColdLoserSource = `async fn noisy() -> int {
    print("noisy ran");
    return 7;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let ch = Channel::<int>::new(1:uint);
        ch.send(5);
        let v = race {
            ch.recv() => 1;
            noisy().await() => 2;
        };
        ret v;
    }).await();
    print("after");
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

const selectRacePublishedLoserSource = `async fn slow() -> int {
    print("slow start");
    checkpoint().await();
    print("slow end");
    return 1;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let v = race {
            slow().await() => 1;
            default => 2;
        };
        print("race done");
        ret v;
    }).await();
    print("after");
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

const selectBorrowingOnlyArmSource = `async fn sworker(x: &string) -> int {
    checkpoint().await();
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select {
            sworker(&bl).await() => 46;
        };
        ret v;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

const selectRaceCloneSource = `async fn sworker(x: &string) -> int {
    checkpoint().await();
    print("worker read " + x);
    return len(x) to int;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let t = sworker(&bl);
        let t2 = t.clone();
        let v = race {
            t.await() => 1;
            default => 2;
        };
        let got = compare t2.await() {
            Success(n) => n;
            Cancelled() => 40;
        };
        ret v + got;
    }).await();
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

const selectRiSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    checkpoint().await();
    print("worker read " + x);
    return len(x) to int;
}

async fn leak2(x: &string) -> Task<int> {
    let t = worker(x);
    return t;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select { leak2(&bl).await() => 46; };
        ret v;
    }).await();
    print("after");
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

const selectRiSpawnSource = `async fn worker(x: &string) -> int {
    checkpoint().await();
    checkpoint().await();
    print("worker read " + x);
    return len(x) to int;
}

async fn leak2(x: &string) -> Task<int> {
    let t = spawn worker(x);
    return t;
}

@entrypoint
fn main() -> int {
    let r = (async {
        let mut bl: string = "abc";
        bl = bl + "def";
        let v = select { leak2(&bl).await() => 46; };
        ret v;
    }).await();
    print("after");
    return compare r {
        Success(n) => n;
        Cancelled() => 100;
    };
}
`

func selectRaceLines(out string) []string {
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

func TestSelectRaceRuntime(t *testing.T) {
	// race_cold_loser_answered_unrun pins the CURRENT scan plus RT-COLD-2: the scan stops at the first ready arm
	// (vm_dispatch_async_select.go, rt_async_select.c: an implementation property, not LANGUAGE.md l.1945-1948), so
	// the loser is still COLD when the race cancels it, and a cold cancel is answered unrun (RUNTIME.md l.172-176).
	// A change to the scan must update this row knowingly.
	t.Run("race_cold_loser_answered_unrun", func(t *testing.T) {
		skipTimeoutTests(t)
		res := runProgramFromSource(t, selectRaceColdLoserSource, runOptions{captureStdout: true})
		if res.exitCode != 1 || res.stdout != "after\n" || res.stderr != "" {
			t.Fatalf("exit %d stdout %q stderr %q, want exit 1 and exactly \"after\"", res.exitCode, res.stdout, res.stderr)
		}
	})
	// A published loser is cancelled cooperatively: it may enter its body and even pass its checkpoint (the await of a
	// child already DONE hands the result over, LANGUAGE.md l.1923). Only the line set, the order of the parent's own
	// lines and the exit are promised.
	t.Run("race_published_loser_cancelled", func(t *testing.T) {
		skipTimeoutTests(t)
		res := runProgramFromSource(t, selectRacePublishedLoserSource, runOptions{captureStdout: true})
		lines := selectRaceLines(res.stdout)
		seen := map[string]int{}
		for _, l := range lines {
			seen[l]++
		}
		done := slices.Index(lines, "race done")
		ok := res.exitCode == 2 && res.stderr == "" && len(lines) > 0 && lines[len(lines)-1] == "after" && seen["after"] == 1 &&
			seen["race done"] == 1 && done < len(lines)-1 && seen["slow start"] <= 1 && seen["slow end"] <= 1 &&
			len(seen) == 2+min(1, seen["slow start"])+min(1, seen["slow end"])
		if !ok {
			t.Fatalf("exit %d stdout %q stderr %q, want exit 2, `race done` once before a last `after`, and at most one each of `slow start` and `slow end`", res.exitCode, res.stdout, res.stderr)
		}
	})
	t.Run("select_borrowing_head_in_the_only_arm", func(t *testing.T) {
		skipTimeoutTests(t)
		res := runProgramFromSource(t, selectBorrowingOnlyArmSource, runOptions{captureStdout: true})
		if res.exitCode != 46 || res.stdout != "" || res.stderr != "" {
			t.Fatalf("exit %d stdout %q stderr %q, want exit 46 and no output", res.exitCode, res.stdout, res.stderr)
		}
	})
	// v is 1 (t won, so t2 answers 6) or 2 (default won, t cancelled: Success(6) or Cancelled() -> 40): 7, 8 or 42. If
	// the worker read its borrow, it read it alive: t2.await() joins the loser before bl is released.
	t.Run("race_bound_loser_joined_through_its_clone", func(t *testing.T) {
		skipTimeoutTests(t)
		res := runProgramFromSource(t, selectRaceCloneSource, runOptions{captureStdout: true})
		if !slices.Contains([]int{7, 8, 42}, res.exitCode) || res.stderr != "" || (res.stdout != "" && res.stdout != "worker read abcdef\n") {
			t.Fatalf("exit %d stdout %q stderr %q, want exit 7, 8 or 42 and nothing but `worker read abcdef`", res.exitCode, res.stdout, res.stderr)
		}
	})
	// S-RI-SELECT (design review R7): the head never delivers leak2's payload; leak2's scope join runs the inner task first.
	for _, row := range []struct{ name, source string }{
		{"ri_select_head_over_a_borrowing_payload", selectRiSource},
		{"ri_select_head_over_a_spawned_borrowing_payload", selectRiSpawnSource},
	} {
		t.Run(row.name, func(t *testing.T) {
			skipTimeoutTests(t)
			res := runProgramFromSource(t, row.source, runOptions{captureStdout: true})
			if res.exitCode != 46 || res.stdout != "worker read abcdef\nafter\n" || res.stderr != "" {
				t.Fatalf("exit %d stdout %q stderr %q, want exit 46 and `worker read abcdef` then `after`", res.exitCode, res.stdout, res.stderr)
			}
		})
	}
}
