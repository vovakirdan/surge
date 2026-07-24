package vm_test

import (
	"strings"
	"testing"
	"time"
)

// Reclamation witness for arbitrary-precision `float`.
//
// `float` has no inline form: every value is a heap block and every operation
// allocates one. Before the block carried a reference count nothing reclaimed
// them, so the leak grew with the work done — the addition loop below leaked
// 4,800 bytes in 200 blocks directly plus 7,228 indirectly in the mantissas
// those blocks own, and a longer loop leaked proportionally more.
//
// The program covers every shape that creates or hands on a reference:
//
//   - a loop that reassigns a binding, so the overwritten value must be
//     released before the new one lands;
//   - a struct literal built from literals, then read back field by field —
//     the container owns its fields, and a field read has to take its own
//     reference or the temp's release would be a second one;
//   - an array of floats walked by `for ... in`, i.e. element reads;
//   - a function that returns a fresh value, and one that returns its own
//     parameter — the parameter is BORROWED, so returning it has to mint a
//     reference rather than hand on one it never had;
//   - a division whose result is a long mantissa, so a leak shows up as bytes
//     as well as blocks.
//
// The gate is strict zero. Anything else means a reference was created without
// a matching release or vice versa.
const runtimeV2FloatReclamationSource = `
type Pair = { a: float, b: float };

fn fresh() -> float {
    let made: float = 2.5;
    return made;
}

fn echo(v: float) -> float {
    return v;
}

@entrypoint
fn main() -> int {
    let mut acc: float = 0.0;
    let mut i: int = 0;
    while i < 50 {
        acc = acc + 1.5;
        i = i + 1;
    }

    let p: Pair = Pair { a: 3.25, b: 4.75 };
    let sum: float = p.a + p.b;

    let xs: float[] = [1.5, 2.5, 3.5];
    let mut walked: float = 0.0;
    for x: float in xs {
        walked = walked + x;
    }

    let made: float = fresh();
    let echoed: float = echo(made);
    let ratio: float = 1.0 / 3.0;

    // A block expression whose value leaves through ret. That edge reaches
    // the block's exit by a jump, so the regions it skips never run the flush
    // they would have run on normal completion — their temps have to be freed
    // on the jump instead. Run it in a loop so a per-iteration leak is loud.
    let mut branched: float = 0.0;
    let mut j: int = 0;
    while j < 20 {
        let picked: float = compare j { 0 => { ret 1.5 + 2.5; }; _ => { ret 0.25 + 0.75; }; };
        branched = branched + picked;
        j = j + 1;
    }

    if acc > 0.0 && sum > 0.0 && walked > 0.0 && echoed > 0.0 && ratio > 0.0 && branched > 0.0 {
        print("float-reclamation-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2FloatReclamationValgrindZero(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FloatReclamationSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("float reclamation e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "float-reclamation-witness") {
		t.Fatalf("float reclamation e2e missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf(
			"float reclamation regressed: got %d bytes in %d blocks definitely lost, want strict zero\nstderr:\n%s",
			bytesLost, blocksLost, stderr,
		)
	}
}

// A tag constructor LOOKS like a call but is a STORE: the union it builds keeps
// the payload, which outlives the call. Passing the payload as a borrowing
// argument — correct for a real call, and what keeps the arithmetic from
// leaking — released the source while the union still pointed at it, so the
// value read back was freed memory. Symptom: "numeric size limit exceeded",
// because the corrupted word was then parsed as a bignum length.
//
// This row's gate is USE-AFTER-FREE plus the computed value, not zero leak: the
// payload a compare arm binds is still not released, because pattern bindings
// carry no drop obligation (`inferComparePatternTypes` types them without
// registering them) and compare arms push no drop scope to register them into.
// The residual is exactly one block per extraction and is pinned here so it
// cannot grow silently; it goes to zero when that binding gains a release.
const runtimeV2FloatUnionPayloadSource = `
fn boxed(k: int) -> float? {
    if k == 0 { return nothing; }
    return 1.5 + 2.5;
}

@entrypoint
fn main() -> int {
    let mut acc: float = 0.0;
    let mut k: int = 1;
    while k < 21 {
        let o: float? = boxed(k);
        let v: float = compare o { Some(x) => x; nothing => 0.0; };
        acc = acc + v;
        k = k + 1;
    }
    print(acc to string);
    print("float-union-payload-witness");
    return 0;
}
`

func TestRuntimeV2FloatUnionPayloadSurvivesItsConstructor(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2FloatUnionPayloadSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error — the boxed payload was freed while the union still held it\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("float union payload e2e failed (exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "float-union-payload-witness") {
		t.Fatalf("missing completion marker; stdout=%q", stdout)
	}
	// 20 iterations x (1.5 + 2.5) = 80. A freed payload reads as garbage and
	// either panics or prints something else, so the value is the sharpest
	// check that the union really carried a live block.
	if !strings.HasPrefix(stdout, "80\n") {
		t.Fatalf("boxed float payload did not survive its constructor; want the sum 80 first, stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	const knownResidualBlocks = 20 // one per compare-arm payload extraction
	if blocksLost > knownResidualBlocks {
		t.Fatalf("union payload leak GREW past the recorded residual: %d bytes in %d blocks, want at most %d blocks\nstderr:\n%s",
			bytesLost, blocksLost, knownResidualBlocks, stderr)
	}
}
