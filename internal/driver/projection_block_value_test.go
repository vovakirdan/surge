package driver

import (
	"strings"
	"testing"

	"surge/internal/diag"
)

// A carried `&mut` leaving a block expression by `ret` -- the block a `let`,
// a compare arm or a ternary branch -- held its loan only to the end of the
// `ret` statement, so the binding the block initialized held nothing.
func TestCarriedEntryLeavingABlockIsHeld(t *testing.T) {
	const tail = `
    insm(m);
    compare o { Some(v) => { *v = heap("z"); } nothing => {} };
    return 0;
}
`
	const head = `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
`
	rows := []struct{ name, text string }{
		{"block_value", head + `    let o = { let z = 1; ret m.get_mut(&k); };` + tail},
		{"block_value_of_a_record_helper", `type HM = { m: Map<string, string> };
fn ent(k: &mut HM, key: &string) -> Option<&mut string> { return k.m.get_mut(key); }
fn g(k: &mut HM) -> int {
    let kk = heap("k");
    let o = { let z = 1; ret ent(k, &kk); };
    insm(k.m);
    compare o { Some(v) => { *v = heap("z"); } nothing => {} };
    return 0;
}
`},
		{"compare_arm_block", head + `    let o = compare k.__len() { 0 => nothing; _ => { ret m.get_mut(&k); } };` + tail},
		{"ternary_branch_block", head + `    let o = k.__len() > 0 ? { ret m.get_mut(&k); } : nothing;` + tail},
		{"nested_blocks", head + `    let o = { ret { ret m.get_mut(&k); }; };` + tail},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			snippet := "m);\n    compare o"
			if strings.Contains(row.text, "k.m);") {
				snippet = "k.m);"
			}
			bytesViewRefusal(t, projectionPrelude+row.text, diag.SemaBorrowConflict, snippet)
		})
	}
}

// An entry taken through an explicit reborrow and bound by nothing ends with
// its statement, as the implicit `m.get_mut(&k);` does.
func TestUnboundExplicitEntryEndsWithItsStatement(t *testing.T) {
	for _, row := range []struct{ name, text string }{
		{"discarded_by_let", `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    let _ = (&mut *m).get_mut(&k);
    insm(m);
    return 0;
}
`},
		{"expression_statement", `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    (&mut *m).get_mut(&k);
    insm(m);
    return 0;
}
`},
	} {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, projectionPrelude+row.text)
		})
	}
	// An arm binding holds the entry to the end of its block (the lexical
	// restriction above), through the explicit form as through the implicit.
	for _, recv := range []string{"(&mut *m)", "m"} {
		bytesViewRefusal(t, projectionPrelude+`fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    let n = compare `+recv+`.get_mut(&k) { Some(v) => 1; nothing => 0; };
    insm(m);
    return n;
}
`, diag.SemaBorrowConflict, "m);\n    return n")
	}
}

// A `ret` nested in an `if`, `for` or `while` body of a block expression took
// the entry loan in that body's scope, which expired before the binding held
// it -- for the receiver's own borrow of a local map too.
func TestEntryFromANestedRetIsHeld(t *testing.T) {
	const tail = `
    insm(m);
    compare o { Some(v) => { *v = heap("z"); } nothing => {} };
    return 0;
}
`
	const head = `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
`
	rows := []struct{ name, text, snippet string }{
		{"loop_then_if", head + `    let o = { let mut i = 0; while i < 3 { if i == 1 { ret m.get_mut(&k); } i = i + 1; } ret nothing; };` + tail, ""},
		{"if", head + `    let o = { if k.__len() > 0 { ret m.get_mut(&k); } ret nothing; };` + tail, ""},
		{"for", head + `    let xs: int[] = [1, 2];
    let o = { for x in xs { ret m.get_mut(&k); } ret nothing; };` + tail, ""},
		{"while_true", head + `    let o = { while true { ret m.get_mut(&k); } ret nothing; };` + tail, ""},
		{"async_fn", `async fn ga(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    let o = { if k.__len() > 0 { ret m.get_mut(&k); } ret nothing; };` + tail, ""},
		{"async_block", `async fn g() -> int {
    let t = async {
        let mut mm = Map::<string, string>.new();
        let k = heap("k");
        let r = &mut mm;
        let o = { if k.__len() > 0 { ret r.get_mut(&k); } ret nothing; };
        insm(r);
        compare o { Some(v) => { *v = heap("z"); } nothing => {} };
        ret 0;
    };
    compare t.await() { Success(n) => { return n; } _ => { return 1; } };
}
`, "r);\n        compare o"},
		{"owned_local_map", `fn g() -> int {
    let mut mm = Map::<string, string>.new();
    let k = heap("k");
    let o = { if k.__len() > 0 { ret mm.get_mut(&k); } ret nothing; };
    insm(&mut mm);
    compare o { Some(v) => { *v = heap("z"); } nothing => {} };
    return 0;
}
`, "&mut mm);"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			snippet := row.snippet
			if snippet == "" {
				snippet = "m);\n    compare o"
			}
			bytesViewRefusal(t, projectionPrelude+row.text, diag.SemaBorrowConflict, snippet)
		})
	}
}

