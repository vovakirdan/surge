package vm_test

import (
	"strings"
	"testing"
	"time"

	"surge/internal/diag"
)

// Reclamation witness for the overwritten-value obligation at a PLACE.
//
// Assigning a heap-owning value over a place must free what it displaces. A
// whole binding always did. The four shapes below — a struct field, a field one
// level down, a tuple element, and a store through `&mut` — did not, natively.
//
// Not because sema failed to record the obligation for one lane and not the
// other: both lanes run the same sema. The obligation was recorded for NO ONE,
// and the VM looked correct only because its store releases what it reads out
// of the cell on its way past. The native store is a bare store, so the
// displaced value simply stayed allocated: 2,048 bytes in 32 blocks here,
// exactly four shapes times the eight iterations below.
//
// The gate is strict zero, and the loop is what makes it a gate rather than an
// anecdote — a per-iteration shortfall is eight blocks, not one, so it cannot
// hide inside noise.
const runtimeV2PlaceOverwriteSource = `
type Item = { name: string, id: int };
type Holder = { inner: Item };

fn mk(n: int) -> string {
    return "displaced-value-long-enough-to-reach-the-heap-" + (n to string);
}

fn overwrite_through_ref(target: &mut string, n: int) -> nothing {
    *target = mk(n);
    return nothing;
}

@entrypoint
fn main() -> int {
    let mut it: Item = Item { name = mk(0), id = 0 };
    let mut h: Holder = Holder { inner = Item { name = mk(0), id = 0 } };
    let mut t: (string, int) = (mk(0), 0);
    let mut s: string = mk(0);

    let mut i: int = 0;
    while i < 8 {
        it.name = mk(i + 1);
        h.inner.name = mk(i + 1);
        t.0 = mk(i + 1);
        overwrite_through_ref(&mut s, i + 1);
        i = i + 1;
    }

    // Read every shape back without taking it out: a partial move would empty
    // the container and change what the scope still owes at exit, which is the
    // very thing being measured.
    if rt_string_len(&it.name) > 0:uint
        && rt_string_len(&h.inner.name) > 0:uint
        && rt_string_len(&t.0) > 0:uint
        && rt_string_len(&s) > 0:uint {
        print("place-overwrite-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2PlaceOverwriteReclamationValgrindZero(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2PlaceOverwriteSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	// Checked before the leak numbers: releasing a place the program had
	// already given away, or one its container still owns, shows up here as an
	// invalid free rather than as a leak, and reporting "zero bytes lost" for a
	// double free would be the worst possible reading of this run.
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("valgrind reported a real memcheck error (invalid free / use-after-free / uninitialized read)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("place overwrite e2e failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "place-overwrite-witness") {
		t.Fatalf("place overwrite e2e missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf(
			"assigning over a place stopped freeing what it displaced: got %d bytes in %d blocks definitely lost, want strict zero\nstderr:\n%s",
			bytesLost, blocksLost, stderr,
		)
	}
}

// A LOOP BINDING DOES NOT OWN WHAT IT HOLDS, and this is the witness for it.
//
// `for it in xs` binds a word-wise COPY of the element — MIR shows
// `tag_payload_move copy`, and the frame emits no drop for the binding, because
// the array is still the owner. Anything that frees THROUGH that binding frees
// the array's storage, which the array then frees again on its own reclamation.
//
// Both spellings are covered because they reach the obligation by different
// routes and they were NOT in the same state before this was found: assigning a
// whole loop binding (`s = mk(7)`) went through recordReassignOldDrop and was
// ALREADY an invalid free at the time — valgrind reported 7 errors in 3 contexts
// including `Invalid free()`, and the program aborted with `free(): double free
// detected in tcache 2`. Assigning a PLACE under it (`it.name = mk(7)`) was
// merely a silent leak, and became an invalid free the moment the place
// obligation was recorded. That is this project's own pattern once more: the
// leak was the only thing keeping the second owner from freeing what the first
// one would.
//
// THE PROGRAM BELOW NO LONGER COMPILES, and that is the stronger outcome.
// RV2-DEBT-212's owner ruling made `for-in` read-only forever, so both
// spellings are refused by sema as SEM3201 and the invalid free this test was
// written to catch is unreachable from source rather than merely guarded
// against. The test therefore asserts the REFUSAL: the guards in
// recordReassignOldDrop and reinitializeAssignedPlace stay as defence for any
// future producer of markNonOwningBinding, and the program that would exercise
// them cannot be written.
//
// The residual 192-byte leak this program used to have — the value assigned to
// the copy was lost, the array never saw it — went with the shape.
const runtimeV2LoopBindingOverwriteSource = `
type Item = { name: string, id: int };

fn mk(n: int) -> string {
    return "displaced-value-long-enough-to-reach-the-heap-" + (n to string);
}

@entrypoint
fn main() -> int {
    let mut xs: Item[] = Array::<Item>::with_len(3:uint);
    xs[0] = Item { name = mk(0), id = 0 };
    xs[1] = Item { name = mk(1), id = 1 };
    xs[2] = Item { name = mk(2), id = 2 };
    for it in xs {
        it.name = mk(7);
    }

    let mut strs: string[] = Array::<string>::with_len(3:uint);
    strs[0] = mk(0);
    strs[1] = mk(1);
    strs[2] = mk(2);
    for s in strs {
        s = mk(7);
    }

    print("loop-binding-witness");
    return 0;
}
`

