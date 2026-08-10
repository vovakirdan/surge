package vm_test

import (
	"os"
	"os/exec"
	"testing"
)

// A cloned handle asks the same task for the same result, and each asker must
// end up owning a value of its own.
//
// The payload here is a string, which is the shape that makes the question
// visible without any transport allocation in the picture: two askers handed
// the same string handle both free it, and the second free is a double free.
// The program returns 0 only if BOTH askers read the whole value, so an answer
// that quietly served one of them an empty or already-freed string fails here
// rather than passing on a lucky read.
func TestLLVMNativeClonedTaskHandleServesEachAwaiterItsOwnResult(t *testing.T) {
	source := `async fn greet() -> string {
    return "hello from the task";
}

async fn run() -> int {
    let mut bad: int = 0;
    let mut i: int = 0;
    while i < 8 {
        let task: Task<string> = spawn greet();
        let sibling: Task<string> = task.clone();
        let first: string = compare task.await() {
            Success(value) => value;
            Cancelled() => "";
        };
        let second: string = compare sibling.await() {
            Success(value) => value;
            Cancelled() => "";
        };
        if first != "hello from the task" { bad = bad + 1; }
        if second != "hello from the task" { bad = bad + 1; }
        print(first);
        print(second);
        i = i + 1;
    }
    return bad;
}

@entrypoint
fn main() -> int {
    compare run().await() {
        Success(v) => return v;
        Cancelled() => return 97;
    };
}
`
	root := repoRoot(t)
	outputPath := buildLLVMProgramFromSource(t, source)
	cmd := exec.Command(outputPath)
	cmd.Dir = root
	cmd.Env = overrideEnvVar(os.Environ(), "SURGE_THREADS", "4")
	stdout, stderr, exitCode := runCommand(t, cmd, "")
	if exitCode != 0 {
		t.Fatalf("cloned task handle did not serve both awaiters: exit %d\nstdout:\n%s\nstderr:\n%s",
			exitCode, stdout, stderr)
	}
}
