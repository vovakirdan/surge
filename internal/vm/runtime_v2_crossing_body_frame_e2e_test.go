package vm_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"surge/internal/buildpipeline"
	"surge/internal/diag"
)

// TC-XB (RV2-DEBT-378) end to end. The borrowing program is the review's q18: a task published hot through a
// plain fn borrows the `on` body's copy of a parameter, the body ends, and the caller ends in `panic`. It
// diagnosed clean and crashed natively (SIGSEGV) before TC-XB; now the build refuses it on both backends at the
// body's `ret`. Its twin passes the parameter BY VALUE: it is accepted, the VM refuses it as unexecutable
// (FUT7014: the VM has no cross-shard transport), and natively it runs to its `panic` at one shard, printing
// only the value it was given. The native run goes through runBinaryWithTimeout, which kills the process group
// on its deadline, so a hang is a red row and never a hung gate (RV2-DEBT-344).

const crossingFrameOnHotTaskSource = `async fn peek(x: &int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = *x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let ft = on distributed {
        let h = keep(peek(&s));
        ret 1;
    };
    let r = compare ft {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const crossingFrameOnByValueSource = `async fn peek(x: int) -> int {
    checkpoint().await();
    checkpoint().await();
    let n: int = x;
    print("peek " + (n to string));
    return n;
}

fn keep(t: Task<int>) -> Task<int> {
    return spawn t;
}

fn mk(n: int) -> string {
    return "abcdefghij" + (n to string);
}

async fn run(s: int) -> nothing {
    let ft = on distributed {
        let h = keep(peek(s));
        ret 1;
    };
    let r = compare ft {
        Success(v) => v;
        Cancelled() => 0;
    };
    let mut i: int = 0;
    while i < 2000 {
        let junk: string = mk(i);
        checkpoint().await();
        i = i + 1;
    }
    panic("stop");
}

@entrypoint
fn main() -> int {
    return compare run(424242).await() {
        Success(_) => 0;
        Cancelled() => 1;
    };
}
`

const (
	crossingFrameOnHotTaskSourceDigest = "c723e54fe8e587d8ba5caa741ccebdbafecb16015b88e26f1f95c4b4270245ed"
	crossingFrameOnByValueSourceDigest = "02dc7cdb2c15a0458b51422c72b431e2cbd3203baded637c2daa47bd560be017"
)

// crossingFrameCompile runs the build pipeline's front half for this test's backend and answers the sorted set of
// error codes and every error message: a refusal is an answer here, not a failure.
func crossingFrameCompile(t *testing.T, name, text, digest string) (string, []string) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen probe changed: %s", got)
	}
	root := repoRoot(t)
	t.Setenv("SURGE_STDLIB", root)
	dir := t.TempDir()
	path := filepath.Join(dir, name+".sg")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := buildpipeline.BackendVM
	if testBackend(t) == backendLLVM {
		backend = buildpipeline.BackendLLVM
	}
	res, err := buildpipeline.Compile(t.Context(), &buildpipeline.CompileRequest{
		TargetPath: path, BaseDir: dir, MaxDiagnostics: 64, Backend: backend, Analysis: true,
	})
	seen := map[string]bool{}
	var messages []string
	if res.Diagnose != nil && res.Diagnose.Bag != nil {
		for _, d := range res.Diagnose.Bag.Items() {
			if d.Severity >= diag.SevError {
				seen[d.Code.ID()] = true
				messages = append(messages, d.Message)
			}
		}
	}
	if err != nil && len(seen) == 0 {
		t.Fatalf("PRECONDITION: the build stopped with no diagnostic: %v", err)
	}
	codes := make([]string, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	t.Logf("CROSSING_FRAME %s codes %q messages %q", name, codes, messages)
	return strings.Join(codes, ","), messages
}

// 3 RUN: 1 parent, 2 leaves; each leaf on the backend SURGE_BACKEND names.
func TestRuntimeV2CrossingBodyTaskOverItsCopy(t *testing.T) {
	t.Run("borrowing_body_task_is_refused", func(t *testing.T) {
		codes, messages := crossingFrameCompile(t, "tcxb_on_uaf", crossingFrameOnHotTaskSource, crossingFrameOnHotTaskSourceDigest)
		for _, m := range messages {
			if m == "a task still borrows 's' at this ret" {
				return
			}
		}
		t.Fatalf("codes %q messages %q, want a SEM3021 refusal %q on the %s backend: the task outlives the body's copy (TC-XB)",
			codes, messages, "a task still borrows 's' at this ret", testBackend(t))
	})
	t.Run("by_value_twin_runs", func(t *testing.T) {
		codes, _ := crossingFrameCompile(t, "tcxb_on_byvalue", crossingFrameOnByValueSource, crossingFrameOnByValueSourceDigest)
		if testBackend(t) != backendLLVM {
			if codes != "FUT7014" {
				t.Fatalf("codes %q, want exactly FUT7014: the VM cannot execute an `on` crossing, and sema must accept the twin", codes)
			}
			return
		}
		if codes != "" {
			t.Fatalf("codes %q, want none: the by-value twin is a sound program", codes)
		}
		skipTimeoutTests(t)
		outputPath := buildRuntimeV2CrossingSource(t, crossingFrameOnByValueSource, nil)
		env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "1")
		_, res := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
		ok := res.exitCode == 1 && strings.Contains(res.stderr, "panic: stop")
		for _, line := range strings.Split(strings.TrimSpace(res.stdout), "\n") {
			ok = ok && (line == "" || line == "peek 424242")
		}
		if !ok {
			t.Fatalf("exit %d stdout %q stderr %q, want exit 1 with \"panic: stop\" and no line but \"peek 424242\"",
				res.exitCode, res.stdout, res.stderr)
		}
	})
}
