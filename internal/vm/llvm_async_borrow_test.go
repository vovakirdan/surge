//go:build !golden
// +build !golden

package vm_test

import (
	"os"
	"os/exec"
	"testing"
)

func TestLLVMNativeAsyncBorrowedRefsSurviveParentYield(t *testing.T) {
	source := `type Router = {
    chans: Channel<int>[],
};

async fn send_all(router: &Router, value: &int) -> nothing {
    let count: int = router.chans.__len() to int;
    let mut i: int = 0;
    while i < count {
        let ch: Channel<int> = router.chans[i];
        ch.send(*value);
        i = i + 1;
    }
    return nothing;
}

async fn read_after_wait(x: &int) -> int {
    checkpoint().await();
    return *x;
}

async fn run() -> int {
    let ch = make_channel::<int>(8:uint);
    let mut chans: Channel<int>[] = [];
    chans.push(ch);
    let router: Router = Router { chans = chans };
    let value: int = 7;
    let _ = send_all(&router, &value).await();
    let sent = compare ch.recv() {
        Some(v) => v;
        nothing => 99;
    };
    let waited_value: int = 2;
    let waited = compare read_after_wait(&waited_value).await() {
        Success(v) => v;
        Cancelled() => 80;
    };
    return sent + waited;
}

@entrypoint
fn main() -> int {
    compare run().await() {
        Success(v) => return v;
        Cancelled() => return 98;
    };
}
`
	root := repoRoot(t)
	outputPath := buildLLVMProgramFromSource(t, source)
	cmd := exec.Command(outputPath)
	cmd.Dir = root
	cmd.Env = overrideEnvVar(os.Environ(), "SURGE_THREADS", "4")
	stdout, stderr, exitCode := runCommand(t, cmd, "")
	if exitCode != 9 {
		t.Fatalf("exit code: want 9, got %d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}
