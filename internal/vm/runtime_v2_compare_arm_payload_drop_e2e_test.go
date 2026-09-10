package vm_test

import "testing"

// A compare arm's payload binding is freed ONCE, by ONE emitter.
//
// Two mechanisms used to answer for the same binding. Sema opens a drop scope
// around an arm's pattern and gives the binding the ordinary scope-exit
// obligation every binding has; normalization, written earlier and on the
// premise that a pattern binding "carries no scope-exit obligation", minted a
// second one for the reference-counted types alone. Both rode the same implicit
// `ret` as DropsAfterValue, so a `float` or a `Channel<T>` payload was released
// twice on every evaluation.
//
// Neither backend told the truth about it on its own, which is why the rows
// below are deliberately split across both. The VM's checker sees the second
// release for what it is and panics `use-after-free: local "v" used after drop`
// even for an arm that never mentions the binding — the "use" IS the second
// drop. The native lane stores null after every release, so the second one
// receives null and returns: accidentally defused, never correct.
//
// The counter-check is the hand-out row, and it is the reason this file cannot
// simply delete one emitter and stop. Sema WITHDRAWS the obligation from an arm
// whose result is the binding itself, on the premise that the value moved
// onward. That premise is false for a reference-counted value: those are Copy,
// so reading one RETAINS and the binding's own reference is still outstanding
// when the compare's result is handed to its receiver. Removing the second
// emitter without teaching the withdrawal that distinction trades this double
// free for a leak of one counted block per evaluation of `Some(x) => x` — a
// clean-looking VM answer with a wrong census, the mirror of the failure this
// area recorded the first time it was worked.
//
// Every row therefore pins the ANSWER as well as the census. That is not
// belt-and-braces here: an earlier attempt at this same binding released it at
// the end of its own `let` statement, ran valgrind-clean, and printed the wrong
// result. A leak census alone cannot tell that failure from a success.
const runtimeV2CompareArmPayloadDropSource = `
tag Reading(float);
tag NoReading();
type Measure = Reading(float) | NoReading;

tag Label(string);
tag NoLabel();
type Tagged = Label(string) | NoLabel;

@copy type Cell = { left: int, right: int };
tag Boxed(Cell);
tag NoBox();
type Crate = Boxed(Cell) | NoBox;

tag Count(int);
tag NoCount();
type Tally = Count(int) | NoCount;

tag Port(Channel<int>);
tag NoPort();
type Wire = Port(Channel<int>) | NoPort;

// A borrow probe must DEREFERENCE, or every shape here reads clean.
fn peek(x: &string) -> int {
    return len(x) to int;
}

// The arm never mentions its binding. What the VM reports as a use-after-free
// is the second release itself, so an arm that ignores its payload is the
// SHARPEST row rather than a degenerate one.
fn ignored_counted_payload(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let m: Measure = Reading(1.5 + 0.25);
        let out = compare m {
            Reading(v) => "seen";
            NoReading() => "";
        };
        acc = acc + peek(&out);
        i = i + 1;
    }
    return acc;
}

// The arm READS its binding and answers with something else, so the payload
// stays the arm's and the built value is the compare's. Both must happen, once
// each.
fn derived_counted_payload(n: int) -> float {
    let mut i = 0;
    let mut total: float = 0.0;
    while i < n {
        let m: Measure = Reading(1.5);
        let derived = compare m {
            Reading(v) => v + 0.5;
            NoReading() => 0.0;
        };
        total = total + derived;
        i = i + 1;
    }
    return total;
}

// The arm ANSWERS with its binding. The read retains, so two references exist
// and two releases are owed -- the arm's and the receiver's. This is the row a
// naive repair leaks.
fn handed_out_counted_payload(n: int) -> float {
    let mut i = 0;
    let mut total: float = 0.0;
    while i < n {
        let m: Measure = Reading(1.5 + 0.25);
        let value = compare m {
            Reading(v) => v;
            NoReading() => 0.0;
        };
        total = total + value;
        i = i + 1;
    }
    return total;
}

fn look(m: &Measure) -> float {
    return compare *m {
        Reading(v) => v + 1.0;
        NoReading() => 0.0;
    };
}

// The scrutinee is BORROWED and the same union is read n times, so an arm that
// releases once too often is not merely leak-neutral: it frees the CALLER's
// payload while the caller still holds it. The read after the loop is what
// makes that visible.
fn borrowed_counted_payload(n: int) -> float {
    let held: Measure = Reading(2.5);
    let mut i = 0;
    let mut total: float = 0.0;
    while i < n {
        total = total + look(&held);
        i = i + 1;
    }
    if look(&held) != 3.5 {
        return 0.0;
    }
    return total;
}

// The other reference-counted shape: a channel HANDLE, which the same predicate
// selects and which leaks or double-frees by the same argument.
fn channel_payload_ignored(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let ch = Channel::<int>::new(4:uint);
        let w: Wire = Port(ch);
        acc = acc + compare w {
            Port(p) => 1;
            NoPort() => 0;
        };
        i = i + 1;
    }
    return acc;
}

// Handed out and then USED, so a handle freed one release early is a round trip
// through dead storage rather than a quiet leak.
fn channel_payload_handed_out(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let ch = Channel::<int>::new(4:uint);
        let w: Wire = Port(ch);
        let taken = compare w {
            Port(p) => p;
            NoPort() => Channel::<int>::new(1:uint);
        };
        taken.try_send(7);
        let got: Option<int> = taken.try_recv();
        acc = acc + compare got { Some(v) => v; nothing => 0; };
        i = i + 1;
    }
    return acc;
}

// Built at runtime rather than written as a literal, so every payload is its
// own block and a missing release cannot hide behind a shared one.
fn move_only_payload(n: int) -> int {
    let mut i = 0;
    let mut acc = 0;
    while i < n {
        let mut built = "m";
        built = built + "ove";
        let t: Tagged = Label(built);
        let out = compare t {
            Label(s) => s + "!";
            NoLabel() => "";
        };
        acc = acc + peek(&out);
        i = i + 1;
    }
    return acc;
}

fn copy_composite_payload() -> int {
    let original = Cell { left = 1, right = 2 };
    let mut duplicate = original;
    duplicate.left = 9;
    if original.left != 1 || duplicate.left != 9 || duplicate.right != 2 {
        return 0;
    }
    let crate: Crate = Boxed(duplicate);
    return compare crate {
        Boxed(c) => 1;
        NoBox() => 0;
    };
}

fn non_owning_payload() -> int {
    let tally: Tally = Count(7);
    return compare tally {
        Count(v) => v;
        NoCount() => 0 - 1;
    };
}

@entrypoint
fn main() -> int {
    if ignored_counted_payload(16) != 64 {
        print("an ignored counted payload went wrong");
        return 1;
    }
    if derived_counted_payload(16) != 32.0 {
        print("a derived counted payload went wrong");
        return 2;
    }
    if handed_out_counted_payload(16) != 28.0 {
        print("a handed-out counted payload went wrong");
        return 3;
    }
    if borrowed_counted_payload(16) != 56.0 {
        print("a borrowed counted payload went wrong");
        return 4;
    }
    if channel_payload_ignored(16) != 16 {
        print("an ignored channel payload went wrong");
        return 5;
    }
    if channel_payload_handed_out(16) != 112 {
        print("a handed-out channel payload went wrong");
        return 6;
    }
    print("arm-payload-axis-refcounted");

    if move_only_payload(16) != 80 {
        print("a move-only payload went wrong");
        return 7;
    }
    print("arm-payload-axis-move-only");

    if copy_composite_payload() != 1 {
        print("a copy composite payload went wrong");
        return 8;
    }
    print("arm-payload-axis-copy-composite");

    if non_owning_payload() != 7 {
        print("a non-owning payload went wrong");
        return 9;
    }
    print("arm-payload-axis-non-owning");
    return 0;
}
`

func TestRuntimeV2ComparePayloadIsFreedOnceByItsArm(t *testing.T) {
	ownershipGate(
		t,
		runtimeV2CompareArmPayloadDropSource,
		moveOnlyHeapMarker("arm-payload-axis-move-only"),
		copyValueCompositeMarker("arm-payload-axis-copy-composite"),
		referenceCountedScalarMarker("arm-payload-axis-refcounted"),
		nonOwningMarker("arm-payload-axis-non-owning"),
	)
}
