package vm_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// These are compiler-to-runtime checks of real counted families. The native
// lifecycle stand separately witnesses a staged TASK_WAITING before each
// cancellation at one/eight workers; checkpoints alone do not prove that
// ordering with multiple workers.
func channelSendOfferProgram(handle, cancel bool) (string, string) {
	typ, initial, post, checkGot, checkOriginal := "float", "1.5",
		"if kept != 1.5 { return 8; }",
		"if got != 1.5 { return 7; }",
		"if kept != 1.5 { return 9; }"
	family := "float"
	if handle {
		family, typ, initial = "channel", "Channel<int32>", "Channel::<int32>::new(1:uint)"
		post = "kept.send(17:int32);"
		checkGot = `let value = compare got.recv() { Some(x) => x; nothing => 0:int32; };
            if value != 17:int32 { return 7; }`
		checkOriginal = `kept.send(23:int32);
            let value = compare kept.recv() { Some(x) => x; nothing => 0:int32; };
            if value != 23:int32 { return 9; }`
	}
	action, repeats := "answer", 1
	finish := fmt.Sprintf(`
    compare ch.recv() {
        Some(got) => { %s }
        nothing => { return 7; }
    };
    let code = compare p.await() { Success(code) => code; Cancelled() => 91; };
    if code != 0 { return code; }
    %s`, checkGot, checkOriginal)
	if cancel {
		action, repeats = "cancel", 16
		finish = `p.cancel();
    let cancelled = compare p.await() { Success(_) => false; Cancelled() => true; };
    if !cancelled { return 7; }
    ` + checkOriginal
	}
	marker := "offer-" + family + "-" + action + "-ok"
	body := fmt.Sprintf(`{
    let ch = Channel::<%s>::new(0:uint);
    let kept = %s;
    let p: Task<int> = spawn producer(ch, kept);
    checkpoint().await();
    checkpoint().await();
    %s
}
`, typ, initial, finish)
	src := fmt.Sprintf(`
async fn producer(ch: Channel<%s>, kept: %s) -> int {
    ch.send(kept);
    %s
    return 0;
}
async fn run() -> int {
    %s
    print("%s");
    return 0;
}
@entrypoint
fn main() -> int {
    let task = spawn run();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`, typ, typ, post, strings.Repeat(body, repeats), marker)
	return src, marker
}

func TestRuntimeV2ChannelSendOfferPreservesOriginal(t *testing.T) {
	for _, handle := range []bool{false, true} {
		for _, cancel := range []bool{false, true} {
			src, marker := channelSendOfferProgram(handle, cancel)
			for _, workers := range []string{"1", "8"} {
				t.Run(marker+"/workers-"+workers, func(t *testing.T) {
					t.Setenv("SURGE_SHARDS", "1")
					t.Setenv("SURGE_THREADS", workers)
					result := runProgramFromSource(t, src, runOptions{captureStdout: true})
					if result.exitCode != 0 || result.stderr != "" ||
						strings.TrimSpace(result.stdout) != marker {
						t.Fatalf("offer program failed (code=%d)\nstdout:\n%s\nstderr:\n%s", result.exitCode, result.stdout, result.stderr)
					}
				})
			}
		}
	}
}

func TestRuntimeV2ChannelSendOfferValgrindZero(t *testing.T) {
	for _, handle := range []bool{false, true} {
		for _, cancel := range []bool{false, true} {
			src, marker := channelSendOfferProgram(handle, cancel)
			t.Run(marker, func(t *testing.T) {
				bin := buildRuntimeV2CrossingSource(t, src, nil)
				for _, workers := range []string{"1", "8"} {
					t.Run("workers-"+workers, func(t *testing.T) {
						env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "1")
						env = overrideEnvVar(env, "SURGE_THREADS", workers)
						stdout, stderr, code := runBinaryUnderValgrind(t, bin, env, 120*time.Second)
						if code != 0 || hasValgrindMemcheckError(stderr) || !strings.Contains(stdout, marker) {
							t.Fatalf("offer valgrind failed (code=%d)\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
						}
						if strings.Count(stderr, "ERROR SUMMARY:") != 1 ||
							!strings.Contains(stderr, "ERROR SUMMARY: 0 errors from 0 contexts") {
							t.Fatalf("offer valgrind requires exactly one zero-error summary\nstderr:\n%s", stderr)
						}
						inUseBytes, inUseBlocks := parseValgrindInUseAtExit(t, stderr)
						if inUseBytes != 0 || inUseBlocks != 0 {
							t.Fatalf("offer physical heap at exit: bytes=%d blocks=%d, want zero\nstderr:\n%s", inUseBytes, inUseBlocks, stderr)
						}
						bytes, blocks, err := parseValgrindDefinitelyLost(stderr)
						if err != nil || bytes != 0 || blocks != 0 {
							t.Fatalf("offer leak census: bytes=%d blocks=%d err=%v\nstderr:\n%s", bytes, blocks, err, stderr)
						}
					})
				}
			})
		}
	}
}
