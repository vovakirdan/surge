package driver

import (
	"testing"

	"surge/internal/sema"
)

// A dynamic array field of a plain struct read through a named reference is a
// borrow of the field's place, so it keeps exactly the reference's sources, like
// any other reference-free field. The loans the field's contents keep are not
// read at the projection: loading or passing the place on still asks
// containerLoans or the backing-call targets, which refuse a base they cannot
// prove. A fixed-array or cursor field keeps the projection refusal: its view or
// cursor points into the referent itself, and the checker does not keep the
// caller's argument borrowed while that result lives (grow_after_view reallocates
// the element the view reads, overwrite_after_view writes the viewed storage).
const fieldBorrowSource = `pragma module::dep;
type Bag = { items: int64[] };
type Win = { cells: uint64[4] };
type Rows = { cells: uint64[] };
type Shelf = { rows: Rows };
type Bytes = { buf: byte[] };
type Nest = { items: Array<uint64[]> };
fn read_items(b: &Bag) -> &int64[] {
    return b.items;
}
fn push_byte(h: &mut Bytes, x: byte) -> nothing {
    h.buf.push(x);
    return nothing;
}
fn count(h: &Bytes) -> uint {
    return h.buf.__len();
}
fn view_of_field(w: &Win) -> uint64[] {
    return w.cells[[0..2]];
}
fn cursor_of_field(w: &Win) -> Range<uint64> {
    return w.cells.__range();
}
fn grow_after_view() -> uint64 {
    let mut xs: Win[] = [];
    xs.push(Win { cells = [11:uint64, 22:uint64, 33:uint64, 44:uint64] });
    let v = view_of_field(&xs[0]);
    xs.push(Win { cells = [1:uint64, 2:uint64, 3:uint64, 4:uint64] });
    return clone(v[1]);
}
fn overwrite_after_view() -> uint64 {
    let mut w: Win = Win { cells = [11:uint64, 22:uint64, 33:uint64, 44:uint64] };
    let v = view_of_field(&w);
    w = Win { cells = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };
    return clone(v[1]);
}
fn through(pp: &&Bag) -> &int64[] {
    let p: &Bag = *pp;
    return p.items;
}
fn nested(s: &Shelf) -> &uint64[] {
    return s.rows.cells;
}
fn view_rows(w: &Rows) -> uint64[] {
    return w.cells[[0..2]];
}
fn keep_view(h: &mut Nest, v: uint64[]) -> nothing {
    h.items.push(v);
    return nothing;
}
fn stash_local_view(h: &mut Nest) -> nothing {
    let a: uint64[3] = [1:uint64, 2:uint64, 3:uint64];
    h.items.push(a[[0..2]]);
    return nothing;
}
`

const fieldBorrowDigest = "e3e6c6b61edfff00d0a4dca9a63b767835b74fcd878c3dbff9c1a13450d0bc35"

const fieldBorrowDerefRefusal = "reference loaded through another reference needs content provenance"

const fieldBorrowCalleeRefusal = "callee returned an unproved source"

