package vm_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// A member cancelled while cold never runs, end to end. The program needs a non-own
// `.await()`, which this line builds only once the await's origin transfer lands with it; the
// runtime half is pinned without it by TestRuntimeV2ColdTaskFailfastCancelsAColdMemberUnrun
// (native stand) and TestColdTaskCancelledByFailfastAnswersWithoutRunning (VM).
//
// The sibling is cancelled while cold and ends Cancelled, which cancels every member of the
// @failfast scope; the block meanwhile creates 100000 members without suspending and drops each
// inside `sink`, so that cancel can land on a member between its creation and its drop. Such a
// member must answer Cancelled() without running: its body ends the process with 46. Since the
// dropped-task rule (SEM3218) the member cannot be `worker(&l);` dropped where it stands -- that
// program, byte for byte, is refused by TestTaskDroppedRefusesTheBorrowedOriginals/failfast_member --
// so it borrows nothing and 40 (a read after the block released `l`) cannot occur. Before the
// runtime answered such a task unrun, the borrowed form exited 40 natively in 31 of 300 runs at
// SURGE_THREADS=4 and 38 of 300 at 8 (measured at 0aa69629); the VM, which runs tasks on one
// thread, cannot open the window (0 of 20). The borrow-free member is not carrier-affine, so its
// window rate is its own: K below is re-derived from DROPTASK's stage S-FAILFAST-K.
//
// A statistical detector, not a witness: nothing in the program can show the window opened.
// It runs where SURGE_SKIP_TIMEOUT_TESTS=0 (the nightly llvm suite) unless a gate names it.
const coldFailfastMemberSource = `async fn worker(n: int) -> int {
    rt_exit(n + 40);
    return n;
}

fn sink(t: Task<int>) -> int {
    return 0;
}

async fn sibling() -> int {
    let mut i: int = 0;
    while i < 1000 {
        let _ = checkpoint().await();
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let outcome = (@failfast async {
        let s = sibling();
        s.cancel();
        let mut k: int = 0;
        while k < 100000 {
            let _r = sink(worker(6));
            k = k + 1;
        }
        let _ = s.await();
        ret 0;
    }).await();
    return compare outcome {
        Success(n) => n;
        Cancelled() => 0;
    };
}
`

// coldFailfastMemberRuns is K per thread count on the native backend. The rates above were the
// borrowed, carrier-affine member's; the borrow-free member's rate p is re-measured with RT-COLD-2's
// CF1 composed (DROPTASK S-FAILFAST-K, 300 runs at 4 and at 8 threads), and K must satisfy
// (1-p)^K <= 1e-2 per cell: K = 50 holds for p >= 0.088; below that rate this constant is raised
// to ceil(ln(0.01)/ln(1-p)) with p stated here.
const coldFailfastMemberRuns = 50

func TestRuntimeV2ColdCancelledMemberNeverRuns(t *testing.T) {
	skipTimeoutTests(t)
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	src := artifactSourcePath(artifacts)
	if err := os.WriteFile(src, []byte(coldFailfastMemberSource), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	res := runProgram(t, root, src, runOptions{captureStdout: true}, artifacts)
	if res.exitCode != 0 || res.stdout != "" || res.stderr != "" {
		t.Fatalf("first run: exit %d stdout %q stderr %q, want exit 0 and no output", res.exitCode, res.stdout, res.stderr)
	}
	if testBackend(t) != backendLLVM {
		return // one thread runs every task: the first run is the whole answer
	}
	bin := llvmOutputPath(root, src)
	for _, threads := range []string{"4", "8"} {
		bad := map[string]int{}
		for range coldFailfastMemberRuns {
			cmd := exec.Command(bin)
			cmd.Env = append(os.Environ(), "SURGE_THREADS="+threads)
			stdout, stderr, code := runCommand(t, cmd, "")
			if code != 0 || stdout != "" || stderr != "" {
				bad[fmt.Sprintf("exit=%d stdout=%q stderr=%.200q", code, stdout, strings.TrimSpace(stderr))]++
			}
		}
		if len(bad) > 0 {
			t.Errorf("SURGE_THREADS=%s: runs that were not a clean exit 0 out of %d (46: a member cancelled while cold ran): %v",
				threads, coldFailfastMemberRuns, bad)
		}
	}
}
