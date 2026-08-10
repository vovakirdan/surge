package vm_test

import "testing"

// Reading a value THROUGH a borrow must leave the owner's value alone, and
// copying one OUT of a container must leave the copy with an owner.
//
// `float` is where the two questions meet. It is a Copy type at the surface and
// a reference-counted block underneath, so a read through a reference copies a
// handle the owner knows nothing about. The temporary that receives it is
// registered for a release the moment it exists, so without a reference of its
// own it gives the OWNER's away: a callee that merely COMPARED its `&float`
// parameter freed the caller's value. The other direction costs the opposite —
// a copy that takes a reference while the binding holding it is treated as an
// alias never releases it, which is a leak per evaluation.
//
// Every row therefore reads the source AFTER the borrow is done with it, and
// the leak total is asserted at strict zero, because these two failures show up
// in different columns of the same report.
const runtimeV2BorrowedScalarSource = `
type Box = { value: float };
@copy type CopyCell = { left: int, right: int };

fn build_text(prefix: string) -> string {
    let mut text = prefix;
    let mut i = 0;
    while i < 4 {
        text = text + "x";
        i = i + 1;
    }
    return text;
}

fn reads_text(v: &string) -> int {
    return len(v) to int;
}

fn borrows_runtime_text() -> int {
    let text = build_text("v-");
    let mut i = 0;
    let mut total = 0;
    while i < 8 {
        total = total + reads_text(&text);
        i = i + 1;
    }
    if total != 48 {
        return 11;
    }
    if len(text) != 6 {
        return 12;
    }
    return 0;
}

fn copies_cell(v: &CopyCell) -> CopyCell {
    return *v;
}

fn borrows_copy_composite() -> int {
    let original = CopyCell { left = 1, right = 2 };
    let mut duplicate = copies_cell(&original);
    duplicate.left = 9;
    if original.left != 1 || duplicate.left != 9 || duplicate.right != 2 {
        return 21;
    }
    return 0;
}

fn reads_fixnum(v: &int) -> int {
    return *v;
}

fn borrows_non_owning() -> int {
    let original: int = 7;
    let duplicate = reads_fixnum(&original);
    if original != 7 || duplicate != 7 {
        return 31;
    }
    return 0;
}

// The whole defect, in the smallest shape that has it: a callee that only
// LOOKS at its borrowed argument.
fn compares(v: &float) -> int {
    if v > 0.0 {
        return 1;
    }
    return 0;
}

fn computes(v: &float) -> float {
    return v + 1.0;
}

fn renders(v: &float) -> string {
    return v to string;
}

// A borrow handed on to another borrower: the second read must not release
// what the first one did not own either.
fn forwards(v: &float) -> float {
    return derefs(v);
}

fn derefs(v: &float) -> float {
    return *v;
}

// The deref feeds a by-value parameter, which is the third way the value can
// leave a borrow.
fn by_value(v: float) -> float {
    return v;
}

fn passes(v: &float) -> float {
    return by_value(*v);
}

// The deref is BOUND. The binding is a real owner — it took a reference of its
// own — so it has to release it, and a binding treated as an alias here is the
// leak half of the same contract.
fn binds(v: &float) -> int {
    let c = *v;
    if c > 1.0 {
        return 1;
    }
    return 0;
}

fn binds_and_returns(v: &float) -> float {
    let c = *v;
    return c;
}

// A field read is the same copy without any borrow in sight, and it is where
// the leak was measured first.
fn reads_a_field(n: int) -> int {
    let mut i = 0;
    let mut hits = 0;
    while i < n {
        let b = Box { value = 2.5 };
        let f = b.value;
        if f == 2.5 {
            hits = hits + 1;
        }
        i = i + 1;
    }
    return hits;
}

// Repetition, because a reference lost once is a use-after-free while a spare
// one is only loud in a loop.
fn borrows_repeatedly(v: &float, n: int) -> int {
    let mut i = 0;
    let mut hits = 0;
    while i < n {
        hits = hits + compares(v);
        i = i + 1;
    }
    return hits;
}

@entrypoint
fn main() -> int {
    let move_only_code = borrows_runtime_text();
    if move_only_code != 0 {
        print("a borrowed runtime-built string did not survive its readers");
        return move_only_code;
    }
    print("borrowed-scalar-axis-move-only");

    let copy_code = borrows_copy_composite();
    if copy_code != 0 {
        print("a borrowed @copy composite did not produce an independent duplicate");
        return copy_code;
    }
    print("borrowed-scalar-axis-copy-composite");

    let a: float = 5.5;
    if compares(&a) != 1 {
        print("comparing a borrowed value went wrong");
        return 1;
    }
    if computes(&a) != 6.5 {
        print("computing with a borrowed value went wrong");
        return 2;
    }
    let rendered = renders(&a);
    if len(rendered) == 0 {
        print("rendering a borrowed value went wrong");
        return 3;
    }
    if forwards(&a) != 5.5 {
        print("forwarding a borrow went wrong");
        return 4;
    }
    if passes(&a) != 5.5 {
        print("passing a borrowed value by value went wrong");
        return 5;
    }
    if binds(&a) != 1 {
        print("binding a borrowed value went wrong");
        return 6;
    }
    if binds_and_returns(&a) != 5.5 {
        print("returning a bound borrow went wrong");
        return 7;
    }
    if reads_a_field(8) != 8 {
        print("reading a field went wrong");
        return 8;
    }
    if borrows_repeatedly(&a, 8) != 8 {
        print("repeated borrowing went wrong");
        return 9;
    }
    // The owner is read LAST, and its value is the proof: every row above ran
    // without taking the block out from under it.
    if a != 5.5 {
        print("the owner's value did not survive its borrows");
        return 10;
    }
    print("borrowed-scalar-axis-refcounted-scalar");

    let non_owning_code = borrows_non_owning();
    if non_owning_code != 0 {
        print("a borrowed non-owning fixnum went wrong");
        return non_owning_code;
    }
    print("borrowed-scalar-axis-non-owning");
    return 0;
}
`

func TestRuntimeV2BorrowedScalarSurvivesItsReaders(t *testing.T) {
	ownershipGate(
		t,
		runtimeV2BorrowedScalarSource,
		moveOnlyHeapMarker("borrowed-scalar-axis-move-only"),
		copyValueCompositeMarker("borrowed-scalar-axis-copy-composite"),
		referenceCountedScalarMarker("borrowed-scalar-axis-refcounted-scalar"),
		nonOwningMarker("borrowed-scalar-axis-non-owning"),
	)
}