// 1 parent + 12 leaves = 13 RUN.
func TestAnalyzeFieldBorrowOrigins(t *testing.T) {
	f, analysis := analyzeOriginDependency(t, "field_borrow", fieldBorrowSource, nil)
	checkOriginBodyLeaves(t, analysis, f, fieldBorrowSource, fieldBorrowDigest, []originBodyLeaf{
		// Controls: the borrow of a dynamic array field names the parameter's referent.
		{name: "read_items", body: "read_items", function: originSpan{216, 274, "fn read_items(b: &Bag) -> &int64[] {\n    return b.items;\n}"}, clean: true, slots: []uint32{0},
			cleared: []originRefusal{{originSpan{264, 271, "b.items"}, originProjectionRefusal}}},
		{name: "push_byte", body: "push_byte", function: originSpan{275, 365, "fn push_byte(h: &mut Bytes, x: byte) -> nothing {\n    h.buf.push(x);\n    return nothing;\n}"}, clean: true},
		{name: "count", body: "count", function: originSpan{366, 423, "fn count(h: &Bytes) -> uint {\n    return h.buf.__len();\n}"}, clean: true},
		// Witnesses: a fixed-array field's view and cursor keep the projection
		// refusal, so the callers that outlive or overwrite the viewed storage
		// never finish on a proven source.
		{name: "fixed_view_field", body: "view_of_field", function: originSpan{424, 493, "fn view_of_field(w: &Win) -> uint64[] {\n    return w.cells[[0..2]];\n}"},
			stays: []originRefusal{{originSpan{475, 482, "w.cells"}, originProjectionRefusal}, {originSpan{450, 461, "-> uint64[]"}, originResultRefusal}}},
		{name: "fixed_cursor_field", body: "cursor_of_field", function: originSpan{494, 572, "fn cursor_of_field(w: &Win) -> Range<uint64> {\n    return w.cells.__range();\n}"},
			stays: []originRefusal{{originSpan{552, 559, "w.cells"}, originProjectionRefusal}, {originSpan{522, 538, "-> Range<uint64>"}, originResultRefusal}}},
		{name: "grow_after_view", body: "grow_after_view", function: originSpan{573, 840, "fn grow_after_view() -> uint64 {\n    let mut xs: Win[] = [];\n    xs.push(Win { cells = [11:uint64, 22:uint64, 33:uint64, 44:uint64] });\n    let v = view_of_field(&xs[0]);\n    xs.push(Win { cells = [1:uint64, 2:uint64, 3:uint64, 4:uint64] });\n    return clone(v[1]);\n}"},
			stays: []originRefusal{{originSpan{721, 742, "view_of_field(&xs[0])"}, fieldBorrowCalleeRefusal}}},
		{name: "overwrite_after_view", body: "overwrite_after_view", function: originSpan{841, 1084, "fn overwrite_after_view() -> uint64 {\n    let mut w: Win = Win { cells = [11:uint64, 22:uint64, 33:uint64, 44:uint64] };\n    let v = view_of_field(&w);\n    w = Win { cells = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };\n    return clone(v[1]);\n}"},
			stays: []originRefusal{{originSpan{974, 991, "view_of_field(&w)"}, fieldBorrowCalleeRefusal}}},
		// Witnesses: an unproved reference, a nested place, the loans of a
		// container read through the projection, and a store of an element that
		// can hold loans into the projected container all keep their refusal.
		{name: "unknown_reference", body: "through", function: originSpan{1085, 1165, "fn through(pp: &&Bag) -> &int64[] {\n    let p: &Bag = *pp;\n    return p.items;\n}"},
			stays: []originRefusal{{originSpan{1139, 1142, "*pp"}, fieldBorrowDerefRefusal}, {originSpan{1107, 1118, "-> &int64[]"}, originResultRefusal}}},
		{name: "nested_place", body: "nested", function: originSpan{1166, 1228, "fn nested(s: &Shelf) -> &uint64[] {\n    return s.rows.cells;\n}"},
			stays: []originRefusal{{originSpan{1213, 1225, "s.rows.cells"}, originProjectionRefusal}, {originSpan{1187, 1199, "-> &uint64[]"}, originResultRefusal}}},
		{name: "dynamic_view_loans", body: "view_rows", function: originSpan{1229, 1295, "fn view_rows(w: &Rows) -> uint64[] {\n    return w.cells[[0..2]];\n}"},
			stays:   []originRefusal{{originSpan{1277, 1292, "w.cells[[0..2]]"}, arrayPopContainerLoanBase}, {originSpan{1252, 1263, "-> uint64[]"}, originResultRefusal}},
			cleared: []originRefusal{{originSpan{1277, 1284, "w.cells"}, originProjectionRefusal}}},
		{name: "store_through_mut", body: "keep_view", function: originSpan{1296, 1391, "fn keep_view(h: &mut Nest, v: uint64[]) -> nothing {\n    h.items.push(v);\n    return nothing;\n}"},
			stays: []originRefusal{{originSpan{1353, 1368, "h.items.push(v)"}, rangeNextLoanElement}}},
		{name: "store_local_view", body: "stash_local_view", function: originSpan{1392, 1544, "fn stash_local_view(h: &mut Nest) -> nothing {\n    let a: uint64[3] = [1:uint64, 2:uint64, 3:uint64];\n    h.items.push(a[[0..2]]);\n    return nothing;\n}"},
			stays: []originRefusal{{originSpan{1498, 1521, "h.items.push(a[[0..2]])"}, rangeNextLoanElement}, {originSpan{1498, 1521, "h.items.push(a[[0..2]])"}, backingLoanDiscard}}},
	})
}

// The same container projection through a local reference reports its owner's
// escape instead of the projection refusal.
const fieldBorrowEscapeSource = `pragma no_std;
type Bag = { items: int64[] };
fn leak_items() -> &int64[] {
    let owned: Bag = Bag { items = [1, 2] };
    let view: &Bag = &owned;
    return view.items;
}
`

func TestAnalyzeFieldBorrowEscape(t *testing.T) {
	escape := originSpan{154, 172, "return view.items;"}
	result := originSpan{62, 73, "-> &int64[]"}
	checkOriginSource(t, fieldBorrowEscapeSource, "d9cc6904a2378b84e02f491d81228c894a84c1f434ac45ed292f5573829f1f65", escape, result)
	f := originalGenericSignatureFixture(t, fieldBorrowEscapeSource, true, false)
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	local := originPendingWithin(analysis, f.unit.SourceKey, 0, len(fieldBorrowEscapeSource))
	logReturnOriginCallEvidence(t, map[string]any{"stage": "field_borrow_escape", "pending": local, "diagnostics": analysis.Diagnostics})
	requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID, escape, "owned")
	if len(local) != 0 {
		t.Errorf("escaped container projection pending = %+v, want none: the SEM3139 refusal completes the result at %q", local, result.snippet)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, "leak_items", true, nil)
}
