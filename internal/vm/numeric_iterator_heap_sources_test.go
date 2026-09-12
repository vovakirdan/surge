package vm_test

import "strings"

// A..D fit uint64 but exceed both counted kinds' inline maxima. Construct
// through uint64 so this proof asks about loop ownership, not literal parsing.
// Scores are independently 0+1+2=3, twice that=6, or 0+2=2 after skipping B.
const numericIteratorHeapPrelude = `
fn heap_a() -> $K { return (9223372036854775811:uint64):$K; }
fn heap_b() -> $K { return (9223372036854775812:uint64):$K; }
fn heap_c() -> $K { return (9223372036854775813:uint64):$K; }
fn heap_d() -> $K { return (9223372036854775814:uint64):$K; }
fn heap_offset(value: $K) -> int64 {
    return ((value:uint64) - (9223372036854775811:uint64)):int64;
}
`

func numericIteratorHeapSource(kind, form, body string) string {
	if kind != "int" && kind != "uint" {
		panic("heap iterator source requires int or uint")
	}
	return strings.NewReplacer("$K", kind, "$MARKER", numericIteratorHeapMarker(kind, form)).Replace(numericIteratorHeapPrelude + body)
}

func numericIteratorHeapMarker(kind, form string) string {
	return "numeric-iterator-heap-" + kind + "-" + form + "-witness"
}

func numericIteratorHeapArraySource(kind string) string {
	return numericIteratorHeapSource(kind, "arrays", numericIteratorHeapArrays)
}

// Returning helpers finish before main checks their owners. The named caller
// is read after each borrowing/generic use; the marker precedes final teardown,
// while the native physical heap check follows it.
const numericIteratorHeapArrays = `
fn heap_values() -> $K[] { return [heap_a(), heap_b(), heap_c()]; }
fn heap_fixed() -> $K[3] { return [heap_a(), heap_b(), heap_c()]; }

fn heap_temporary() -> int64 {
    let mut sum: int64 = 0:int64;
    for x in heap_values() { sum = sum + heap_offset(x); }
    return sum;
}

fn heap_borrowed(values: &$K[]) -> int64 {
    let mut sum: int64 = 0:int64;
    for x in values { sum = sum + heap_offset(x); }
    return sum;
}

fn heap_generic_count<T>(values: &T[]) -> int64 {
    let mut count: int64 = 0:int64;
    for item in values { count = count + 1:int64; }
    return count;
}

fn heap_fixed_forms() -> int64 {
    let values = heap_fixed();
    let mut sum: int64 = 0:int64;
    for x in values { sum = sum + heap_offset(x); }
    for x in heap_fixed() { sum = sum + heap_offset(x); }
    if (values[1]:uint64) != 9223372036854775812:uint64 { return -1:int64; }
    return sum;
}

fn heap_break() -> $K {
    let mut found: $K = 0:$K;
    for x in heap_values() { found = x; break; }
    return found;
}

fn heap_continue() -> int64 {
    let mut sum: int64 = 0:int64;
    for x in heap_values() {
        if (x:uint64) == 9223372036854775812:uint64 { continue; }
        sum = sum + heap_offset(x);
    }
    return sum;
}

fn heap_return_x() -> $K {
    for x in heap_values() {
        if (x:uint64) == 9223372036854775812:uint64 { return x; }
    }
    return 0:$K;
}

fn heap_nested_expression_return() -> $K {
    for x in heap_values() {
        for y in heap_values() {
            let ignored: int64 = {
                if (y:uint64) == 9223372036854775812:uint64 { return x; }
                ret 0:int64;
            };
            if ignored != 0:int64 { return 0:$K; }
        }
    }
    return 0:$K;
}

@entrypoint
fn main() -> int {
    if heap_temporary() != 3:int64 { return 1; }
    let caller = heap_values();
    let mut twice: int64 = 0:int64;
    for x in caller { twice = twice + heap_offset(x); }
    for x in caller { twice = twice + heap_offset(x); }
    if twice != 6:int64 { return 2; }
    if (caller[1]:uint64) != 9223372036854775812:uint64 { return 3; }
    if heap_borrowed(&caller) != 3:int64 { return 4; }
    if (caller[1]:uint64) != 9223372036854775812:uint64 { return 4; }
    if heap_generic_count(&caller) != 3:int64 { return 5; }
    if (caller[1]:uint64) != 9223372036854775812:uint64 { return 5; }
    if heap_fixed_forms() != 6:int64 { return 6; }
    let broken = heap_break();
    if (broken:uint64) != 9223372036854775811:uint64 { return 7; }
    if heap_continue() != 2:int64 { return 8; }
    let returned = heap_return_x();
    if (returned:uint64) != 9223372036854775812:uint64 { return 9; }
    let nested = heap_nested_expression_return();
    if (nested:uint64) != 9223372036854775811:uint64 { return 10; }
    if (caller[1]:uint64) != 9223372036854775812:uint64 { return 10; }
    print("$MARKER");
    return 0;
}
`