// Named over-refusals of the lexical model, the owned form's too: an entry a
// block's own statement takes and nothing hands out still lives to the end
// of the statement around the block.
func TestEntryTakenInsideABlockOverRefusals(t *testing.T) {
	for _, row := range []struct{ name, block string }{
		{"let_in_block", `{ let t = m.get_mut(&k); ret 5; }`},
		{"compare_in_block", `{ compare m.get_mut(&k) { Some(v) => { *v = heap("z"); } nothing => {} }; ret 1; }`},
	} {
		t.Run(row.name, func(t *testing.T) {
			bytesViewRefusal(t, projectionPrelude+`fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    let n = `+row.block+`;
    insm(m);
    return n;
}
`, diag.SemaBorrowConflict, "m);\n    return n")
		})
	}
}

// A value leaving a block by a nested `ret` that can hold no loan -- a copy
// `ret *t`, a scalar a compare computes from an arm binding -- carries none
// out of the block, owned or through a reference. (The flat owned `{ ret
// *firstm(&mut a); }` is refused at the base already, by the receiver's own
// borrow; these rows do not lean on it.)
func TestPlainValueFromANestedRetHoldsNothing(t *testing.T) {
	for _, row := range []struct{ name, text string }{
		{"owned_copy_of_an_element", `fn g() -> int {
    let mut a: int[] = [1, 2, 3];
    let n = { if a.__len() > 0 { ret *firstm(&mut a); } ret 0; };
    app(&mut a);
    return n;
}
`},
		{"owned_copy_through_a_binding", `fn g() -> int {
    let mut a: int[] = [1, 2, 3];
    let n = { if a.__len() > 0 { let t = firstm(&mut a); ret *t; } ret 0; };
    app(&mut a);
    return n;
}
`},
		{"owned_copy_in_a_loop", `fn g() -> int {
    let mut a: int[] = [1, 2, 3];
    let n = { let mut i = 0; while i < 4 { i = i + 1; let t = firstm(&mut a); *t = i; if i == 3 { ret *t; } } ret 0; };
    app(&mut a);
    return n;
}
`},
		{"copy_through_a_reference", `fn g(r: &mut int[]) -> int {
    let n = { if r.__len() > 0 { let t = firstm(r); ret *t; } ret 0; };
    app(r);
    return n;
}
`},
		{"copy_through_a_reference_with_a_continue", `fn g(r: &mut int[]) -> int {
    let n = { let mut i = 0; while i < 4 { i = i + 1; let t = firstm(r); *t = i; if i == 2 { continue; } if i == 3 { ret *t; } } ret 0; };
    app(r);
    return n;
}
`},
		{"scalar_from_an_entry_in_a_loop", `fn g(m: &mut Map<string, string>) -> int {
    let k = heap("k");
    let n = { let mut i = 0; while i < 3 { if i == 1 { ret compare m.get_mut(&k) { Some(v) => 1; nothing => 0; }; } i = i + 1; } ret 0; };
    insm(m);
    return n;
}
`},
	} {
		t.Run(row.name, func(t *testing.T) {
			bytesViewAccepted(t, projectionPrelude+row.text)
		})
	}
}
