package vm_test

// Every branch below has an independent expected answer and failure code.
// The final marker is reached only after every caller and returned float has
// been used; the native heap census runs after those owners have all released.
const numericIteratorArraySource = `
fn numeric_values() -> float[] { return [1.5, 2.5, 3.5]; }
fn numeric_fixed() -> float[3] { return [1.5, 2.5, 3.5]; }

fn numeric_normal() -> float {
    let mut sum: float = 0.0;
    for x in numeric_values() { sum = sum + x; }
    return sum;
}

fn numeric_named_twice() -> float {
    let values = numeric_values();
    let mut sum: float = 0.0;
    for x in values { sum = sum + x; }
    for x in values { sum = sum + x; }
    if values[1] != 2.5 { return -1.0; }
    return sum;
}

fn numeric_borrowed(values: &float[]) -> float {
    let mut sum: float = 0.0;
    for x in values { sum = sum + x; }
    return sum;
}

fn numeric_generic_count<T>(values: &T[]) -> int {
    let mut count: int = 0;
    for item in values { count = count + 1; }
    return count;
}

fn numeric_fixed_forms() -> float {
    let values = numeric_fixed();
    let mut sum: float = 0.0;
    for x in values { sum = sum + x; }
    for x in numeric_fixed() { sum = sum + x; }
    if values[1] != 2.5 { return -1.0; }
    return sum;
}

fn numeric_break() -> float {
    let mut sum: float = 0.0;
    for x in numeric_values() { sum = sum + x; break; }
    return sum;
}

fn numeric_continue() -> float {
    let mut sum: float = 0.0;
    for x in numeric_values() {
        if x == 2.5 { continue; }
        sum = sum + x;
    }
    return sum;
}

fn numeric_expression_continue() -> float {
    let mut sum: float = 0.0;
    for x in numeric_values() {
        let ignored: int = { if x == 2.5 { continue; } ret 0; };
        sum = sum + x + (ignored to float);
    }
    return sum;
}

fn numeric_return_x() -> float {
    for x in numeric_values() { if x == 2.5 { return x; } }
    return -1.0;
}

fn numeric_nested_return() -> float {
    for x in numeric_values() {
        for y in numeric_values() { if y == 2.5 { return x + y; } }
    }
    return -1.0;
}

fn numeric_expression_return() -> float {
    for x in numeric_values() {
        let ignored: int = { if x == 2.5 { return x; } ret 0; };
        if ignored != 0 { return -2.0; }
    }
    return -1.0;
}

fn numeric_discard_and_empty() -> int {
    let mut count: int = 0;
    for _ in numeric_values() { count = count + 1; }
    let empty: float[] = [];
    for x in empty { count = count + 100; }
    return count;
}

@copy type NumericRecord = { n: int };
fn numeric_composite_control(values: &NumericRecord[]) -> int {
    let mut sum: int = 0;
    for item in values { sum = sum + item.n; }
    return sum;
}

@entrypoint
fn main() -> int {
    if numeric_normal() != 7.5 { return 1; }
    if numeric_named_twice() != 15.0 { return 2; }
    let caller = numeric_values();
    if numeric_borrowed(&caller) != 7.5 { return 3; }
    if caller[1] != 2.5 { return 4; }
    if numeric_generic_count<float>(&caller) != 3 { return 5; }
    if numeric_fixed_forms() != 15.0 { return 6; }
    if numeric_break() != 1.5 { return 7; }
    if numeric_continue() != 5.0 { return 8; }
    if numeric_expression_continue() != 5.0 { return 9; }
    if numeric_return_x() != 2.5 { return 10; }
    if numeric_nested_return() != 4.0 { return 11; }
    if numeric_expression_return() != 2.5 { return 12; }
    if numeric_discard_and_empty() != 3 { return 13; }
    let records: NumericRecord[] = [NumericRecord { n = 4 }, NumericRecord { n = 5 }];
    if numeric_composite_control(&records) != 9 { return 14; }
    if records[0].n != 4 { return 15; }
    print("numeric-iterator-arrays-witness");
    return 0;
}
`

// This is the source fast path that is supported before the int/uint counted
// transition. The attempt bound makes bypassing the latch fail with an answer,
// rather than relying on a timeout to detect the old continue rewrite gap.
const numericIteratorFastSource = `
fn numeric_fast_expression_continue() -> int {
    let mut attempts: int = 0;
    let mut sum: int = 0;
    for x in 0..5 {
        attempts = attempts + 1;
        if attempts > 8 { return -1; }
        let ignored: int = { if x == 1 { continue; } ret 0; };
        sum = sum + x + ignored;
    }
    if attempts != 5 { return -2; }
    return sum;
}

@entrypoint
fn main() -> int {
    let mut normal: int = 0;
    for x in 0..4 { normal = normal + x; }
    if normal != 6 { return 1; }
    let mut cont: int = 0;
    for x in 0..5 { if x == 1 { continue; } cont = cont + x; }
    if cont != 9 { return 2; }
    let mut stop: int = 0;
    for x in 0..5 { stop = stop + x; if x == 2 { break; } }
    if stop != 3 { return 3; }
    if numeric_fast_expression_continue() != 9 { return 4; }
    let mut unsigned: uint = 0:uint;
    for x: uint in 0:uint..4:uint { unsigned = unsigned + x; }
    if unsigned != 6:uint { return 5; }
    let mut fixed: int32 = 0:int32;
    for x: int32 in 0:int32..4:int32 { fixed = fixed + x; }
    if fixed != 6:int32 { return 6; }
    print("numeric-iterator-fast-witness");
    return 0;
}
`

// LLVM-only: the VM does not implement float bounds arithmetic. Stored,
// passed and temporary Range<float> values each finish after yielding values;
// the returned scalar is deliberately consumed by its caller as well.
const numericIteratorBoundsSource = `
fn numeric_make_range() -> Range<float> { return 1.5..4.5; }

fn numeric_walk_range(r: Range<float>) -> float {
    let mut sum: float = 0.0;
    for x in r { sum = sum + x; }
    return sum;
}

fn numeric_stored_range() -> float {
    let r = numeric_make_range();
    let mut sum: float = 0.0;
    for x in r { sum = sum + x; }
    return sum;
}

fn numeric_temporary_range() -> float {
    let mut sum: float = 0.0;
    for x in numeric_make_range() { sum = sum + x; }
    return sum;
}

fn numeric_return_bound() -> float {
    for x in 1.5..4.5 { if x == 2.5 { return x; } }
    return -1.0;
}

@entrypoint
fn main() -> int {
    if numeric_stored_range() != 7.5 { return 1; }
    if numeric_walk_range(numeric_make_range()) != 7.5 { return 2; }
    if numeric_temporary_range() != 7.5 { return 3; }
    if numeric_return_bound() != 2.5 { return 4; }
    let mut skipped: int = 0;
    for x in 2.5..2.5 { skipped = skipped + 1; }
    if skipped != 0 { return 5; }
    print("numeric-iterator-bounds-witness");
    return 0;
}
`
