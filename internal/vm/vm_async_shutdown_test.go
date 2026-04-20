package vm_test

import "testing"

func TestVMAsyncChildRtExitHaltsProgram(t *testing.T) {
	sourceCode := `async fn child() -> nothing {
    rt_exit(7);
    return nothing;
}

@entrypoint
fn main() -> int {
    let task: Task<nothing> = spawn child();
    compare task.await() {
        Success(_) => return 42;
        Cancelled() => return 99;
    }
}`

	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", result.exitCode)
	}
	if result.stderr != "" {
		t.Fatalf("unexpected VM error:\n%s", result.stderr)
	}
}

func TestVMAsyncChildPanicHaltsProgram(t *testing.T) {
	sourceCode := `async fn child() -> nothing {
    panic("boom");
    return nothing;
}

@entrypoint
fn main() -> int {
    let task: Task<nothing> = spawn child();
    compare task.await() {
        Success(_) => return 42;
        Cancelled() => return 99;
    }
}`

	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", result.exitCode)
	}
	if result.stderr != "" {
		t.Fatalf("unexpected VM error:\n%s", result.stderr)
	}
}
