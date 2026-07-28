//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// A value composite crosses a shard boundary correctly: the destination gets a
// value of its own, the sender keeps its own, and every box is reclaimed once.
//
// This is the contract's crossing row, and it is separate from the local one
// because the routes reach independence by DIFFERENT means, and a test that
// only checked the answer would not notice if they were confused:
//
//   - a CAPTURE is duplicated at its operand, because the sender keeps its
//     binding and the destination needs its own box;
//   - a RESULT is a transfer — the body builds the box, the caller receives it,
//     and there is never a second holder — so it must NOT be duplicated. An
//     extra copy there is invisible to any answer this file could assert, which
//     is why the reclamation census beside it is the instrument that sees it;
//   - a channel ELEMENT is duplicated at the send, like a capture.
//
// Shards are threads in one address space, so "crossed" means another thread
// holds the pointer. Independence here is therefore not a formality: without
// the duplication, the mutation below would be a data race on one box rather
// than a write to a copy.
const runtimeV2CompositeCrossingSource = `
@copy type Pair = { a: int, b: int };
@copy type Inner = { x: int };
@copy type Outer = { inner: Inner, label: int };

async fn capture_crossing() -> int {
    let mut p = Pair { a = 1, b = 2 };
    let t: far Task<int> = spawn on shard(0:ShardId) {
        ret p.a + p.b;
    };
    // Mutating AFTER the crossing was set up: the body must still see 1 and 2.
    p.a = 99;
    let crossed = compare t.await() {
        Success(v) => v;
        Cancelled() => 0 - 1;
    };
    if crossed != 3 {
        print("capture was shared, not copied");
        return 1;
    }
    if p.a != 99 {
        print("sender lost its own value");
        return 2;
    }
    return 0;
}

async fn nested_capture_crossing() -> int {
    let mut o = Outer { inner = Inner { x = 5 }, label = 7 };
    let t: far Task<int> = spawn on shard(0:ShardId) {
        ret o.inner.x + o.label;
    };
    o.inner.x = 99;
    let crossed = compare t.await() {
        Success(v) => v;
        Cancelled() => 0 - 1;
    };
    if crossed != 12 {
        print("nested capture was shared, not copied");
        return 3;
    }
    if o.inner.x != 99 {
        print("sender lost its nested value");
        return 4;
    }
    return 0;
}

async fn run_all() -> int {
    let r1 = capture_crossing().await();
    let v1 = compare r1 {
        Success(v) => v;
        Cancelled() => 0 - 1;
    };
    if v1 != 0 {
        return v1;
    }
    let r2 = nested_capture_crossing().await();
    let v2 = compare r2 {
        Success(v) => v;
        Cancelled() => 0 - 1;
    };
    if v2 != 0 {
        return v2;
    }
    print("composite-crossing-ok");
    return 0;
}

@entrypoint
fn main() -> int {
    let outcome = run_all().await();
    return compare outcome {
        Success(v) => v;
        Cancelled() => 90;
    };
}
`

func TestRuntimeV2CompositeCrossesIndependently(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CompositeCrossingSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("composite crossing hit a memcheck error (invalid free / use-after-free)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("composite crossing contract failed at row %d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "composite-crossing-ok") {
		t.Fatalf("composite crossing probe missing completion marker; stdout=%q", stdout)
	}
	// The reclamation half, and the only instrument that can see the failure
	// mode the independence rows above are blind to: a route that duplicates
	// where it should transfer allocates a box nobody needed, and a route that
	// transfers where it should duplicate is caught above.
	lostBytes, lostBlocks, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("composite crossing: %v\nstderr:\n%s", err, stderr)
	}
	if lostBytes != 0 || lostBlocks != 0 {
		t.Fatalf("composite crossing leaked %d bytes in %d blocks; want strict zero\nstderr:\n%s",
			lostBytes, lostBlocks, stderr)
	}
}
