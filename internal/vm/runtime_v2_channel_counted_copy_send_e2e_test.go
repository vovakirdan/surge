package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A channel send of a Copy value that carries a counted block — a `float`, a
// `@copy` composite with one inside — on the three local paths that took the
// caller's bits without a reference of their own:
//
//   - the SUSPENDING send of an async body (InstrChanSend), which never reached
//     retainStoredRefCountedArgs: counted scalar/handle Copy now offers a fresh
//     retained reference per poll, while composite Copy still uses its prelude
//     clone in a transfer temp;
//   - the SEND arm of a local select, whose winner's value the runtime moves out
//     of the caller's own storage while a losing arm's stays put: the winning arm
//     takes a reference of its own at the head of its body;
//   - a `@copy` composite on every send, whose clone the emitter built and then
//     dropped on the floor while the runtime moved the caller's original.
//
// Red on the tree before: the first program printed `second=small` for 2.5 > 2.0
// and `0` for a live 1.5 (the block was freed under the channel; valgrind:
// invalid reads and writes), the second died with exit 255 reading the value
// the channel delivered, and the third leaked one block per park (valgrind: 24
// bytes definitely lost).
const runtimeV2AsyncChannelFloatSendSource = `
async fn run() -> int {
    let ch = Channel::<float>::new(2:uint);
    let kept = 1.5;
    ch.send(kept);
    ch.send(2.5);
    let first = ch.recv();
    compare first {
        Some(x) => { if x > 1.0 { print("first=big"); } else { print("first=small"); } }
        nothing => print("first=none");
    }
    let second = ch.recv();
    compare second {
        Some(y) => { if y > 2.0 { print("second=big"); } else { print("second=small"); } }
        nothing => print("second=none");
    }
    print(kept to string);
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

const runtimeV2SelectFloatSendSource = `
async fn run() -> int {
    let ch = Channel::<float>::new(1:uint);
    let stop = Channel::<int>::new(1:uint);
    let kept = 1.5;
    let first = select {
        ch.send(kept) => 1;
        stop.recv() => 2;
    };
    print(first to string);
    stop.send(7);
    let second = select {
        ch.send(kept) => 1;
        stop.recv() => 2;
    };
    print(second to string);
    compare ch.recv() {
        Some(x) => print(x to string);
        nothing => print("none");
    }
    print(kept to string);
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

// The producer's second send finds the channel full and parks; it is polled
// again once the consumer takes a value. The clone the send hands the channel
// is made once, in the prelude, whichever poll commits it.
const runtimeV2ParkedCopyUnionSendSource = `
@copy
type P = { v: float };
tag Held(P);
tag Empty();
@copy
type U = Held(P) | Empty();

async fn producer(ch: Channel<U>, a: float) -> int {
    let u: U = Held(P{ v: a });
    ch.send(u);
    ch.send(u);
    return 0;
}

async fn run() -> int {
    let ch = Channel::<U>::new(1:uint);
    let a: float = 2.5;
    let filler: U = Held(P{ v: a });
    ch.send(filler);
    let p = spawn producer(ch, a);
    checkpoint().await();
    checkpoint().await();
    let r1 = ch.recv();
    checkpoint().await();
    let r2 = ch.recv();
    checkpoint().await();
    let r3 = ch.recv();
    let code = compare p.await() {
        Success(c) => c;
        Cancelled() => 91;
    };
    print(a to string);
    return code;
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

func TestRuntimeV2ChannelSendOfACountedCopyKeepsEveryOwnerHonest(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		stdout []string
	}{
		{
			name:   "async send of a float",
			src:    runtimeV2AsyncChannelFloatSendSource,
			stdout: []string{"first=big", "second=big", "1.5E+0"},
		},
		{
			name:   "select send arm of a float, winning and losing",
			src:    runtimeV2SelectFloatSendSource,
			stdout: []string{"1", "2", "1.5E+0", "1.5E+0"},
		},
		{
			name:   "parked async send of a copy union carrying a float",
			src:    runtimeV2ParkedCopyUnionSendSource,
			stdout: []string{"2.5E+0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputPath := buildRuntimeV2CrossingSource(t, tc.src, nil)
			env := envWithStdlib(repoRoot(t))
			_, result := runBinaryWithTimeout(t, outputPath, env, 30*time.Second)
			if result.exitCode != 0 {
				t.Fatalf("program exit=%d\nstdout:\n%s\nstderr:\n%s", result.exitCode, result.stdout, result.stderr)
			}
			lines := strings.Split(strings.TrimSpace(result.stdout), "\n")
			if strings.Join(lines, "|") != strings.Join(tc.stdout, "|") {
				t.Fatalf("stdout = %q, want %q", lines, tc.stdout)
			}
		})
	}
}

func TestRuntimeV2ChannelSendOfACountedCopyValgrindZero(t *testing.T) {
	for name, src := range map[string]string{
		"async send of a float":             runtimeV2AsyncChannelFloatSendSource,
		"select send arm of a float":        runtimeV2SelectFloatSendSource,
		"parked async send of a copy union": runtimeV2ParkedCopyUnionSendSource,
	} {
		t.Run(name, func(t *testing.T) {
			outputPath := buildRuntimeV2CrossingSource(t, src, nil)
			env := envWithStdlib(repoRoot(t))
			stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
			if hasValgrindMemcheckError(stderr) {
				t.Fatalf("valgrind reported a memcheck error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if exitCode != 0 {
				t.Fatalf("program exit=%d under valgrind\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
			}
			bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
			if err != nil {
				t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
			}
			if bytesLost != 0 || blocksLost != 0 {
				t.Fatalf("%d bytes in %d blocks definitely lost, want strict zero\nstderr:\n%s", bytesLost, blocksLost, stderr)
			}
		})
	}
}
