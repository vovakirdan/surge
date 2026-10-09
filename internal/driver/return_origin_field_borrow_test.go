package driver

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
)

// A dynamic array field of a plain struct read through a named reference is a
// borrow of the field's place, so it keeps exactly the reference's sources, like
// any other reference-free field. The loans the field's contents keep are not
// read at the projection: loading or passing the place on still asks
// containerLoans or the backing-call targets, which refuse a base they cannot
// prove. The same owner composes through a nested plain-struct field. A
// fixed-array or cursor field keeps the projection refusal: its view or
// cursor points into the referent itself. The callers that reallocate or
// overwrite the viewed storage while the view lives are refused by the checker,
// which keeps the argument borrowed (TestFieldBorrowWindowCallersAreRefused).
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

const fieldBorrowDigest = "f5fe4e728f37b0044611f0ca9b0d7213f3c48fa915043535da87ed4cbd85d05c"

const fieldBorrowDerefRefusal = "reference loaded through another reference needs content provenance"

// 1 parent + 10 leaves = 11 RUN.
func TestAnalyzeFieldBorrowOrigins(t *testing.T) {
	f, analysis := analyzeOriginDependency(t, "field_borrow", fieldBorrowSource, nil)
	checkOriginBodyLeaves(t, analysis, f, fieldBorrowSource, fieldBorrowDigest, []originBodyLeaf{
		// Controls: the borrow of a dynamic array field names the parameter's referent.
		{name: "read_items", body: "read_items", function: originSpan{216, 274, "fn read_items(b: &Bag) -> &int64[] {\n    return b.items;\n}"}, clean: true, slots: []uint32{0},
			cleared: []originRefusal{{originSpan{264, 271, "b.items"}, originProjectionRefusal}}},
		{name: "push_byte", body: "push_byte", function: originSpan{275, 365, "fn push_byte(h: &mut Bytes, x: byte) -> nothing {\n    h.buf.push(x);\n    return nothing;\n}"}, clean: true},
		{name: "count", body: "count", function: originSpan{366, 423, "fn count(h: &Bytes) -> uint {\n    return h.buf.__len();\n}"}, clean: true},
		// Witnesses: a fixed-array field's view and cursor keep the projection
		// refusal.
		{name: "fixed_view_field", body: "view_of_field", function: originSpan{424, 493, "fn view_of_field(w: &Win) -> uint64[] {\n    return w.cells[[0..2]];\n}"},
			stays: []originRefusal{{originSpan{475, 482, "w.cells"}, originProjectionRefusal}, {originSpan{450, 461, "-> uint64[]"}, originResultRefusal}}},
		{name: "fixed_cursor_field", body: "cursor_of_field", function: originSpan{494, 572, "fn cursor_of_field(w: &Win) -> Range<uint64> {\n    return w.cells.__range();\n}"},
			stays: []originRefusal{{originSpan{552, 559, "w.cells"}, originProjectionRefusal}, {originSpan{522, 538, "-> Range<uint64>"}, originResultRefusal}}},
		// Witnesses: an unproved reference, the loans of a container read through
		// the projection, and a store of an element that can hold loans into the
		// projected container keep their refusal. The nested plain-field place
		// composes the same parameter owner.
		{name: "unknown_reference", body: "through", function: originSpan{573, 653, "fn through(pp: &&Bag) -> &int64[] {\n    let p: &Bag = *pp;\n    return p.items;\n}"},
			stays: []originRefusal{{originSpan{627, 630, "*pp"}, fieldBorrowDerefRefusal}, {originSpan{595, 606, "-> &int64[]"}, originResultRefusal}}},
		{name: "nested_place", body: "nested", function: originSpan{654, 716, "fn nested(s: &Shelf) -> &uint64[] {\n    return s.rows.cells;\n}"}, clean: true, slots: []uint32{0},
			cleared: []originRefusal{{originSpan{701, 713, "s.rows.cells"}, originProjectionRefusal}}},
		{name: "dynamic_view_loans", body: "view_rows", function: originSpan{717, 783, "fn view_rows(w: &Rows) -> uint64[] {\n    return w.cells[[0..2]];\n}"},
			stays:   []originRefusal{{originSpan{765, 780, "w.cells[[0..2]]"}, arrayPopContainerLoanBase}, {originSpan{740, 751, "-> uint64[]"}, originResultRefusal}},
			cleared: []originRefusal{{originSpan{765, 772, "w.cells"}, originProjectionRefusal}}},
		{name: "store_through_mut", body: "keep_view", function: originSpan{784, 879, "fn keep_view(h: &mut Nest, v: uint64[]) -> nothing {\n    h.items.push(v);\n    return nothing;\n}"},
			stays: []originRefusal{{originSpan{841, 856, "h.items.push(v)"}, rangeNextLoanElement}}},
		{name: "store_local_view", body: "stash_local_view", function: originSpan{880, 1032, "fn stash_local_view(h: &mut Nest) -> nothing {\n    let a: uint64[3] = [1:uint64, 2:uint64, 3:uint64];\n    h.items.push(a[[0..2]]);\n    return nothing;\n}"},
			stays: []originRefusal{{originSpan{986, 1009, "h.items.push(a[[0..2]])"}, rangeNextLoanElement}, {originSpan{986, 1009, "h.items.push(a[[0..2]])"}, backingLoanDiscard}}},
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

// A caller that reallocates or overwrites the storage a fixed-array field's
// window reads, while the window lives, is refused by the checker: the call
// keeps its `&` argument borrowed for as long as the window. grow_after_view
// reallocates the element the view reads, overwrite_after_view writes the
// viewed storage.
const fieldBorrowWindowPrelude = `type Win = { cells: uint64[4] };

fn view_of_field(w: &Win) -> uint64[] {
    return w.cells[[0..2]];
}

`

func TestFieldBorrowWindowCallersAreRefused(t *testing.T) {
	rows := []struct {
		name, body, snippet string
		code                diag.Code
	}{
		{"grow_after_view", `fn grow_after_view() -> uint64 {
    let mut xs: Win[] = [];
    xs.push(Win { cells = [11:uint64, 22:uint64, 33:uint64, 44:uint64] });
    let v = view_of_field(&xs[0]);
    xs.push(Win { cells = [1:uint64, 2:uint64, 3:uint64, 4:uint64] });
    return clone(v[1]);
}
`, "xs.push(Win { cells = [1:", diag.SemaBorrowConflict},
		{"overwrite_after_view", `fn overwrite_after_view() -> uint64 {
    let mut w: Win = Win { cells = [11:uint64, 22:uint64, 33:uint64, 44:uint64] };
    let v = view_of_field(&w);
    w = Win { cells = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };
    return clone(v[1]);
}
`, "w = Win { cells = [1:", diag.SemaBorrowMutation},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			refOutwardRefusal(t, fieldBorrowWindowPrelude+row.body, row.code, row.snippet)
		})
	}
}
