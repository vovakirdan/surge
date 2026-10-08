package vm_test

import (
	"fmt"
	"strings"
	"testing"
)

func channelSendOwnedLifecycleSource(aggregate, cancel bool, rounds string, argv bool) (string, string) {
	decl, typ, value, valid := "", "string", `"owned payload"`, `got == "owned payload"`
	family := "string"
	if aggregate {
		family = "pair"
		decl, typ = "type Pair = { first: string, second: string };", "Pair"
		value = `Pair { first = "first payload", second = "second payload" }`
		valid = `got.first == "first payload" && got.second == "second payload"`
	}
	finish := fmt.Sprintf(`let got = compare ch.recv() { Some(v) => v; nothing => { return 3; } };
        if !(%s) { return 4; }
        let code = compare child.await() { Success(v) => v; Cancelled() => 5; };
        if code != 0 { return code; }`, valid)
	action := "deliver"
	if cancel {
		action = "cancel"
		finish = `child.cancel();
        let cancelled = compare child.await() { Success(_) => false; Cancelled() => true; };
        if !cancelled { return 6; }`
	}
	marker := "owned-" + family + "-" + action
	entry := "@entrypoint\nfn main() -> int"
	if argv {
		entry, rounds = "@entrypoint(\"argv\")\nfn main(rounds: uint) -> int", "rounds"
	}
	source := fmt.Sprintf(`%s
async fn sender(ch: Channel<%s>, ready: Channel<int>) -> int {
    let kept = "live after send";
    ready.send(1);
    let value = %s;
    ch.send(own value);
    if kept != "live after send" { return 7; }
    return 0;
}
async fn run(rounds: uint) -> int {
    let mut round = 0:uint;
    while round < rounds {
        let ch = Channel::<%s>::new(0:uint);
        let ready = Channel::<int>::new(0:uint);
        let child = spawn sender(ch, ready);
        let _ = ready.recv();
        checkpoint().await();
        %s
        ch.close();
        round = round + 1:uint;
    }
    print(%q + (round to string));
    return 0;
}
%s {
    return compare run(%s).await() { Success(v) => v; Cancelled() => 8; };
}`, decl, typ, value, typ, finish, marker+" rounds=", entry, rounds)
	return source, marker
}

func TestChannelSendOwnedLifecycle(t *testing.T) {
	for _, aggregate := range []bool{false, true} {
		for _, cancel := range []bool{false, true} {
			source, marker := channelSendOwnedLifecycleSource(aggregate, cancel, "4:uint", false)
			t.Run(marker, func(t *testing.T) {
				t.Setenv("SURGE_THREADS", "1")
				got := runProgramFromSource(t, source, runOptions{captureStdout: true})
				if got.exitCode != 0 || got.stderr != "" || strings.TrimSpace(got.stdout) != marker+" rounds=4" {
					t.Fatalf("owning send lifecycle: exit=%d stdout=%q stderr=%s", got.exitCode, got.stdout, got.stderr)
				}
			})
		}
	}
}

func TestChannelSendOwnedLifecycleValgrindBaseline(t *testing.T) {
	for _, aggregate := range []bool{false, true} {
		for _, cancel := range []bool{false, true} {
			source, marker := channelSendOwnedLifecycleSource(aggregate, cancel, "", true)
			t.Run(marker, func(t *testing.T) {
				bin := buildAsyncAllocationProgram(t, source)
				for _, workers := range []string{"1", "8"} {
					t.Run("workers-"+workers, func(t *testing.T) {
						runAsyncAllocationBaseline(t, bin, marker, asyncAllocationEnvironment(t, workers))
					})
				}
			})
		}
	}
}
