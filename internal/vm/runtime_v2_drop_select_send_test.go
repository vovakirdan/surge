//go:build !golden

package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Select send-arm ownership: exactly one arm wins, so the payload of a
// send arm moves only where that arm won. A winning send delivers the
// value (the receiver frees it, exactly once); a losing send reclaims the
// payload via per-arm drop synthesis inside the winning arm's block.
// Assertions ride exact free_count deltas around print-free windows;
// payloads are runtime-built (string literals are static and never touch
// the heap, so they cannot witness reclamation).
const runtimeV2DropSelectSendSource = `
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn check_frees(win: string, before: &HeapStats, after: &HeapStats, want: uint) -> int {
    let f: uint = after.free_count - before.free_count;
    if f != want {
        print("FAIL ");
        print(win);
        print(" frees=");
        print(f to string);
        print(" want=");
        print(want to string);
        print("\n");
        return 1;
    }
    return 0;
}

async fn run() -> int {
    // Winner path: the send arm is immediately ready and wins; the
    // delivered payload is freed exactly once, by the receiver's drop.
    let ch = make_channel::<string>(1);
    let rx = make_channel::<int>(1);
    let s = build("w-");
    let a0: HeapStats = rt_heap_stats();
    let v = select {
        ch.send(own s) => 1;
        rx.recv() => 2;
    };
    let a1: HeapStats = rt_heap_stats();
    let got = ch.recv();
    let a2: HeapStats = rt_heap_stats();
    @drop got;
    let a3: HeapStats = rt_heap_stats();
    if v != 1 { print("FAIL winner-arm\n"); return 11; }
    // The select window must NOT free the delivered payload.
    let r1: int = check_frees("winner-select", &a0, &a1, 2:uint);
    if r1 != 0 { return 12; }
    let r2: int = check_frees("winner-recv", &a1, &a2, 0:uint);
    if r2 != 0 { return 13; }
    // The payload string + Option box free HERE, exactly once.
    let r3: int = check_frees("winner-drop-got", &a2, &a3, 2:uint);
    if r3 != 0 { return 14; }

    // Loser path: the send arm cannot proceed (channel full) and the
    // recv arm wins; the payload is reclaimed inside the winning arm.
    let ch2 = make_channel::<string>(1);
    let rx2 = make_channel::<int>(1);
    let blocker = build("b-");
    ch2.send(own blocker);
    rx2.send(7);
    let s2 = build("l-");
    let b0: HeapStats = rt_heap_stats();
    let v2 = select {
        ch2.send(own s2) => 1;
        rx2.recv() => 2;
    };
    let b1: HeapStats = rt_heap_stats();
    if v2 != 2 { print("FAIL loser-arm\n"); return 21; }
    let r4: int = check_frees("loser-select", &b0, &b1, 3:uint);
    if r4 != 0 { return 22; }

    print("select-send-drop-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2DropSelectSendArm(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2DropSelectSendSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	for _, threads := range []string{"1", "2"} {
		t.Run(fmt.Sprintf("threads_%s", threads), func(t *testing.T) {
			env := overrideEnvVar(baseEnv, "SURGE_THREADS", threads)
			var result runResult
			for attempt := 1; attempt <= 3; attempt++ {
				_, result = runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
				// RV2-DEBT-049: the runtime can lose the entrypoint task at
				// startup and exit 0 with no output. Retry ONLY that exact
				// signature so the soundness assertions still gate.
				if result.exitCode == 0 && strings.TrimSpace(result.stdout) == "" {
					t.Logf("RV2-DEBT-049 signature (exit 0, empty stdout) on attempt %d; retrying", attempt)
					continue
				}
				break
			}
			if result.exitCode != 0 {
				t.Fatalf("select-send drop e2e failed (exit=%d)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode, result.stdout, result.stderr)
			}
			if strings.Contains(result.stdout, "FAIL") {
				t.Fatalf("select-send drop e2e assertion failed:\n%s", result.stdout)
			}
			if !strings.Contains(result.stdout, "select-send-drop-ok") {
				t.Fatalf("select-send drop e2e missing execution witness; stdout=%q\nstderr:%s", result.stdout, result.stderr)
			}
		})
	}
}
