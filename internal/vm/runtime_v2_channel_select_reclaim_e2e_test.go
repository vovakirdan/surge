package vm_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A select's winning recv arm takes a value it has nowhere to put.
//
// `expr => expr` binds nothing, so the take is not undoable and the value has
// no destination. The runtime therefore destroys it through
// `rt_channel_release_payload`, and that call is the one place in a linked
// binary where a channel's payload reclamation actually fires today: the drain
// inside `rt_channel_free` is unreachable for a local channel, and the
// cancelled-receiver mailbox is only ever entered with an inert element.
//
// Nothing in the corpus exercised it with an element that owns anything. Every
// executable channel fixture in the tree is `Channel<int>`, so this path could
// stop reclaiming entirely and every gate would stay green. These two tests are
// that missing coverage, written BEFORE the typed-storage flip so that the flip
// has something to be measured against rather than something to be trusted on.
//
// Both are SLOPE tests, and the slope is not a weaker absolute — it is the only
// assertion available. A local channel is never freed at all (RV2-DEBT-155), so
// every one of these programs leaks its channels by construction; what must be
// zero is the per-take cost, which whatever leaks once per program cancels out
// of.
//
// The metric is NET ALLOCATIONS (allocs minus frees), not "definitely lost",
// and that distinction was measured rather than assumed. Deleting the reclaim
// call outright leaves "definitely lost" completely unchanged at 168 bytes in 2
// blocks for both take counts -- the abandoned payloads stay reachable from
// somewhere valgrind can see, so the leak summary cannot tell the two builds
// apart. The allocation ledger can: with the reclaim in place frees rise one
// per take (10 -> 14 across 4 -> 8 takes), and with it deleted they do not move
// at all (6 -> 6). A test written against the leak summary here would have
// passed against a runtime that reclaims nothing, which is exactly the false
// green this coverage exists to prevent.

const runtimeV2SelectReclaimStringFmt = `
async fn drain(ch: Channel<string>, stop: Channel<int>, rounds: int) -> int {
    let mut i: int = 0;
    let mut taken: int = 0;
    while i < rounds {
        let which = select {
            ch.recv() => 1;
            stop.recv() => 2;
        };
        taken = taken + which;
        i = i + 1;
    }
    return taken;
}

@entrypoint
fn main() -> int {
    let rounds: int = %d;
    let ch = Channel::<string>::new(32:uint);
    let stop = Channel::<int>::new(1:uint);
    let mut i: int = 0;
    while i < rounds {
        ch.send(own "payload-that-must-be-reclaimed-exactly-once");
        i = i + 1;
    }
    let t = spawn drain(ch, stop, rounds);
    let r = t.await();
    compare r {
        Success(v) => { if v == rounds { print("select-string-reclaim-witness"); } }
        Cancelled => print("cancelled");
    }
    return 0;
}
`

const runtimeV2SelectReclaimCompositeFmt = `
type Held = { label: string, count: int };

async fn drain(ch: Channel<Held>, stop: Channel<int>, rounds: int) -> int {
    let mut i: int = 0;
    let mut taken: int = 0;
    while i < rounds {
        let which = select {
            ch.recv() => 1;
            stop.recv() => 2;
        };
        taken = taken + which;
        i = i + 1;
    }
    return taken;
}

@entrypoint
fn main() -> int {
    let rounds: int = %d;
    let ch = Channel::<Held>::new(32:uint);
    let stop = Channel::<int>::new(1:uint);
    let mut i: int = 0;
    while i < rounds {
        let item: Held = { label = "composite-payload-that-owns-heap", count = i };
        ch.send(own item);
        i = i + 1;
    }
    let t = spawn drain(ch, stop, rounds);
    let r = t.await();
    compare r {
        Success(v) => { if v == rounds { print("select-composite-reclaim-witness"); } }
        Cancelled => print("cancelled");
    }
    return 0;
}
`

