package vm_test

import "testing"

// An arm that answers with its OWN payload binding hands that value to the
// compare, and the compare's reader is the one that decides when it dies.
//
// The arm owns the payload it bound — nobody else does, so it earns a release
// at the arm's end. That release is right until the arm ANSWERS with it: from
// then on the value belongs to whatever receives the compare's result, and
// freeing it where the arm ends frees it while the receiver is still reading.
// A CONSUMING receiver used to take the obligation back, which covered
// `let out = compare ...` and nothing else — a BORROWED result never consumes,
// so the arm's release stood and the borrower read freed memory.
//
// The rows are the four places a compare's value can go, and each reads the
// value AFTER the compare is over, which is the only way an early release shows
// up as anything but a leak. The counter-check is the leak column: an arm that
// stops freeing a value nothing else frees would trade this defect for the
// other one.
const runtimeV2ComparePayloadHandoffSource = `
tag Payload(string);
tag Empty();
type Slot = Payload(string) | Empty;

@copy type CopyCell = { left: int, right: int };
tag CopyPayload(CopyCell);
tag NoCopyPayload();
type CopySlot = CopyPayload(CopyCell) | NoCopyPayload;

tag Reading(float);
tag NoReading();
type Measure = Reading(float) | NoReading;

tag Count(int);
tag NoCount();
type Counter = Count(int) | NoCount;

// Built at runtime rather than written as a literal, so every payload is its
// own block and a missing release cannot hide behind a shared one.
fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn mk(i: int) -> Slot {
    if i > 100 {
        return Empty();
    }
    return Payload(build("v-"));
}

// The probe DEREFERENCES. One that only returns a constant reads every shape
// here as clean — that is how this defect stayed invisible for a session.
fn peek(x: &string) -> int {
    return len(x) to int;
}

// Borrowed: the result is handed to a callee that only looks at it, so nothing
// consumes it and the arm's own release is the one that used to fire.
fn borrowed(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        acc = acc + peek(compare mk(i) {
            Payload(s) => s;
            Empty() => "";
        });
        i = i + 1;
    }
    return acc;
}

// Consumed: the binding is the new owner and must be the only one.
fn consumed(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let out = compare mk(i) {
            Payload(s) => s;
            Empty() => "";
        };
        acc = acc + peek(&out);
        i = i + 1;
    }
    return acc;
}

// Discarded: nobody receives it, so the statement's own reclamation is all
// there is.
fn discarded(n: int) -> int {
    let mut i = 0;
    while i < n {
        compare mk(i) {
            Payload(s) => s;
            Empty() => "";
        };
        i = i + 1;
    }
    return 0;
}

// Returned: the value leaves the function that built it, and the caller reads
// it afterwards.
fn pick(i: int) -> string {
    return compare mk(i) {
        Payload(s) => s;
        Empty() => "";
    };
}

fn returned(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let out = pick(i);
        acc = acc + peek(&out);
        i = i + 1;
    }
    return acc;
}

// An arm that BUILDS from its payload instead of answering with it: the payload
// stays the arm's to free, and the built value is the compare's. Both must
// happen, exactly once each.
fn derived(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let out = compare mk(i) {
            Payload(s) => s + "!";
            Empty() => "";
        };
        acc = acc + peek(&out);
        i = i + 1;
    }
    return acc;
}

fn copy_payload_axis() -> int {
    let original = CopyCell { left = 1, right = 2 };
    let mut duplicate = original;
    duplicate.left = 9;
    if original.left != 1 || duplicate.left != 9 || duplicate.right != 2 {
        return 21;
    }
    // The direct scrutinee avoids an unrelated structural-to-nominal union
    // conversion; its wildcard arm still has to reclaim the copied payload.
    if compare CopyPayload(duplicate) {
        CopyPayload(_) => 1;
        _ => 0;
    } != 1 {
        return 22;
    }
    return 0;
}

fn float_payload_handoff(n: int) -> float {
    let mut i = 0;
    let mut total: float = 0.0;
    while i < n {
        let value = compare Reading(1.5 + 0.25) {
            Reading(v) => v;
            _ => 0.0;
        };
        total = total + value;
        i = i + 1;
    }
    return total;
}

fn fixnum_payload_handoff() -> int {
    return compare Count(7) {
        Count(v) => v;
        _ => 0 - 1;
    };
}

@entrypoint
fn main() -> int {
    if borrowed(16) != 96 {
        print("a borrowed compare result lost its payload");
        return 1;
    }
    if consumed(16) != 96 {
        print("a consumed compare result lost its payload");
        return 2;
    }
    if discarded(16) != 0 {
        print("a discarded compare went wrong");
        return 3;
    }
    if returned(16) != 96 {
        print("a returned compare result lost its payload");
        return 4;
    }
    if derived(16) != 112 {
        print("a derived compare result went wrong");
        return 5;
    }
    print("compare-payload-axis-move-only");

    let copy_code = copy_payload_axis();
    if copy_code != 0 {
        print("an @copy compare payload was not independent");
        return copy_code;
    }
    print("compare-payload-axis-copy-composite");

    if float_payload_handoff(16) != 28.0 {
        print("a reference-counted compare payload handoff went wrong");
        return 31;
    }
    print("compare-payload-axis-refcounted-scalar");

    if fixnum_payload_handoff() != 7 {
        print("a non-owning compare payload handoff went wrong");
        return 41;
    }
    print("compare-payload-axis-non-owning");
    return 0;
}
`

func TestRuntimeV2ComparePayloadOutlivesItsArm(t *testing.T) {
	ownershipGate(
		t,
		runtimeV2ComparePayloadHandoffSource,
		moveOnlyHeapMarker("compare-payload-axis-move-only"),
		copyValueCompositeMarker("compare-payload-axis-copy-composite"),
		referenceCountedScalarMarker("compare-payload-axis-refcounted-scalar"),
		nonOwningMarker("compare-payload-axis-non-owning"),
	)
}
