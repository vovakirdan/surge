package driver

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"surge/internal/diag"
)

const projectionPrelude = aliasThroughRefPrelude + `fn hid(k: &mut H) -> &mut H { return k; }
fn it0(k: &mut H) -> &mut int { return &mut k.items[0]; }
fn ss0(k: &mut H) -> &mut string { return &mut k.ss[0]; }
fn fsH(k: &mut H) -> &mut string { return idsm(k.s); }
fn fstm(k: &mut H) -> &mut int[] { return idam(k.items); }

type H2 = { xs: int[], ys: int[] };
fn mkH2() -> H2 { return H2{ xs: [1, 2, 3], ys: [4, 5, 6] }; }
fn pickf(k: &mut H2) -> &mut int[] { return idam(k.ys); }

fn idmap(m: &mut Map<string, string>) -> &mut Map<string, string> { return m; }
fn insm(m: &mut Map<string, string>) -> nothing {
    let mut i = 0;
    while i < 50 { m.insert(heap("k") + (i to string), heap("w")); i = i + 1; }
    return nothing;
}

`

// A `&mut` a call returns from a reference to a record points somewhere inside
// it, and without a summary of the callee the checker cannot tell where. It
// stood on a reborrow of the whole referent, which a grow or a replace through
// the reference is excused by, so the store through it wrote the freed buffer
// (valgrind: invalid read and write). It now holds an exclusive element loan
// on each dynamic buffer inside the referent that may hold its referent type,
// and a reference taken through a returned field holds that field's element.
func TestReferenceReturnedIntoARecordHoldsItsBuffers(t *testing.T) {
	rows := []struct {
		name, text, snippet string
		code                diag.Code
	}{
		{"element_of_a_field_then_the_record_grown", `fn g(k: &mut H) -> int {
    let e = it0(k);
    k.grow();
    *e = 5;
    return 0;
}
`, "k.grow", diag.SemaBorrowConflict},
		{"element_of_a_field_then_the_record_replaced", `fn g(k: &mut H) -> int {
    let e = it0(k);
    setH(k);
    *e = 5;
    return 0;
}
`, "k);\n    *e", diag.SemaBorrowConflict},
		{"element_of_a_field_then_the_record_assigned", `fn g(k: &mut H) -> int {
    let e = it0(k);
    *k = mkH();
    *e = 5;
    return 0;
}
`, "*k = mkH()", diag.SemaBorrowMutation},
		{"element_of_a_field_then_the_field_grown", `fn g(k: &mut H) -> int {
    let e = it0(k);
    app(k.items);
    *e = 5;
    return 0;
}
`, "k.items);\n    *e", diag.SemaBorrowConflict},
		{"string_element_then_the_record_replaced", `fn g(k: &mut H) -> int {
    let e = ss0(k);
    setH(k);
    *e = heap("w");
    return 0;
}
`, "k);\n    *e", diag.SemaBorrowConflict},
		{"string_element_then_the_field_grown", `fn g(k: &mut H) -> int {
    let e = ss0(k);
    apps(k.ss);
    return e.__len() to int;
}
`, "k.ss);", diag.SemaBorrowConflict},
		{"element_through_an_alias_then_the_source_grown", `fn g(k0: &mut H) -> int {
    let k = hid(k0);
    let e = it0(k);
    k0.grow();
    *e = 5;
    return 0;
}
`, "k0.grow", diag.SemaBorrowConflict},
		{"element_through_a_local_reference_then_replaced", `fn g() -> int {
    let mut hh = mkH();
    let k = &mut hh;
    let e = it0(k);
    setH(k);
    *e = 5;
    return 0;
}
`, "k);\n    *e", diag.SemaBorrowConflict},
		{"element_of_a_returned_field_then_the_field_grown", `fn g(k: &mut H) -> int {
    let q = fstm(k);
    let e = &mut q[0];
    app(k.items);
    *e = 5;
    return 0;
}
`, "k.items);\n    *e", diag.SemaBorrowConflict},
		{"interior_of_a_returned_field_then_the_field_grown", `fn g(k: &mut H) -> int {
    let e = firstm(fstm(k));
    app(k.items);
    *e = 5;
    return 0;
}
`, "k.items);\n    *e", diag.SemaBorrowConflict},
		{"interior_of_a_bound_returned_field_then_the_field_grown", `fn g(k: &mut H) -> int {
    let q = fstm(k);
    let e = firstm(q);
    app(k.items);
    *e = 5;
    return 0;
}
`, "k.items);\n    *e", diag.SemaBorrowConflict},
		{"element_of_one_of_two_candidate_fields_then_the_other_grown", `fn g(k: &mut H2) -> int {
    let q = pickf(k);
    let e = &mut q[0];
    app(k.ys);
    *e = 5;
    return 0;
}
`, "k.ys);\n    *e", diag.SemaBorrowConflict},
		// fsH may as well return `&mut k.ss[0]` (ss0 has its signature), so a
		// replace of the record is refused while its result lives.
		{"string_that_may_be_an_element_then_the_record_assigned", `fn g(k: &mut H) -> int {
    let q = fsH(k);
    *k = mkH();
    *q = heap("w");
    return 0;
}
`, "*k = mkH()", diag.SemaBorrowMutation},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, projectionPrelude+row.text, row.code, row.snippet)
		})
	}
}