// measureSelectReclaimSlope runs the same program at two take counts and
// returns the blocks lost at each. Anything allocated once per program — the
// channels themselves, the executor — is identical in both and cancels.
func measureSelectReclaimSlope(t *testing.T, format, witness string, low, high int) (int, int) {
	t.Helper()
	measure := func(rounds int) int {
		t.Helper()
		src := fmt.Sprintf(format, rounds)
		outputPath := buildRuntimeV2CrossingSource(t, src, nil)
		env := envWithStdlib(repoRoot(t))
		stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 240*time.Second)
		if hasValgrindMemcheckError(stderr) {
			t.Fatalf("taking a payload through a select hit a memcheck error at %d rounds\nstdout:\n%s\nstderr:\n%s",
				rounds, stdout, stderr)
		}
		if exitCode != 0 {
			t.Fatalf("select program failed at %d rounds (exit=%d)\nstdout:\n%s\nstderr:\n%s",
				rounds, exitCode, stdout, stderr)
		}
		if !strings.Contains(stdout, witness) {
			t.Fatalf("the program did not reach its witness at %d rounds, so no take was measured\nstdout:\n%s",
				rounds, stdout)
		}
		outstanding, err := parseValgrindOutstandingAllocations(stderr)
		if err != nil {
			t.Fatalf("parse valgrind allocation ledger: %v\nstderr:\n%s", err, stderr)
		}
		return outstanding
	}
	return measure(low), measure(high)
}

// parseValgrindOutstandingAllocations returns allocs minus frees from the
// "total heap usage" line: how many allocations the program never returned.
func parseValgrindOutstandingAllocations(stderr string) (int, error) {
	m := valgrindHeapUsagePattern.FindStringSubmatch(stderr)
	if m == nil {
		return 0, fmt.Errorf("no \"total heap usage\" line in valgrind output")
	}
	allocs, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	if err != nil {
		return 0, fmt.Errorf("unreadable alloc count %q: %w", m[1], err)
	}
	frees, err := strconv.Atoi(strings.ReplaceAll(m[2], ",", ""))
	if err != nil {
		return 0, fmt.Errorf("unreadable free count %q: %w", m[2], err)
	}
	return allocs - frees, nil
}

var valgrindHeapUsagePattern = regexp.MustCompile(`total heap usage: ([0-9,]+) allocs, ([0-9,]+) frees`)

// A string taken by a winning recv arm is released exactly once. If it were
// released zero times the slope goes to one block per take; if it were released
// twice the run dies on a memcheck error before the slope is read at all.
func TestRuntimeV2SelectReleasesAStringPayloadExactlyOnce(t *testing.T) {
	at4, at8 := measureSelectReclaimSlope(t, runtimeV2SelectReclaimStringFmt, "select-string-reclaim-witness", 4, 8)
	if at8-at4 != 0 {
		t.Fatalf(
			"a string taken by a select's recv arm is never returned: %.2f allocations per take\n"+
				"4 takes left %d outstanding, 8 takes left %d.\n"+
				"The value has no destination -- the arm binds nothing -- so rt_channel_release_payload "+
				"is what owes its destruction.",
			float64(at8-at4)/4.0, at4, at8,
		)
	}
}

// The composite case, which is a different question rather than a bigger one.
// A composite element travels as a pointer to a box while its descriptor
// describes it unboxed, so the two drop bodies a type can have do DIFFERENT
// work here: one frees the box, the other reaches into fields. Reclaiming with
// the wrong one leaks every box silently or frees an address off the end of the
// ring. This is the row that measures which one runs.
func TestRuntimeV2SelectReleasesACompositePayloadExactlyOnce(t *testing.T) {
	at4, at8 := measureSelectReclaimSlope(t, runtimeV2SelectReclaimCompositeFmt, "select-composite-reclaim-witness", 4, 8)
	if at8-at4 != 0 {
		t.Fatalf(
			"a composite taken by a select's recv arm is never returned: %.2f allocations per take\n"+
				"4 takes left %d outstanding, 8 takes left %d.\n"+
				"Two blocks per take would be the box AND the string inside it; one would be the box "+
				"alone, which is what running the descriptor's drop instead of the box's release does.",
			float64(at8-at4)/4.0, at4, at8,
		)
	}
}
