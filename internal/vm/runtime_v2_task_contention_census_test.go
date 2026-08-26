package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A cohort of E handles awaited CONCURRENTLY, from E tasks on several workers,
// costs exactly E-1 duplications and one move.
//
// The sequential cohort row (runtime_v2_task_cohort_census_test.go) cannot see
// the case this one is about: there the last asker always finds the slot to
// itself. Here E askers race for one result, so the last of them can arrive
// while an earlier one is still copying out of the slot. The task answers
// that asker WAIT and it parks on the join key; the reader that retires last
// wakes it, and it moves. A task that served it a duplicate instead -- the
// conservative answer -- would cost E duplications on exactly the rounds
// where the race happened.
//
// The result type counts its own duplications: its user __clone marks the
// copy it builds, so an asker can tell whether it was handed the original
// (moved) or a duplicate. Per round the duplicates must number exactly E-1 --
// one asker, and only one, gets the original -- and every round of the run
// is checked, so a single round that duplicated for its last asker fails the
// row. No heap census is involved, so scheduler queue growth cannot blur it.
const runtimeV2TaskContentionCensusSource = `
type Marked = { s: string, copies: int }

extern<Marked> {
    pub fn __clone(self: &Marked) -> Marked {
        return Marked { s = self.s.__clone(), copies = self.copies + 1 };
    }
}

async fn produce() -> Marked {
    let mut i: int = 0;
    let mut s: string = "cohort-";
    while i < 3 {
        checkpoint().await();
        s = s + "v";
        i = i + 1;
    }
    return Marked { s = s, copies = 0 };
}

// How many duplications stand between this asker and the canonical value:
// 0 for the asker that was handed the original, 1 for a duplicate.
async fn ask(handle: Task<Marked>) -> int {
    let v: Marked = compare handle.await() { Success(x) => x; Cancelled() => Marked { s = "", copies = 99 }; };
    if v.s != "cohort-vvv" { return 99; }
    return v.copies;
}

async fn contended_two() -> int {
    let t: Task<Marked> = spawn produce();
    let s: Task<Marked> = t.clone();
    let a: Task<int> = spawn ask(t);
    let b: Task<int> = spawn ask(s);
    let ra: int = compare a.await() { Success(x) => x; Cancelled() => 99; };
    let rb: int = compare b.await() { Success(x) => x; Cancelled() => 99; };
    return ra + rb;
}

async fn contended_four() -> int {
    let t: Task<Marked> = spawn produce();
    let s1: Task<Marked> = t.clone();
    let s2: Task<Marked> = t.clone();
    let s3: Task<Marked> = t.clone();
    let a: Task<int> = spawn ask(t);
    let b: Task<int> = spawn ask(s1);
    let c: Task<int> = spawn ask(s2);
    let d: Task<int> = spawn ask(s3);
    let ra: int = compare a.await() { Success(x) => x; Cancelled() => 99; };
    let rb: int = compare b.await() { Success(x) => x; Cancelled() => 99; };
    let rc: int = compare c.await() { Success(x) => x; Cancelled() => 99; };
    let rd: int = compare d.await() { Success(x) => x; Cancelled() => 99; };
    return ra + rb + rc + rd;
}

async fn run() -> int {
    let mut round: int = 0;
    let mut bad_rounds: int = 0;
    let mut extra_dups: int = 0;
    while round < 48 {
        let two: int = compare contended_two().await() { Success(x) => x; Cancelled() => 99; };
        if two != 1 {
            bad_rounds = bad_rounds + 1;
            extra_dups = extra_dups + two - 1;
        }
        let four: int = compare contended_four().await() { Success(x) => x; Cancelled() => 99; };
        if four != 3 {
            bad_rounds = bad_rounds + 1;
            extra_dups = extra_dups + four - 3;
        }
        round = round + 1;
    }
    print("task contention census: rounds=");
    print((round * 2) to string);
    print(" rounds with other than E-1 duplications=");
    print(bad_rounds to string);
    print(" extra duplications=");
    print(extra_dups to string);
    if bad_rounds != 0 {
        print("FAIL a contended cohort did not cost exactly E-1 duplications");
        return 1;
    }
    print("task-contention-census-ok");
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

func TestRuntimeV2TaskContentionCohortCostsExactlyOneFewerDuplication(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2TaskContentionCensusSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_THREADS", "4")
	env = overrideEnvVar(env, "SURGE_SHARDS", "1")
	for round := range 3 {
		duration, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
		if result.exitCode != 0 {
			t.Fatalf("task contention census failed on run %d (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
				round, result.exitCode, duration, result.stdout, result.stderr)
		}
		if !strings.Contains(result.stdout, "task-contention-census-ok") {
			t.Fatalf("task contention census missing completion marker; stdout=%q", result.stdout)
		}
	}
}
