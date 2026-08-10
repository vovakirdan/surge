package vm_test

import "testing"

// A cast from a type to ITSELF hands the source value straight back, so the
// result is a second name for storage the source's owner still holds. Nothing
// may release it on the result's behalf.
//
// `float` is the shape that shows it: its heap block is reference-counted, and
// every temporary holding one is registered for a release when its region ends.
// A cast that produced a temporary but filled it with an alias therefore took a
// reference it never had — the owner's block was freed while the binding still
// pointed at it, and the next read of that binding read freed memory.
//
// The controls matter as much as the rows: a cast that really does convert
// (`int` to `float`) allocates and must keep its release, and the same code
// without a cast pins what the identity rows should cost. All three read their
// values AFTER the cast, which is the only way an over-release shows up as
// anything but a leak.
const runtimeV2IdentityCastSource = `
@copy type CopyCell = { left: int, right: int };

// This axis must allocate at runtime: a literal alone can hide a missing move
// or release behind shared/static storage.
fn build_text(prefix: string) -> string {
    let mut text = prefix;
    let mut i = 0;
    while i < 4 {
        text = text + "x";
        i = i + 1;
    }
    return text;
}

fn move_only_axis() -> int {
    let built = build_text("id-");
    let text = built;
    if len(text) != 7 {
        return 11;
    }
    return 0;
}

// The binding must receive a real independent @copy duplicate, not a second
// name for the original composite.
fn copy_composite_axis() -> int {
    let original = CopyCell { left = 1, right = 2 };
    let mut duplicate = original;
    duplicate.left = 9;
    if original.left != 1 || duplicate.left != 9 || duplicate.right != 2 {
        return 21;
    }
    return 0;
}

fn non_owning_identity() -> int {
    let original: int = 7;
    let duplicate = original to int;
    if original + duplicate != 14 {
        return 31;
    }
    return 0;
}

fn take(v: float) -> float {
    return v + 1.0;
}

// Discarded: nothing consumes the cast, so the statement's own reclamation is
// what decides whether the sources survive it.
fn discarded(flag: bool) -> int {
    let x: float = 1.5;
    let y: float = 2.5;
    flag ? (x to float) : (y to float);
    if x + y != 4.0 {
        return 1;
    }
    return 0;
}

// Consumed by a binding: the binding takes its own reference, and the source
// keeps the one it had.
fn consumed() -> int {
    let a: float = 3.5;
    let b = a to float;
    if a + b != 7.0 {
        return 2;
    }
    return 0;
}

// Handed to a callee, which is the third place a value's ownership can be
// decided.
fn as_argument() -> int {
    let a: float = 4.5;
    let r = take(a to float);
    if a + r != 10.0 {
        return 3;
    }
    return 0;
}

// A cast that CHANGES representation: it builds a value nothing else holds, and
// its release is the only one that value gets.
fn widening() -> int {
    let n: int = 7;
    let f = n to float;
    if f != 7.0 {
        return 4;
    }
    return 0;
}

// The same shape with no cast at all — the cost the identity rows must match.
fn no_cast() -> int {
    let a: float = 5.5;
    let b = a;
    if a + b != 11.0 {
        return 5;
    }
    return 0;
}

// The operand is a LITERAL, which allocates a block of its own. Here the cast
// hands on a value that really is fresh, so somebody must still release it —
// the difference is that the release belongs to the literal underneath, not to
// the cast. Consumed, discarded and forwarded through a branch, because those
// are the three places that answer "who releases it" differently.
fn literal_consumed() -> int {
    let x = 1.5 to float;
    if x != 1.5 {
        return 7;
    }
    return 0;
}

fn literal_discarded() -> int {
    2.5 to float;
    return 0;
}

fn literal_through_branch(flag: bool) -> int {
    let x = flag ? (1.5 to float) : (2.5 to float);
    if x != 1.5 {
        return 8;
    }
    return 0;
}

// A loop, because a lost reference is a use-after-free while a spare one is a
// leak, and only repetition makes the second one loud.
fn repeated(n: int) -> int {
    let mut i = 0;
    let mut acc: float = 0.0;
    while i < n {
        let a: float = 2.0;
        let b = a to float;
        acc = acc + b;
        i = i + 1;
    }
    if acc != 32.0 {
        return 6;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let move_only_code = move_only_axis();
    if move_only_code != 0 {
        print("a move-only runtime-built string move went wrong");
        return move_only_code;
    }
    print("identity-cast-axis-move-only");

    let copy_code = copy_composite_axis();
    if copy_code != 0 {
        print("an @copy duplicate was not independent");
        return copy_code;
    }
    print("identity-cast-axis-copy-composite");

    let discarded_code = discarded(true);
    if discarded_code != 0 {
        print("discarded identity cast released its source");
        return discarded_code;
    }
    let consumed_code = consumed();
    if consumed_code != 0 {
        print("consumed identity cast released its source");
        return consumed_code;
    }
    let argument_code = as_argument();
    if argument_code != 0 {
        print("identity cast as an argument released its source");
        return argument_code;
    }
    let widening_code = widening();
    if widening_code != 0 {
        print("widening cast lost its value");
        return widening_code;
    }
    let plain_code = no_cast();
    if plain_code != 0 {
        print("the uncast control computed the wrong value");
        return plain_code;
    }
    let literal_code = literal_consumed();
    if literal_code != 0 {
        print("a bound literal identity cast lost its value");
        return literal_code;
    }
    let literal_discarded_code = literal_discarded();
    if literal_discarded_code != 0 {
        print("a discarded literal identity cast computed the wrong value");
        return literal_discarded_code;
    }
    let literal_branch_code = literal_through_branch(true);
    if literal_branch_code != 0 {
        print("a literal identity cast through a branch lost its value");
        return literal_branch_code;
    }
    let repeated_code = repeated(16);
    if repeated_code != 0 {
        print("repeated identity cast computed the wrong value");
        return repeated_code;
    }
    print("identity-cast-axis-refcounted-scalar");

    let non_owning_code = non_owning_identity();
    if non_owning_code != 0 {
        print("a non-owning identity cast computed the wrong value");
        return non_owning_code;
    }
    print("identity-cast-axis-non-owning");
    return 0;
}
`

func TestRuntimeV2IdentityCastKeepsItsSourceOwned(t *testing.T) {
	ownershipGate(
		t,
		runtimeV2IdentityCastSource,
		moveOnlyHeapMarker("identity-cast-axis-move-only"),
		copyValueCompositeMarker("identity-cast-axis-copy-composite"),
		referenceCountedScalarMarker("identity-cast-axis-refcounted-scalar"),
		nonOwningMarker("identity-cast-axis-non-owning"),
	)
}
