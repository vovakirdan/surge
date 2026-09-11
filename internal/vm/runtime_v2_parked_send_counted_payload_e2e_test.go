package vm_test

import (
	"fmt"
	"strings"
	"testing"
)

// These are compiler-to-runtime checks of real counted families. The native
// lifecycle stand separately witnesses a staged TASK_WAITING before each
// cancellation at one/eight workers; checkpoints alone do not prove that
// ordering with multiple workers.
func channelSendOfferProgram(handle, cancel bool) (string, string) {
	return channelSendOfferSource(handle, cancel, false)
}

func channelSendOfferSource(handle, cancel, countFromArgv bool) (string, string) {
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
	runParameter, runArgument := "", ""
	entrypoint := "@entrypoint\nfn main() -> int"
	workload := strings.Repeat(body, repeats)
	witness := fmt.Sprintf("print(%q);", marker)
	if countFromArgv {
		runParameter, runArgument = "rounds: uint", "rounds"
		entrypoint = "@entrypoint(\"argv\")\nfn main(rounds: uint) -> int"
		workload = "let mut round: uint = 0:uint;\nwhile round < rounds {\n" + body +
			"round = round + 1:uint;\n}\n"
		witness = fmt.Sprintf("print(%q + (round to string));", marker+" rounds=")
	}
	src := fmt.Sprintf(`
async fn producer(ch: Channel<%s>, kept: %s) -> int {
    ch.send(kept);
    %s
    return 0;
}
async fn run(%s) -> int {
    %s
    %s
    return 0;
}
%s {
    let task = spawn run(%s);
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`, typ, typ, post, runParameter, workload, witness, entrypoint, runArgument)
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

// Physical runtime bootstrap and worker TLS survive process exit. This checks
// zero additional allocations against a fully attributed control in the SAME
// executable, with full symbolic stacks and an independent logical census.
// Park-slot reclamation before channel teardown is proved by the parked native
// cancellation stand; process XML alone cannot witness an interior pool slot.
func TestRuntimeV2ChannelSendOfferValgrindBaseline(t *testing.T) {
	for _, handle := range []bool{false, true} {
		for _, cancel := range []bool{false, true} {
			src, marker := channelSendOfferSource(handle, cancel, true)
			t.Run(marker, func(t *testing.T) {
				bin := buildAsyncAllocationProgram(t, src)
				for _, workers := range []string{"1", "8"} {
					t.Run("workers-"+workers, func(t *testing.T) {
						runAsyncAllocationBaseline(t, bin, marker, asyncAllocationEnvironment(t, workers))
					})
				}
			})
		}
	}
}