func TestRuntimeV2LoopBindingOverwriteIsRefused(t *testing.T) {
	codes := compileRuntimeV2SourceForDiagnostics(t, runtimeV2LoopBindingOverwriteSource)
	// TWICE: once for the place spelling and once for the whole binding. A rule
	// that fired on one of the two would leave the other reaching the guards
	// this test used to exercise at run time.
	if got := codes[diag.SemaWriteToLoopBinding]; got != 2 {
		t.Fatalf(
			"expected both loop-binding stores to be refused as %s, got %d of them; all codes: %v",
			diag.SemaWriteToLoopBinding.ID(), got, codes,
		)
	}
}

// The guard, given its own witness because it is the half that can only fail
// LOUDLY: a place already given away must not be freed a second time by the
// store that reinitializes it. `@drop h.inner` then assigning over `h.inner` is
// the program an adversarial reviewer used to refuse the blind native-side
// release, and it is the reason the obligation is recorded in sema — where the
// moved-set can answer — rather than emitted unconditionally at the store.
const runtimeV2PlaceOverwriteAfterDropSource = `
type Item = { name: string, id: int };
type Holder = { inner: Item };

fn mk(n: int) -> string {
    return "displaced-value-long-enough-to-reach-the-heap-" + (n to string);
}

@entrypoint
fn main() -> int {
    let mut h: Holder = Holder { inner = Item { name = mk(0), id = 0 } };
    @drop h.inner;
    h.inner = Item { name = mk(1), id = 1 };
    if rt_string_len(&h.inner.name) > 0:uint {
        print("place-overwrite-after-drop-witness");
        return 0;
    }
    return 1;
}
`

func TestRuntimeV2PlaceOverwriteAfterDropIsNotADoubleFree(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2PlaceOverwriteAfterDropSource, nil)
	env := envWithStdlib(repoRoot(t))
	stdout, stderr, exitCode := runBinaryUnderValgrind(t, outputPath, env, 120*time.Second)
	if hasValgrindMemcheckError(stderr) {
		t.Fatalf("assigning over a dropped place freed it a second time\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if exitCode != 0 {
		t.Fatalf("place overwrite after drop failed (program exit=%d)\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "place-overwrite-after-drop-witness") {
		t.Fatalf("place overwrite after drop missing completion marker; stdout=%q", stdout)
	}
	bytesLost, blocksLost, err := parseValgrindDefinitelyLost(stderr)
	if err != nil {
		t.Fatalf("parse valgrind leak summary: %v\nstderr:\n%s", err, stderr)
	}
	if bytesLost != 0 || blocksLost != 0 {
		t.Fatalf(
			"the dropped-then-reassigned place leaked: got %d bytes in %d blocks definitely lost, want strict zero\nstderr:\n%s",
			bytesLost, blocksLost, stderr,
		)
	}
}
