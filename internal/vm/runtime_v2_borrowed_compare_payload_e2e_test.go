//go:build !golden

package vm_test

import "testing"

// A compare that only READS its union owns nothing it takes out of it.
//
// `compare *arg { ... }` is the ordinary way to look at a borrowed union — it
// is how `core/format.sg` reads a `&FmtArg`, and how `stdlib/json` walks a
// borrowed value. The deref strips the reference, so the subject's TYPE is a
// bare union and a payload binding read out of it looked like a value the arm
// owned. It is not: the union belongs to whoever the borrow points at, so
// releasing the payload at the arm's end frees the caller's storage. Every
// formatted print with a string argument did exactly that.
//
// The opposite rows are what make this a contract rather than a patch, because
// each one wants the obligation KEPT:
//
//   - a `@copy` union read through the same deref is CLONED into the compare,
//     so the payload really is the arm's;
//   - a reference-counted scalar payload takes a reference of its own even from
//     a borrowed union, so the arm gives it back;
//   - an owned subject transfers its payload into the arm as it always did.
//
// The caller reads its value AFTER every borrow, which is the only way a
// release too many shows up as anything but a leak, and the leak column is
// asserted at strict zero, because that is the direction this fix could trade
// into.
const runtimeV2BorrowedComparePayloadSource = `
tag Payload(string);
tag Empty();
type Slot = Payload(string) | Empty;

@copy type Cell = { a: int, b: int };
tag Held(Cell);
tag Absent();
@copy type Holder = Held(Cell) | Absent;

tag Reading(float);
tag NoReading();
type Measure = Reading(float) | NoReading;

tag Count(int);
tag NoCount();
type Counter = Count(int) | NoCount;

fn build(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn peek(x: &string) -> int {
    return len(x) to int;
}

// The defect itself: an arm that only LOOKS at a payload of a borrowed union.
fn reads_payload(slot: &Slot) -> int {
    return compare *slot {
        Payload(s) => peek(&s);
        Empty() => 0;
    };
}

// The same shape as core/format.sg's append_fmt_arg: the payload feeds a concat
// that builds a new string, and the union keeps its own.
fn appends_payload(out: string, slot: &Slot) -> string {
    return compare *slot {
        Payload(s) => out + s;
        Empty() => out;
    };
}

// A @copy union through the same deref: the read CLONES, so this arm owns its
// payload and must still release it. Kept in the same file as the row above
// because the two answers are opposite and one predicate decides both.
fn reads_copy_payload(h: &Holder) -> int {
    return compare *h {
        Held(c) => c.a;
        _ => 0 - 1;
    };
}

// A reference-counted scalar payload of a BORROWED union: the extraction
// retains, so the arm holds a reference of its own and gives it back.
fn reads_float_payload(m: &Measure) -> float {
    return compare *m {
        Reading(v) => v;
        _ => 0.0;
    };
}

fn reads_fixnum_payload(c: &Counter) -> int {
    return compare *c {
        Count(v) => v;
        _ => 0 - 1;
    };
}

// A compare over something that is NOT a union at all: the subject moves into
// the pattern's binding, which is then its only owner. The borrowed-union rule
// must not reach this — a review caught exactly that, one leaked string per
// evaluation.
fn binds_a_plain_value() -> int {
    let text = build("v-");
    return compare text {
        s => peek(&s);
    };
}

// The control: an OWNED subject, where the payload transfers into the arm.
fn owns_its_subject(i: int) -> int {
    let slot: Slot = Payload(build("v-"));
    return compare slot {
        Payload(s) => peek(&s) + i;
        Empty() => 0;
    };
}

@entrypoint
fn main() -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < 16 {
        let slot: Slot = Payload(build("v-"));
        // 6, then 10 for "out:" + the payload, then the OWNER read after both
        // borrows — still six characters long, which is the whole assertion.
        acc = acc + reads_payload(&slot);
        let rendered = appends_payload("out:", &slot);
        acc = acc + peek(&rendered);
        acc = acc + compare slot {
            Payload(s) => peek(&s);
            Empty() => 0;
        };
        i = i + 1;
    }
    if acc != 352 {
        print("a borrowed payload did not survive its readers");
        print(acc to string);
        return 1;
    }

    if owns_its_subject(0) != 6 {
        print("an owned subject went wrong");
        return 4;
    }

    let mut plain = 0;
    i = 0;
    while i < 16 {
        plain = plain + binds_a_plain_value();
        i = i + 1;
    }
    if plain != 96 {
        print("a plain bound subject went wrong");
        return 5;
    }
    print("borrowed-compare-payload-axis-move-only");

    let held: Holder = Held(Cell { a = 1, b = 2 });
    let mut copies = 0;
    i = 0;
    while i < 16 {
        copies = copies + reads_copy_payload(&held);
        i = i + 1;
    }
    let original = Cell { a = 1, b = 2 };
    let mut duplicate = original;
    duplicate.a = 9;
    if copies != 16 || original.a != 1 || duplicate.a != 9 || duplicate.b != 2 {
        print("a cloned payload was not independent");
        return 2;
    }
    print("borrowed-compare-payload-axis-copy-composite");

    let measure: Measure = Reading(1.5 + 0.25);
    let mut total: float = 0.0;
    i = 0;
    while i < 16 {
        total = total + reads_float_payload(&measure);
        i = i + 1;
    }
    if total != 28.0 {
        print("a borrowed float payload went wrong");
        return 3;
    }
    print("borrowed-compare-payload-axis-refcounted-scalar");

    let counter: Counter = Count(7);
    let mut counts = 0;
    i = 0;
    while i < 16 {
        counts = counts + reads_fixnum_payload(&counter);
        i = i + 1;
    }
    if counts != 112 || reads_fixnum_payload(&counter) != 7 {
        print("a borrowed fixnum payload went wrong");
        return 6;
    }
    print("borrowed-compare-payload-axis-non-owning");
    return 0;
}
`

func TestRuntimeV2BorrowedComparePayloadStaysTheOwners(t *testing.T) {
	ownershipGate(
		t,
		runtimeV2BorrowedComparePayloadSource,
		moveOnlyHeapMarker("borrowed-compare-payload-axis-move-only"),
		copyValueCompositeMarker("borrowed-compare-payload-axis-copy-composite"),
		referenceCountedScalarMarker("borrowed-compare-payload-axis-refcounted-scalar"),
		nonOwningMarker("borrowed-compare-payload-axis-non-owning"),
	)
}
