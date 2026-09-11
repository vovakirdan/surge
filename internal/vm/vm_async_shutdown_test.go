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
	// The VM reports panic through its runtime; native writes it to stderr.
	wantStderr := ""
	if testBackend(t) == backendLLVM {
		wantStderr = "panic: boom\n"
	}
	if result.stderr != wantStderr {
		t.Fatalf("stderr: want %q, got %q", wantStderr, result.stderr)
	}
}

func TestVMRtExitDropsBufferedChannelPayloads(t *testing.T) {
	sourceCode := `async fn child() -> nothing {
    rt_exit(7);
    return nothing;
}

@entrypoint
fn main() -> int {
    let ch: own Channel<string> = Channel::<string>::new(1:uint);
    ch.send(own "hello");
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

func TestVMPanicDropsBufferedChannelPayloads(t *testing.T) {
	sourceCode := `async fn child() -> nothing {
    panic("boom");
    return nothing;
}

@entrypoint
fn main() -> int {
    let ch: own Channel<string> = Channel::<string>::new(1:uint);
    ch.send(own "boom");
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
	// This proves termination with a buffered payload, not native cleanup after _exit.
	wantStderr := ""
	if testBackend(t) == backendLLVM {
		wantStderr = "panic: boom\n"
	}
	if result.stderr != wantStderr {
		t.Fatalf("stderr: want %q, got %q", wantStderr, result.stderr)
	}
}

func TestVMNestedAsyncChildRtExitHaltsProgram(t *testing.T) {
	sourceCode := `async fn child() -> nothing {
    rt_exit(7);
    return nothing;
}

async fn parent() -> nothing {
    let task: Task<nothing> = spawn child();
    compare task.await() {
        Success(_) => return nothing;
        Cancelled() => return nothing;
    }
}

@entrypoint
fn main() -> int {
    let task: Task<nothing> = spawn parent();
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
