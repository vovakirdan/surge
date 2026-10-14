//go:build runtime_v2_pending

package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The parent is deliberately synchronous source main. Wrapping it in an
// async helper would hide the second executor lane that RV2-DEBT-488 exposed.
const runtimeV2RootCountedScalarSource = `async fn churn(x: int, rounds: int) -> int {
    let mut i: int = 0;
    let mut ok: int = 0;
    while i < rounds {
        let y: int = x;
        if y == x { ok = ok + 1; };
        i = i + 1;
    };
    return ok;
}
@entrypoint
fn main() -> int {
    let shared: int = 9223372036854775808;
    let rounds: int = 200000;
    let child = spawn churn(shared, rounds);
    let mut i: int = 0;
    let mut ok: int = 0;
    while i < rounds {
        let y: int = shared;
        if y == shared { ok = ok + 1; };
        i = i + 1;
    };
    return compare child.await() {
        Success(n) => ok + n == 400000 ? 0 : 2;
        Cancelled() => 3;
    };
}
`

func TestRuntimeV2EntrypointCountedScalarStaysOnOneCarrier(t *testing.T) {
	ensureLLVMToolchain(t)
	skipTimeoutTests(t)
	bin := buildLLVMProgramFromSource(t, runtimeV2RootCountedScalarSource)
	for _, shards := range []int{1, 2, 8} {
		t.Run(fmt.Sprintf("shards-%d", shards), func(t *testing.T) {
			env := envWithStdlib(repoRoot(t))
			env = overrideEnvVar(env, "SURGE_SHARDS", fmt.Sprint(shards))
			env = overrideEnvVar(env, "SURGE_BLOCKING_THREADS", "1")
			for i := range 20 {
				dur, res := runBinaryWithTimeout(t, bin, env, 30*time.Second)
				if res.exitCode != 0 || res.stdout != "" || res.stderr != "" {
					t.Fatalf("root run %d failed (exit=%d, dur=%s)\nstdout:\n%s\nstderr:\n%s",
						i, res.exitCode, dur, res.stdout, res.stderr)
				}
			}
		})
	}
}

func TestRuntimeV2RootKeepsNoStdUserNames(t *testing.T) {
	const sourceCode = `pragma no_std;
type Task = bool;
type TaskResult = int;
fn __entry_error_code(n: int) -> int { return n + 1; }
fn __surge_root() -> int { return 3; }
fn __surge_root_entry() -> int { return 4; }
@entrypoint
fn main() -> int {
    let ready: Task = true;
    let answer: TaskResult = __entry_error_code(34) + __surge_root() + __surge_root_entry();
    return ready && answer == 42 ? 0 : 1;
}

`
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			result := runProgramFromSource(t, sourceCode, runOptions{})
			if result.exitCode != 0 || result.stderr != "" {
				t.Fatalf("private root ABI captured a user name: exit=%d stderr=%q", result.exitCode, result.stderr)
			}
		})
	}
}

func TestRuntimeV2RootLocalSelectUsesCoroutine(t *testing.T) {
	const source = `@entrypoint fn main() -> int {
    let ch = Channel::<int>::new(1:uint);
    return select { ch.send(7) => 0; };
}`
	for _, backend := range []string{backendVM, backendLLVM} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv(backendEnvVar, backend)
			result := runProgramFromSource(t, source, runOptions{})
			if result.exitCode != 0 || result.stderr != "" {
				t.Fatalf("root select did not lower: exit=%d stderr=%q", result.exitCode, result.stderr)
			}
		})
	}
}

func TestRuntimeV2RootBorrowSurvivesSuspension(t *testing.T) {
	for _, row := range []struct{ name, source string }{
		{"captured-block", `@entrypoint fn main() -> int {
    let seed: int = 9223372036854775808;
    let child = async {
        checkpoint().await();
        ret seed + 1;
    };
    return compare child.await() {
        Success(n) => n == 9223372036854775809 ? 0 : 1;
        Cancelled() => 2;
    };
}`},
		{"shared", `async fn read(value: &string) -> uint {
    checkpoint().await();
    checkpoint().await();
    return value.__len();
}
@entrypoint fn main() -> int {
    let value = "root storage must survive suspension";
    let expected = value.__len();
    let child = spawn read(&value);
    checkpoint().await();
    return compare child.await() { Success(n) => n == expected ? 0 : 1; Cancelled() => 2; };
}`},
		{"mutable", `async fn update(value: &mut int) -> nothing {
    checkpoint().await();
    *value = *value + 1;
    return nothing;
}
@entrypoint fn main() -> int {
    let mut value: int = 9223372036854775808;
    let child = spawn update(&mut value);
    checkpoint().await();
    let _ = child.await();
    return value == 9223372036854775809 ? 0 : 1;
}`},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv(backendEnvVar, backendVM)
			result := runProgramFromSource(t, row.source, runOptions{})
			if result.exitCode != 0 || result.stderr != "" {
				t.Fatalf("VM root borrow: exit=%d stderr=%q", result.exitCode, result.stderr)
			}
			bin := buildLLVMProgramFromSource(t, row.source)
			for _, shards := range []string{"1", "2", "8"} {
				env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", shards)
				_, result := runBinaryWithTimeout(t, bin, env, 30*time.Second)
				if result.exitCode != 0 || result.stderr != "" {
					t.Fatalf("native root borrow at shards=%s: exit=%d stderr=%q", shards, result.exitCode, result.stderr)
				}
			}
			env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "2")
			stdout, stderr, code := runBinaryUnderValgrind(t, bin, env, 120*time.Second)
			if code != 0 || stdout != "" || hasValgrindMemcheckError(stderr) || !strings.Contains(stderr, "All heap blocks were freed") {
				t.Fatalf("root borrow lifetime: exit=%d stdout=%q\n%s", code, stdout, stderr)
			}
		})
	}
}