func numericIteratorHeapRangeSource(kind string) string {
	return numericIteratorHeapSource(kind, "ranges", numericIteratorHeapRanges)
}

// Direct range expressions select the fast latch; function-produced/stored
// Range values select IterInit/Next. VM may advance the stored cursor itself,
// so the second use proves lifetime without requiring it to restart like LLVM.
const numericIteratorHeapRanges = `
fn heap_range() -> Range<$K> { return heap_a()..heap_d(); }
fn heap_empty_range() -> Range<$K> { return heap_a()..heap_a(); }

fn heap_fast_normal() -> int64 {
    let start = heap_a();
    let end = heap_d();
    let mut sum: int64 = 0:int64;
    let mut count: int64 = 0:int64;
    for x: $K in start..end {
        sum = sum + heap_offset(x);
        count = count + 1:int64;
    }
    if count != 3:int64 { return -1:int64; }
    if (start:uint64) != 9223372036854775811:uint64 { return -2:int64; }
    if (end:uint64) != 9223372036854775814:uint64 { return -3:int64; }
    return sum;
}

fn heap_fast_expression_continue() -> int64 {
    let mut attempts: int64 = 0:int64;
    let mut sum: int64 = 0:int64;
    for x: $K in heap_a()..heap_d() {
        attempts = attempts + 1:int64;
        if attempts > 4:int64 { return -1:int64; }
        let ignored: int64 = {
            if (x:uint64) == 9223372036854775812:uint64 { continue; }
            ret 0:int64;
        };
        sum = sum + heap_offset(x) + ignored;
    }
    if attempts != 3:int64 { return -2:int64; }
    return sum;
}

fn heap_fast_break() -> $K {
    let mut found: $K = 0:$K;
    for x: $K in heap_a()..heap_d() { found = x; break; }
    return found;
}

fn heap_end_initializer(leave: bool) -> $K {
    for x: $K in heap_a()..({
        if leave { return heap_c(); }
        ret heap_d();
    }) { return x; }
    return 0:$K;
}

fn heap_saved_range() -> int64 {
    let start = heap_a();
    let end = heap_d();
    let saved: Range<$K> = start..end;
    let mut sum: int64 = 0:int64;
    for x in saved { sum = sum + heap_offset(x); }
    for _ in saved { break; }
    if (start:uint64) != 9223372036854775811:uint64 { return -1:int64; }
    if (end:uint64) != 9223372036854775814:uint64 { return -2:int64; }
    return sum;
}

fn heap_walk_range(r: Range<$K>) -> int64 {
    let mut sum: int64 = 0:int64;
    for x in r { sum = sum + heap_offset(x); }
    return sum;
}

fn heap_temporary_range() -> int64 {
    let mut sum: int64 = 0:int64;
    for x in heap_range() { sum = sum + heap_offset(x); }
    return sum;
}

fn heap_return_bound() -> $K {
    for x in heap_range() {
        if (x:uint64) == 9223372036854775812:uint64 { return x; }
    }
    return 0:$K;
}

fn heap_empty_count() -> int64 {
    let empty = heap_empty_range();
    let mut count: int64 = 0:int64;
    for x in empty { count = count + 1:int64; }
    return count;
}

@entrypoint
fn main() -> int {
    if heap_fast_normal() != 3:int64 { return 21; }
    if heap_fast_expression_continue() != 2:int64 { return 22; }
    let broken = heap_fast_break();
    if (broken:uint64) != 9223372036854775811:uint64 { return 23; }
    let early = heap_end_initializer(true);
    if (early:uint64) != 9223372036854775813:uint64 { return 24; }
    let entered = heap_end_initializer(false);
    if (entered:uint64) != 9223372036854775811:uint64 { return 25; }
    if heap_saved_range() != 3:int64 { return 26; }
    if heap_walk_range(heap_range()) != 3:int64 { return 27; }
    if heap_temporary_range() != 3:int64 { return 28; }
    let returned = heap_return_bound();
    if (returned:uint64) != 9223372036854775812:uint64 { return 29; }
    if heap_empty_count() != 0:int64 { return 30; }
    print("$MARKER");
    return 0;
}
`