// A field slot survives a replace of its record, and a buffer the result
// cannot point into is not held.
func TestReferenceReturnedIntoARecordControlsStayAccepted(t *testing.T) {
	rows := []struct{ name, text string }{
		{"returned_field_then_the_record_replaced", `fn g(k: &mut H) -> int {
    let q = fstm(k);
    setH(k);
    q.push(1);
    return q[0];
}
`},
		{"element_of_a_field_then_another_buffer_grown", `fn g(k: &mut H) -> int {
    let e = it0(k);
    apps(k.ss);
    *e = 5;
    return 0;
}
`},
		{"returned_field_then_another_field_read", `fn g(k: &mut H) -> int {
    let q = fstm(k);
    let n = k.s.__len() to int;
    q.push(n);
    return 0;
}
`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, projectionPrelude+row.text)
		})
	}
}

// `get_mut` through a `&mut Map` reference hands out a `&mut V` into the
// entries and took no loan: an insert or a remove through the reference
// rehashed or freed the entry the arm's binding still wrote (valgrind:
// invalid read and write). It now holds an exclusive entry loan, as the
// receiver's own borrow does for a local map.
func TestMapEntryThroughAReferenceIsHeld(t *testing.T) {
	rows := []struct{ name, text, snippet string }{
		{"entry_then_inserted_through_the_parameter", `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    compare m.get_mut(&k) { Some(v) => { insm(m); *v = heap("z"); } nothing => {} };
    return 0;
}
`, "m); *v"},
		{"entry_then_removed_through_the_parameter", `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    compare m.get_mut(&k) { Some(v) => { let _ = m.remove(&k); *v = heap("z"); } nothing => {} };
    return 0;
}
`, "m.remove"},
		{"entry_through_an_alias_then_inserted_through_the_source", `fn g(m: &mut Map<string, string>) -> int {
    let q = idmap(m);
    let k = heap("k");
    compare q.get_mut(&k) { Some(v) => { insm(m); *v = heap("z"); } nothing => {} };
    return 0;
}
`, "m); *v"},
		{"entry_bound_then_inserted", `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    let o = m.get_mut(&k);
    insm(m);
    compare o { Some(v) => { *v = heap("z"); } nothing => {} };
    return 0;
}
`, "m);\n    compare"},
		{"entry_through_a_local_reference_then_inserted", `fn g() -> int {
    let mut mm = Map::<string, string>.new();
    let r = &mut mm;
    let k = heap("k");
    compare r.get_mut(&k) { Some(v) => { insm(r); *v = heap("z"); } nothing => {} };
    return 0;
}
`, "r); *v"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, projectionPrelude+row.text, diag.SemaBorrowConflict, row.snippet)
		})
	}
	bytesViewAccepted(t, projectionPrelude+`fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    compare m.get_mut(&k) { Some(v) => { *v = heap("z"); } nothing => {} };
    return 0;
}
`)
}

