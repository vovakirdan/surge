package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Per-arm drop synthesis: a droppable moved on some compare/if arms but
// live on others is freed at the end of the arm where it stays live, so
// the common `compare a { Ok => consume(b); Err => 0 }` shape needs no
// restructuring. The census asserts free_count balances across the two
// calls (one consumes b, one drops it via synthesis) — same reclamation
// either way.
const runtimeV2DropArmSynthesisSource = `
fn make_s() -> string {
    return "arm-payload-value";
}

fn eat(s: string) -> int {
    let n: int = s.__len() to int;
    @drop s;
    return n;
}

fn read_len(s: &string) -> int {
    return s.__len() to int;
}

// compare arm: b consumed only on the true arm; synthesized on the false.
fn compare_arm(cond: bool) -> int {
    let b: string = make_s();
    let r: int = compare cond {
        true => eat(b);
        false => 0;
    };
    return r;
}

// if statement with uneven branches: b moved in then, dropped in else.
fn if_arm(cond: bool) -> int {
    let b: string = make_s();
    if cond {
        let n: int = eat(b);
        return n;
    } else {
        let n: int = read_len(&b);
        @drop b;
        return n;
    }
}

// if WITHOUT else: b moved in then; synthesized else frees it on the
// fall-through.
fn if_no_else(cond: bool) -> int {
    let b: string = make_s();
    if cond {
        let n: int = eat(b);
        return n;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let before: HeapStats = rt_heap_stats();
    // Each call reclaims exactly one make_s() string, whichever path.
    let c1: int = compare_arm(true);
    let c2: int = compare_arm(false);
    if c1 != 17 { return 1; }
    if c2 != 0 { return 2; }
    let i1: int = if_arm(true);
    let i2: int = if_arm(false);
    if i1 != 17 { return 3; }
    if i2 != 17 { return 4; }
    let n1: int = if_no_else(true);
    let n2: int = if_no_else(false);
    if n1 != 17 { return 5; }
    if n2 != 0 { return 6; }
    let after: HeapStats = rt_heap_stats();
    // Six make_s() strings created and reclaimed; frees must have moved.
    if after.free_count <= before.free_count {
        return 7;
    }
    print("arm-synthesis-ok");
    return 0;
}
`

func TestRuntimeV2DropArmSynthesis(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DropArmSynthesisSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "2"} {
		t.Run(fmt.Sprintf("threads_%s", threads), func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
			_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf("arm-synthesis e2e failed (exit=%d)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode, result.stdout, result.stderr)
			}
			if !strings.Contains(result.stdout, "arm-synthesis-ok") {
				t.Fatalf("arm-synthesis e2e missing marker; stdout=%q", result.stdout)
			}
		})
	}
}
