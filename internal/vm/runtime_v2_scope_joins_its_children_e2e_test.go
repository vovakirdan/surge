package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A scope does not answer before its children do. That is the whole of
// structured concurrency's promise: a block that started work owns it, and the
// block's own answer waits for it.
//
// The program below spawns a child that sleeps and prints, and never awaits it.
// The scope has nothing else to wait on, so the ONLY thing that can hold its
// answer back is its child count. Order is the assertion: the child's line must
// come out before the scope's, and one line arriving without the other is a
// vacuous pass, so both are required.
//
// This is the shape a hand-built stand cannot exhibit. Membership is decided by
// a claim on the child's own word, and `spawn` lowers to a wake FOLLOWED BY a
// registration -- the wake adopts the child into the waking task's scope, which
// for an ordinary spawn is the same scope the registration is for. A stand that
// builds its child by hand never runs the wake, so its registration always wins
// a claim that the real path has already taken, and it reports a count the real
// path never reaches.
const runtimeV2ScopeJoinsItsChildrenSource = `
async fn late() -> int {
    sleep(200:uint).await();
    print("child-finished");
    return 1;
}

async fn quick() -> int {
    return 2;
}

@entrypoint
fn main() -> int {
    let outcome = (async {
        let l = spawn late();
        let q = spawn quick();
        let rq = q.await();
        ret 0;
    }).await();
    print("scope-answered");
    return compare outcome {
        Success(v) => v;
        Cancelled() => 7;
    };
}
`

func TestRuntimeV2ScopeAnswersAfterItsChildren(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2ScopeJoinsItsChildrenSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	env := overrideEnvVar(baseEnv, "SURGE_SHARDS", "1")
	env = overrideEnvVar(env, "SURGE_THREADS", "1")
	duration, result := runBinaryWithTimeout(t, outputPath, env, 60*time.Second)
	if result.exitCode != 0 {
		t.Fatalf("scope join program failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode, duration, result.stdout, result.stderr)
	}
	child := strings.Index(result.stdout, "child-finished")
	scope := strings.Index(result.stdout, "scope-answered")
	if child < 0 || scope < 0 {
		t.Fatalf("both markers are required, so a missing one is a failure rather than a skip; stdout=%q", result.stdout)
	}
	if child > scope {
		t.Fatalf("the scope answered before its child finished, so it did not join it; stdout=%q", result.stdout)
	}
}
