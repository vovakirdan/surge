//go:build !golden

package vm_test

import (
	"strings"
	"testing"
	"time"
)

// `compare v { ... }` desugars to a synthetic `let __cmpN = v` created in
// the HIR normalize pass, AFTER sema computed drop obligations — so
// __cmpN never acquired a scope-exit drop, and its union box leaked on
// every evaluated compare (RV2-DEBT-052): one 16-byte block per
// evaluated compare, local and far alike (the far/local census in
// runtime_v2_crossing_heap_capture_e2e_test.go windows a union compare
// too, but cancels the residue via its differential rather than pinning
// it directly).
//
// This file windows a union compare DIRECTLY, on a self-contained union
// type (not the crossing machinery's TaskResult<T>), across the arm
// shapes the fix distinguishes:
//   - wildcard: the taken arm never binds the payload — deep drop must
//     reclaim the payload's own heap content, not just the envelope.
//   - nopayload: the taken arm's tag carries no payload at all (the
//     Cancelled()-shaped case) — deep drop (a no-op beyond the box, but
//     exercised on its own dedicated path).
//   - bound: the taken arm extracts the payload to a binding and MOVES
//     it out (returned, matching the `Success(x) => x` shape every
//     existing TaskResult call site uses) — shallow envelope release
//     (the string is now owned by the caller, not dropped here at all).
//   - guarded: a guarded arm whose guard fails must NOT release — the
//     box has to stay alive for the very next arm's tag test against
//     the same scrutinee.
//   - guardBorrowFallthrough: adversarial-review addition (finding #1)
//     for the shape the original guarded probe deliberately dodged —
//     the failing arm's payload pattern IS a real binding this time,
//     and its guard BORROWS it (never moves it). Extraction runs
//     unconditionally before the guard (see lowerTagArm), so this
//     exercises extraction -> guard-borrow -> guard-fails -> fallthrough
//     -> a later arm's deep drop, end to end. This is "now safe" (per
//     the review) specifically because the guard never moves the
//     binding out — see the sema fix rejecting that shape — and because
//     the failing arm's own binding is never itself dropped (a
//     DIFFERENT, pre-existing, RV2-DEBT-058 gap this probe deliberately
//     does NOT exercise): it never took ownership of anything (its
//     extraction is a bare alias, same underlying pointer __cmpN's own
//     payload slot holds), so the fallthrough arm's deep drop reclaims
//     the box and the payload exactly once, correctly, on its own.
//   - nested: a compare arm's result is itself another compare — proves
//     each compare's release is independent and doesn't interfere with
//     an enclosing one.
//
// Every probe builds its payload with a runtime string concatenation
// (build_tag), never a literal, so the payload's own backing bytes are a
// genuine heap allocation the deep-drop arms must reclaim. Every probe
// constructs its scrutinee DIRECTLY as the compare's own value
// expression (`compare Payload(...) { ... }`), never through an
// intermediate explicitly-typed `let` — going through such a `let`
// forces a structural-to-nominal union re-tagging cast
// (`emitUnionCast`, internal/backend/llvm/emit_union_cast.go) that
// allocates a fresh box and never frees the one it replaces, an
// unrelated, newly-found leak (see the report) that would otherwise
// swamp this window on every single probe.
//
// A payload arm that binds and then LOCALLY CONSUMES the payload
// (e.g. `Payload(s) => len(s);`, never propagating `s` itself out) hits
// a second, separate, newly-found gap: compare-arm pattern bindings are
// type-checked by sema (inferComparePatternTypes) but never walk through
// registerDroppableBinding the way an ordinary `let` does (that call
// only fires from the ast.StmtLet case in type_checker_walk.go), so `s`
// never acquires a scope-exit drop obligation at all — independent of
// RV2-DEBT-052's own __cmpN gap. The bound/guarded probes below dodge
// this by moving the extracted binding OUT (`Payload(s) => s;`), the
// shape every real call site already uses, so this window stays
// specific to the scrutinee release this fix actually makes.
//
// The non-Payload fallback arm in every probe that also binds a payload
// is a bare `_`, never `Empty()`: the scrutinee is constructed directly
// as `Payload(...)` (see above) so its inferred type only carries the
// Payload case, and a same-compare `Empty()` pattern against a type
// that doesn't structurally include it is an LLVM-emit-time error
// ("unknown tag Empty for type ... Payload"), not merely dead code. A
// bare wildcard needs no such structural membership and stays exhaustive
// regardless.
const runtimeV2CompareScrutineeReleaseSource = `
tag Payload(string);
tag Empty();
type Outcome = Payload(string) | Empty;

@copy type Cell = { a: int, b: int };
tag Held(Cell);
tag Absent();
@copy type Holder = Held(Cell) | Absent;

tag Reading(float);
tag NoReading();
type Measure = Reading(float) | NoReading;

// A TWO-slot payload, for the arm shape that binds one field and ignores
// the other. That shape used to leave the envelope with no release at
// all — a shallow free would have abandoned the ignored field, a deep
// drop would have freed the bound one twice — so it leaked one box per
// evaluation. The ignored field now gets a binding of its own, which
// makes the arm all-bound and the shallow release correct again.
tag Both(string, int);
tag Neither();
type Duo = Both(string, int) | Neither;

// A union that MOVES carrying a payload that CLONES. The two halves are
// deliberately opposite: Carrier has no @copy, so the compare owns its
// scrutinee, while Cell does, so reading the payload duplicates it and
// leaves the original inside the envelope. An arm that ignores such a
// payload therefore must NOT be treated as having taken it out — doing
// that frees the envelope shallowly and abandons the original box, which
// is a leak a string-payload probe cannot see, because a string moves.
tag Boxed(Cell);
tag Unboxed();
type Carrier = Boxed(Cell) | Unboxed;

fn build_tag(prefix: string) -> string {
    let mut s = prefix;
    let mut i = 0;
    while i < 4 {
        s = s + "x";
        i = i + 1;
    }
    return s;
}

fn wildcard_once() -> int {
    return compare Payload(build_tag("w-")) {
        _ => 1;
    };
}

fn wildcard_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + wildcard_once();
        i = i + 1;
    }
    return total;
}

fn nopayload_once() -> int {
    return compare Empty() {
        Empty() => 1;
        _ => 0 - 1;
    };
}

fn nopayload_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + nopayload_once();
        i = i + 1;
    }
    return total;
}

fn bound_once() -> string {
    return compare Payload(build_tag("v-")) {
        Payload(s) => s;
        _ => "";
    };
}

// A compare whose scrutinee is a DEREFERENCED BORROW owns nothing: the
// box belongs to the caller, who frees it on its own scope exit. The
// deref strips the reference, so the scrutinee's TYPE is a bare union
// and a type-only ownership test concludes this compare may free the
// box — it may not, and doing so frees the caller's storage a second
// time. The payload pattern is a wildcard so that no binding aliases
// anything: that isolates the SCRUTINEE's release from the payload
// lifecycle, and it is the arm shape that would take the deep-drop path.
//
// This is what made every formatted print with a non-string argument a
// double free on the native backend: core/format.sg's append_fmt_arg
// takes an &FmtArg and compares through a deref of it. The VM survived
// it because its heap is reference-counted, which is exactly why the
// VM/LLVM differential could not see this and a census had to.
fn borrowed_read(arg: &Outcome) -> int {
    return compare *arg {
        Payload(_) => 1;
        _ => 0;
    };
}

fn borrowed_once() -> int {
    let owned: Outcome = Payload(build_tag("b-"));
    return borrowed_read(&owned);
}

// The payload half of the same question, and the one that needs OPPOSITE
// instructions from the owned case. A binding that extracts a
// reference-counted scalar out of a BORROWED union must take a reference
// of its own, because the union keeps its own and outlives the arm; a
// binding extracting from an OWNED union must not, because there the
// single reference transfers into the binding and the envelope release
// leaves the payload alone. Both spell the same tag_payload read, so the
// ownership answer has to be carried down from the compare.
//
// Getting it wrong in either direction is a memory error, which is why
// this probe and float_owned_n_times below must BOTH stay balanced: an
// unconditional retain balances this one and leaks that one.
fn borrowed_float_read(arg: &Measure) -> float {
    return compare *arg {
        Reading(v) => v;
        _ => 0.0;
    };
}

fn borrowed_float_once() -> float {
    let owned: Measure = Reading(1.5 + 0.25);
    return borrowed_float_read(&owned);
}

fn borrowed_float_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        let v: float = borrowed_float_once();
        if v > 1.0 { total = total + 1; }
        i = i + 1;
    }
    return total;
}

fn owned_float_once() -> float {
    return compare Reading(1.5 + 0.25) {
        Reading(v) => v;
        _ => 0.0;
    };
}

fn owned_float_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        let v: float = owned_float_once();
        if v > 1.0 { total = total + 1; }
        i = i + 1;
    }
    return total;
}

fn borrowed_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + borrowed_once();
        i = i + 1;
    }
    return total;
}

// One field bound, the other ignored: the MIXED arm. Both fields of the
// envelope must be accounted for — the string by the binding that names
// it, the ignored one by a binding synthesized for it — before the box
// itself can be freed shallowly.
fn mixed_once() -> string {
    return compare Both(build_tag("m-"), 7) {
        Both(s, _) => s;
        _ => "";
    };
}

fn mixed_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        let r: string = mixed_once();
        total = total + (len(r) to int);
        i = i + 1;
    }
    return total;
}

// The mirror image: the field that OWNS something is the ignored one, so
// the synthesized binding is what reclaims it. A shallow free without it
// abandons the string.
fn mixed_ignored_owner_once() -> int {
    return compare Both(build_tag("g-"), 7) {
        Both(_, n) => n;
        _ => 0 - 1;
    };
}

fn mixed_ignored_owner_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + mixed_ignored_owner_once();
        i = i + 1;
    }
    return total;
}

// The ignored payload CLONES when read, so only a deep drop reclaims the
// original. Two spellings, because the second is where the abandoned
// clone would land instead: a guard that never passes still runs the
// extraction, and its drop lives on the return the guard skips.
fn copy_payload_ignored_once() -> int {
    return compare Boxed(Cell { a = 1, b = 2 }) {
        Boxed(_) => 1;
        _ => 0;
    };
}

fn copy_payload_ignored_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + copy_payload_ignored_once();
        i = i + 1;
    }
    return total;
}

fn copy_payload_guard_once() -> int {
    return compare Boxed(Cell { a = 1, b = 2 }) {
        Boxed(_) if false => 1;
        _ => 0;
    };
}

fn copy_payload_guard_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + copy_payload_guard_once();
        i = i + 1;
    }
    return total;
}

fn bound_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        let r: string = bound_once();
        total = total + (len(r) to int);
        i = i + 1;
    }
    return total;
}

// The first arm's payload pattern is a wildcard, so no binding is ever
// created there regardless of the guard outcome: this isolates the
// "guard fails, fall through" path from the extracted-binding lifecycle
// entirely, so it pins exactly one thing — that the scrutinee's own box
// survives a failed guard so the SECOND arm's tag test still works.
fn guarded_once() -> string {
    return compare Payload(build_tag("g-")) {
        Payload(_) if false => "unreachable";
        Payload(s) => s;
        _ => "";
    };
}

fn guarded_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        let r: string = guarded_once();
        total = total + (len(r) to int);
        i = i + 1;
    }
    return total;
}

// Always false for any string shorter than 1000 chars — this guard
// exists purely to make the taken tag's arm fail deterministically,
// every call, while still requiring a real payload extraction first.
fn too_long(s: &string) -> bool {
    return (len(s) to int) > 1000;
}

// The failing arm's payload IS bound (unlike guarded_once's dodge
// above) and its guard only BORROWS it (&x) — the shape RV2-DEBT-052's
// adversarial review flagged as unsafe when the guard instead MOVED the
// binding (rejected at sema now; see compare_move_test.go). Falls
// through to the wildcard arm's deep drop, which reclaims the box and
// the payload together, exactly once.
fn guard_borrow_fallthrough_once() -> int {
    return compare Payload(build_tag("h-")) {
        Payload(x) if too_long(&x) => 1;
        _ => 0;
    };
}

fn guard_borrow_fallthrough_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + guard_borrow_fallthrough_once();
        i = i + 1;
    }
    return total;
}

// The outer arm is a wildcard (deep-drop-safe, same shape as
// wildcard_once) so this stays focused on what it actually probes:
// that the OUTER compare's release doesn't interfere with the INNER
// compare's own (independent) release, and vice versa.
fn nested_once() -> int {
    return compare Payload(build_tag("n-")) {
        Payload(_) => compare Empty() {
            Empty() => 1;
            _ => 0 - 1;
        };
        _ => 0 - 1;
    };
}

fn nested_n_times(n: int) -> int {
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + nested_once();
        i = i + 1;
    }
    return total;
}

// A COPY VALUE COMPOSITE scrutinee, read through a borrow. This is the
// shape whose clone nobody freed: comparing *h DUPLICATES the union into
// the compare's temp rather than moving it, so the temp holds an
// independent box that is the compare's to reclaim while the original
// stays untouched -- which is why releasing it is safe here and would
// not be for a move-only union behind the same deref.
//
// Both arm shapes are probed because they reclaim for OPPOSITE reasons
// and a fix that handles one can miss the other: the wildcard arm never
// took anything out of the clone, and the binding arm's c is itself a
// CLONE of the payload, so the envelope keeps its own payload and needs
// the same deep release rather than a shallow envelope free.
//
// The scrutinee is built ONCE, outside the loop, so the differential over
// n measures only the per-evaluation clone.
fn composite_bound_once(h: &Holder) -> int {
    return compare *h {
        Held(c) => c.a;
        _ => 0 - 1;
    };
}

fn composite_bound_n_times(n: int) -> int {
    let h: Holder = Held(Cell { a = 1, b = 2 });
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + composite_bound_once(&h);
        i = i + 1;
    }
    return total;
}

fn composite_wildcard_once(h: &Holder) -> int {
    return compare *h {
        _ => 1;
    };
}

fn composite_wildcard_n_times(n: int) -> int {
    let h: Holder = Held(Cell { a = 1, b = 2 });
    let mut total = 0;
    let mut i = 0;
    while i < n {
        total = total + composite_wildcard_once(&h);
        i = i + 1;
    }
    return total;
}

fn diff_check(label: string, before1: &HeapStats, after1: &HeapStats, before2: &HeapStats, after2: &HeapStats) -> int {
    let allocs1: uint = after1.alloc_count - before1.alloc_count;
    let frees1: uint = after1.free_count - before1.free_count;
    let allocs2: uint = after2.alloc_count - before2.alloc_count;
    let frees2: uint = after2.free_count - before2.free_count;
    let alloc_diff: uint = allocs2 - allocs1;
    let free_diff: uint = frees2 - frees1;
    if alloc_diff != free_diff {
        print(label);
        print("alloc/free differential mismatch");
        print(alloc_diff to string);
        print(free_diff to string);
        return 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let wc1_before: HeapStats = rt_heap_stats();
    let wc1: int = wildcard_n_times(1);
    let wc1_after: HeapStats = rt_heap_stats();
    let wc2000_before: HeapStats = rt_heap_stats();
    let wc2000: int = wildcard_n_times(2000);
    let wc2000_after: HeapStats = rt_heap_stats();
    if wc1 != 1 || wc2000 != 2000 {
        print("wildcard value mismatch");
        print(wc1 to string);
        print(wc2000 to string);
        return 1;
    }
    let r1: int = diff_check("wildcard", &wc1_before, &wc1_after, &wc2000_before, &wc2000_after);
    if r1 != 0 { return 10 + r1; }

    let np1_before: HeapStats = rt_heap_stats();
    let np1: int = nopayload_n_times(1);
    let np1_after: HeapStats = rt_heap_stats();
    let np2000_before: HeapStats = rt_heap_stats();
    let np2000: int = nopayload_n_times(2000);
    let np2000_after: HeapStats = rt_heap_stats();
    if np1 != 1 || np2000 != 2000 {
        print("nopayload value mismatch");
        print(np1 to string);
        print(np2000 to string);
        return 2;
    }
    let r2: int = diff_check("nopayload", &np1_before, &np1_after, &np2000_before, &np2000_after);
    if r2 != 0 { return 20 + r2; }

    let bd1_before: HeapStats = rt_heap_stats();
    let bd1: int = bound_n_times(1);
    let bd1_after: HeapStats = rt_heap_stats();
    let bd2000_before: HeapStats = rt_heap_stats();
    let bd2000: int = bound_n_times(2000);
    let bd2000_after: HeapStats = rt_heap_stats();
    if bd1 != 6 || bd2000 != 12000 {
        print("bound value mismatch");
        print(bd1 to string);
        print(bd2000 to string);
        return 3;
    }
    let r3: int = diff_check("bound", &bd1_before, &bd1_after, &bd2000_before, &bd2000_after);
    if r3 != 0 { return 30 + r3; }

    let gd1_before: HeapStats = rt_heap_stats();
    let gd1: int = guarded_n_times(1);
    let gd1_after: HeapStats = rt_heap_stats();
    let gd2000_before: HeapStats = rt_heap_stats();
    let gd2000: int = guarded_n_times(2000);
    let gd2000_after: HeapStats = rt_heap_stats();
    if gd1 != 6 || gd2000 != 12000 {
        print("guarded value mismatch");
        print(gd1 to string);
        print(gd2000 to string);
        return 4;
    }
    let r4: int = diff_check("guarded", &gd1_before, &gd1_after, &gd2000_before, &gd2000_after);
    if r4 != 0 { return 40 + r4; }

    let gb1_before: HeapStats = rt_heap_stats();
    let gb1: int = guard_borrow_fallthrough_n_times(1);
    let gb1_after: HeapStats = rt_heap_stats();
    let gb2000_before: HeapStats = rt_heap_stats();
    let gb2000: int = guard_borrow_fallthrough_n_times(2000);
    let gb2000_after: HeapStats = rt_heap_stats();
    if gb1 != 0 || gb2000 != 0 {
        print("guard-borrow-fallthrough value mismatch");
        print(gb1 to string);
        print(gb2000 to string);
        return 6;
    }
    let r6: int = diff_check("guard-borrow-fallthrough", &gb1_before, &gb1_after, &gb2000_before, &gb2000_after);
    if r6 != 0 { return 60 + r6; }

    let ns1_before: HeapStats = rt_heap_stats();
    let ns1: int = nested_n_times(1);
    let ns1_after: HeapStats = rt_heap_stats();
    let ns2000_before: HeapStats = rt_heap_stats();
    let ns2000: int = nested_n_times(2000);
    let ns2000_after: HeapStats = rt_heap_stats();
    if ns1 != 1 || ns2000 != 2000 {
        print("nested value mismatch");
        print(ns1 to string);
        print(ns2000 to string);
        return 5;
    }
    let r5: int = diff_check("nested", &ns1_before, &ns1_after, &ns2000_before, &ns2000_after);
    if r5 != 0 { return 50 + r5; }

    let bw1_before: HeapStats = rt_heap_stats();
    let bw1: int = borrowed_n_times(1);
    let bw1_after: HeapStats = rt_heap_stats();
    let bw2000_before: HeapStats = rt_heap_stats();
    let bw2000: int = borrowed_n_times(2000);
    let bw2000_after: HeapStats = rt_heap_stats();
    if bw1 != 1 || bw2000 != 2000 {
        print("borrowed value mismatch");
        print(bw1 to string);
        print(bw2000 to string);
        return 6;
    }
    let r7: int = diff_check("borrowed", &bw1_before, &bw1_after, &bw2000_before, &bw2000_after);
    if r7 != 0 { return 60 + r7; }

    let bf1_before: HeapStats = rt_heap_stats();
    let bf1: int = borrowed_float_n_times(1);
    let bf1_after: HeapStats = rt_heap_stats();
    let bf2000_before: HeapStats = rt_heap_stats();
    let bf2000: int = borrowed_float_n_times(2000);
    let bf2000_after: HeapStats = rt_heap_stats();
    if bf1 != 1 || bf2000 != 2000 {
        print("borrowed-float value mismatch");
        print(bf1 to string);
        print(bf2000 to string);
        return 7;
    }
    let r8: int = diff_check("borrowed-float", &bf1_before, &bf1_after, &bf2000_before, &bf2000_after);
    if r8 != 0 { return 70 + r8; }

    let of1_before: HeapStats = rt_heap_stats();
    let of1: int = owned_float_n_times(1);
    let of1_after: HeapStats = rt_heap_stats();
    let of2000_before: HeapStats = rt_heap_stats();
    let of2000: int = owned_float_n_times(2000);
    let of2000_after: HeapStats = rt_heap_stats();
    if of1 != 1 || of2000 != 2000 {
        print("owned-float value mismatch");
        print(of1 to string);
        print(of2000 to string);
        return 8;
    }
    let r9: int = diff_check("owned-float", &of1_before, &of1_after, &of2000_before, &of2000_after);
    if r9 != 0 { return 80 + r9; }

    let cb1_before: HeapStats = rt_heap_stats();
    let cb1: int = composite_bound_n_times(1);
    let cb1_after: HeapStats = rt_heap_stats();
    let cb2000_before: HeapStats = rt_heap_stats();
    let cb2000: int = composite_bound_n_times(2000);
    let cb2000_after: HeapStats = rt_heap_stats();
    if cb1 != 1 || cb2000 != 2000 {
        print("composite-bound value mismatch");
        print(cb1 to string);
        print(cb2000 to string);
        return 9;
    }
    let r10: int = diff_check("composite-bound", &cb1_before, &cb1_after, &cb2000_before, &cb2000_after);
    if r10 != 0 { return 90 + r10; }

    let cw1_before: HeapStats = rt_heap_stats();
    let cw1: int = composite_wildcard_n_times(1);
    let cw1_after: HeapStats = rt_heap_stats();
    let cw2000_before: HeapStats = rt_heap_stats();
    let cw2000: int = composite_wildcard_n_times(2000);
    let cw2000_after: HeapStats = rt_heap_stats();
    if cw1 != 1 || cw2000 != 2000 {
        print("composite-wildcard value mismatch");
        print(cw1 to string);
        print(cw2000 to string);
        return 11;
    }
    let r11: int = diff_check("composite-wildcard", &cw1_before, &cw1_after, &cw2000_before, &cw2000_after);
    if r11 != 0 { return 110 + r11; }

    let mx1_before: HeapStats = rt_heap_stats();
    let mx1: int = mixed_n_times(1);
    let mx1_after: HeapStats = rt_heap_stats();
    let mx2000_before: HeapStats = rt_heap_stats();
    let mx2000: int = mixed_n_times(2000);
    let mx2000_after: HeapStats = rt_heap_stats();
    if mx1 != 6 || mx2000 != 12000 {
        print("mixed value mismatch");
        print(mx1 to string);
        print(mx2000 to string);
        return 12;
    }
    let r12: int = diff_check("mixed", &mx1_before, &mx1_after, &mx2000_before, &mx2000_after);
    if r12 != 0 { return 120 + r12; }

    let mo1_before: HeapStats = rt_heap_stats();
    let mo1: int = mixed_ignored_owner_n_times(1);
    let mo1_after: HeapStats = rt_heap_stats();
    let mo2000_before: HeapStats = rt_heap_stats();
    let mo2000: int = mixed_ignored_owner_n_times(2000);
    let mo2000_after: HeapStats = rt_heap_stats();
    if mo1 != 7 || mo2000 != 14000 {
        print("mixed-ignored-owner value mismatch");
        print(mo1 to string);
        print(mo2000 to string);
        return 13;
    }
    let r13: int = diff_check("mixed-ignored-owner", &mo1_before, &mo1_after, &mo2000_before, &mo2000_after);
    if r13 != 0 { return 130 + r13; }

    let ci1_before: HeapStats = rt_heap_stats();
    let ci1: int = copy_payload_ignored_n_times(1);
    let ci1_after: HeapStats = rt_heap_stats();
    let ci2000_before: HeapStats = rt_heap_stats();
    let ci2000: int = copy_payload_ignored_n_times(2000);
    let ci2000_after: HeapStats = rt_heap_stats();
    if ci1 != 1 || ci2000 != 2000 {
        print("copy-payload-ignored value mismatch");
        print(ci1 to string);
        print(ci2000 to string);
        return 14;
    }
    let r14: int = diff_check("copy-payload-ignored", &ci1_before, &ci1_after, &ci2000_before, &ci2000_after);
    if r14 != 0 { return 140 + r14; }

    let cg1_before: HeapStats = rt_heap_stats();
    let cg1: int = copy_payload_guard_n_times(1);
    let cg1_after: HeapStats = rt_heap_stats();
    let cg2000_before: HeapStats = rt_heap_stats();
    let cg2000: int = copy_payload_guard_n_times(2000);
    let cg2000_after: HeapStats = rt_heap_stats();
    if cg1 != 0 || cg2000 != 0 {
        print("copy-payload-guard value mismatch");
        print(cg1 to string);
        print(cg2000 to string);
        return 15;
    }
    let r15: int = diff_check("copy-payload-guard", &cg1_before, &cg1_after, &cg2000_before, &cg2000_after);
    if r15 != 0 { return 150 + r15; }

    print("compare-scrutinee-release-ok");
    return 0;
}
`

func TestRuntimeV2CompareScrutineeReleaseCensusBalanced(t *testing.T) {
	outputPath := buildRuntimeV2CrossingSource(t, runtimeV2CompareScrutineeReleaseSource, nil)
	baseEnv := envWithStdlib(repoRoot(t))
	duration, result := runBinaryWithTimeout(t, outputPath, baseEnv, 30*time.Second)
	if result.exitCode != 0 {
		t.Fatalf(
			"compare scrutinee release census failed (exit=%d, duration=%s)\nstdout:\n%s\nstderr:\n%s",
			result.exitCode,
			duration,
			result.stdout,
			result.stderr,
		)
	}
	if !strings.Contains(result.stdout, "compare-scrutinee-release-ok") {
		t.Fatalf("compare scrutinee release census missing completion marker; stdout=%q", result.stdout)
	}
}
