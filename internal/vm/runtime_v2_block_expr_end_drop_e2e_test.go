package vm_test

import "testing"

// Normal block results leave before their locals are reclaimed. The surviving
// string, partial field, float and outer borrow expose a release too many;
// Valgrind exposes a missing release that VM frame shutdown can conceal.
const runtimeV2BlockExprEndDropSource = `
type Pair = { left: string, right: string };
@copy type Cell = { number: int };
type Reading = { value: float, text: string };

fn build(prefix: string) -> string {
    let mut text = prefix;
    let mut i = 0;
    while i < 4 {
        text = text + "x";
        i = i + 1;
    }
    return text;
}
fn peek(text: &string) -> int { return len(text) to int; }

fn implicit_exit() -> int {
    return compare true {
        true => {
            let inner = build("heap-");
            let owned = inner;
            peek(&owned);
        };
        false => 0;
    };
}
fn legacy_exit() -> int {
    let result = {
        let inner = build("heap-");
        let owned = inner;
        peek(&owned);
    };
    return result;
}
fn explicit_exit() -> int {
    let result = {
        let inner = build("heap-");
        let owned = inner;
        ret peek(&owned);
    };
    return result;
}
fn returned_owner() -> int {
    let result = {
        let owned = build("heap-");
        ret owned;
    };
    return peek(&result);
}
fn partial_owner() -> int {
    let result = {
        let holder = Pair { left = build("heap-"), right = build("stays-") };
        let taken = own holder.left;
        ret taken;
    };
    return peek(&result);
}
// Past the fixnum range, so each value is a counted heap integer.
fn heap(extra: int) -> int { return 4611686018427387904 * 4 + extra; }
fn copied_owner() -> bool {
    let original = Cell { number = heap(3) };
    let observed = {
        let mut copied = original;
        copied.number = heap(5);
        ret copied.number - heap(0);
    };
    // The copied owner has already left its scope when the original is read.
    return observed == 5 && original.number - heap(0) == 3;
}
fn fresh_float(input: float) -> float {
    let result = {
        let doomed = build("heap-");
        ret input + 0.25;
    };
    return result;
}
fn field_float(input: float) -> float {
    let result = {
        let holder = Reading { value = input + 0.5, text = build("heap-") };
        ret holder.value;
    };
    return result;
}
fn borrowed_outer() -> int {
    let owner = build("heap-");
    let borrowed: &string = {
        let doomed = build("doomed-");
        ret &owner;
    };
    let seen = peek(borrowed);
    let fixnum = { let doomed = build("fixnum-"); ret 7; };
    return seen + peek(&owner) + fixnum;
}

@entrypoint
fn main() -> int {
    let mut i = 0;
    while i < 16 {
        if implicit_exit() != 9 { return 1; }
        if legacy_exit() != 9 { return 2; }
        if explicit_exit() != 9 { return 3; }
        if returned_owner() != 9 { return 4; }
        if partial_owner() != 9 { return 5; }
        if !copied_owner() { return 6; }
        // Each result is read after its producer's locals have left scope.
        if fresh_float(1.0) != 1.25 { return 7; }
        if field_float(2.0) != 2.5 { return 8; }
        if borrowed_outer() != 25 { return 9; }
        i = i + 1;
    }
    if i != 16 { return 10; }
    print("block-normal-exit-axis-move-only");
    print("block-normal-exit-axis-copy-composite");
    print("block-normal-exit-axis-refcounted-scalar");
    print("block-normal-exit-axis-non-owning");
    return 0;
}
`

func TestRuntimeV2BlockExprNormalExitReclaimsItsLocals(t *testing.T) {
	ownershipGate(t, runtimeV2BlockExprEndDropSource,
		moveOnlyHeapMarker("block-normal-exit-axis-move-only"),
		copyValueCompositeMarker("block-normal-exit-axis-copy-composite"),
		referenceCountedScalarMarker("block-normal-exit-axis-refcounted-scalar"),
		nonOwningMarker("block-normal-exit-axis-non-owning"))
}