// A `&mut` into an array chosen by a compare or a ternary held no element:
// the choice was not walked to the calls it chose among, so a grow through
// either source freed what it pointed at.
func TestChosenInteriorReferenceHoldsAnElement(t *testing.T) {
	rows := []struct{ name, text, snippet string }{
		{"compare_of_one_source", `fn g(r: &mut int[]) -> int {
    let e: &mut int = compare r.__len() { 3 => firstm(r); _ => firstm(r); };
    app(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e"},
		{"ternary_of_two_sources", `fn g(r: &mut int[], s: &mut int[]) -> int {
    let e = r.__len() > 1 ? firstm(r) : firstm(s);
    app(s);
    *e = 5;
    return 0;
}
`, "s);\n    *e"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, projectionPrelude+row.text, diag.SemaBorrowConflict, row.snippet)
		})
	}
	bytesViewAccepted(t, projectionPrelude+`fn g(r: &mut int[]) -> int {
    let e: &mut int = compare r.__len() { 3 => firstm(r); _ => firstm(r); };
    *e = 4;
    return r[1];
}
`)
}

// A result that carries its `&mut` inside a wrapper -- `Option<&mut V>` from
// a map inside a record, a reference handed as an explicit `&mut *k` -- and a
// source nested deeper than any fixed bound hold the same loans.
func TestCarriedAndDeepProjectionsAreHeld(t *testing.T) {
	const hm = `type HM = { m: Map<string, string> };
fn ent(k: &mut HM, key: &string) -> Option<&mut string> { return k.m.get_mut(key); }
extern<HM> { fn entm(self: &mut HM, key: &string) -> Option<&mut string> { return self.m.get_mut(key); } }
fn idi(p: &mut int) -> &mut int { return p; }

`
	rows := []struct{ name, text, snippet string }{
		{"option_entry_of_a_map_in_a_record_then_inserted", `fn g(k: &mut HM) -> int {
    let kk = heap("k");
    let o = ent(k, &kk);
    insm(k.m);
    compare o { Some(v) => { *v = heap("z"); } nothing => {} };
    return 0;
}
`, "k.m);"},
		{"method_entry_of_a_map_in_a_record_then_inserted", `fn g(k: &mut HM) -> int {
    let kk = heap("k");
    compare k.entm(&kk) { Some(v) => { insm(k.m); *v = heap("z"); } nothing => {} };
    return 0;
}
`, "k.m); *v"},
		{"explicit_reborrow_of_a_record_then_grown", `fn g(k: &mut H) -> int {
    let e = it0(&mut *k);
    k.grow();
    *e = 5;
    return 0;
}
`, "k.grow"},
		{"explicit_reborrow_of_an_array_then_grown", `fn g(r: &mut int[]) -> int {
    let e = firstm(&mut *r);
    app(r);
    *e = 5;
    return 0;
}
`, "r);\n    *e"},
		{"explicit_reborrow_of_a_map_then_inserted", `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    compare (&mut *m).get_mut(&k) { Some(v) => { insm(m); *v = heap("z"); } nothing => {} };
    return 0;
}
`, "m); *v"},
		{"interior_reference_nested_ten_calls_deep", `fn g(k: &mut H) -> int {
    let e = idi(idi(idi(idi(idi(idi(idi(idi(idi(idi(it0(k)))))))))));
    k.grow();
    *e = 5;
    return 0;
}
`, "k.grow"},
		{"source_nested_ten_aliases_deep", `fn g(k: &mut H) -> int {
    let e = it0(hid(hid(hid(hid(hid(hid(hid(hid(hid(hid(k)))))))))));
    k.grow();
    *e = 5;
    return 0;
}
`, "k.grow"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, projectionPrelude+hm+row.text, diag.SemaBorrowConflict, row.snippet)
		})
	}
}

// A `&mut` a helper unwraps out of `get_mut` is one entry loan, as its owned
// form is; two loans on the entry refused the binding itself.
func TestUnwrappedMapEntryIsOneLoan(t *testing.T) {
	for _, row := range []struct{ name, text string }{
		{"string_entry", `fn ev(m: &mut Map<string, string>, k: &string) -> &mut string { return compare m.get_mut(k) { Some(v) => v; nothing => panic("x"); }; }
fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    let e = ev(m, &k);
    *e = heap("z");
    return 0;
}
`},
		{"array_entry", `fn ev(m: &mut Map<string, int[]>, k: &string) -> &mut int[] { return compare m.get_mut(k) { Some(v) => v; nothing => panic("x"); }; }
fn g(m: &mut Map<string, int[]>) -> int {
    let k = heap("k");
    let e = ev(m, &k);
    e.push(4);
    return 0;
}
`},
	} {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, projectionPrelude+row.text)
		})
	}
}

// A named lexical-loan restriction, the same as the owned form's: the entry
// loan an arm binding holds lives to the end of the enclosing block, so a
// grow of the map after the compare statement is refused.
func TestMapEntryLoanIsLexical(t *testing.T) {
	bytesViewRefusal(t, projectionPrelude+`fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    compare m.get_mut(&k) { Some(v) => { *v = heap("z"); } nothing => {} };
    insm(m);
    return 0;
}
`, diag.SemaBorrowConflict, "m);\n    return")
}

// nestedPairs spells T0..Tn, each holding two of the next, the last an int[]
// and an int: 2^n candidate places of int inside T0.
func nestedPairs(n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "type T%d = { a: int[], b: int };\n", n)
	for i := n - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "type T%d = { a: T%d, b: T%d };\n", i, i+1, i+1)
	}
	return b.String()
}

// Past the candidate bound a result holds one interior loan on the whole
// referent -- coarser, never absent -- and naming the candidates stays linear
// in the record's size.
func TestManyCandidatesHoldTheWholeReferent(t *testing.T) {
	t.Run("interior_loan", func(t *testing.T) {
		path := strings.Repeat(".a", 7)
		bytesViewRefusal(t, projectionPrelude+nestedPairs(6)+`fn pk(k: &mut T0) -> &mut int { return &mut k`+path+`[0]; }
fn g(k: &mut T0) -> int {
    let e = pk(k);
    app(k`+path+`);
    *e = 5;
    return 0;
}
`, diag.SemaBorrowConflict, "k"+path+");")
	})
	// 2^14 candidate places: named one by one they took half a minute.
	t.Run("bounded_time", func(t *testing.T) {
		start := time.Now()
		bytesViewAccepted(t, projectionPrelude+nestedPairs(14)+`fn pk(k: &mut T0) -> &mut int { return &mut k`+strings.Repeat(".b", 15)+`; }
fn g(k: &mut T0) -> int {
    let e = pk(k);
    *e = 5;
    return 0;
}
`)
		if spent := time.Since(start); spent > 10*time.Second {
			t.Fatalf("a record of 2^14 candidate places took %v to check", spent)
		}
	})
}
