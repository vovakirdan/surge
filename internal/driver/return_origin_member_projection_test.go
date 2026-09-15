package driver

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// A field of a plain struct read through a named reference is a borrow of a
// sub-place of the referent, so it keeps exactly the reference's sources. A
// nested place, an attributed struct and a container field keep their refusal.
// Only shared references appear: returning a `&mut` field is not admitted.
const memberProjectionSource = `pragma module::dep;
type Note = { text: string };
fn read_text(n: &Note) -> &string {
    return n.text;
}
fn copy_text(n: &Note) -> string {
    return n.text.__clone();
}
type Shelf = { note: Note };
fn read_nested(s: &Shelf) -> &string {
    return s.note.text;
}
@nosend type Sealed = { text: string };
fn read_sealed(s: &Sealed) -> &string {
    return s.text;
}
type Bag = { items: int64[] };
fn read_items(b: &Bag) -> &int64[] {
    return b.items;
}
`

const memberProjectionDigest = "80b5ed0ae6d3904edb07626a82ead71415519ff424784d005ddc9272222028b5"

// checkMemberProjectionTypes proves each member is typed as a borrow of its
// declared field, read through a reference to a struct.
func checkMemberProjectionTypes(t *testing.T, unit sema.ReturnOriginUnit, file source.FileID, members []originSpan) {
	t.Helper()
	in := unit.Sema.TypeInterner
	for _, span := range members {
		id := originExprAt(t, unit, file, span, ast.ExprMember)
		data, ok := unit.Builder.Exprs.Member(id)
		result, typed := in.Lookup(unit.Sema.ExprTypes[id])
		if !ok || data == nil || !typed || result.Kind != types.KindReference {
			t.Fatalf("PRECONDITION: %q is not typed as a borrow", span.snippet)
		}
		target, targetTyped := in.Lookup(unit.Sema.ExprTypes[data.Target])
		if !targetTyped || target.Kind != types.KindReference {
			t.Fatalf("PRECONDITION: %q does not read through a reference", span.snippet)
		}
		info, found := in.StructInfo(target.Elem)
		if !found || info == nil {
			t.Fatalf("PRECONDITION: %q does not read a struct", span.snippet)
		}
		matched := false
		for _, field := range info.Fields {
			matched = matched || field.Name == data.Field && field.Type == result.Elem
		}
		if !matched {
			t.Fatalf("PRECONDITION: %q is not a borrow of its declared struct field", span.snippet)
		}
	}
}

func TestAnalyzeMemberProjectionOrigins(t *testing.T) {
	members := []originSpan{{97, 103, "n.text"}, {153, 159, "n.text"}, {252, 258, "s.note"}, {252, 263, "s.note.text"},
		{358, 364, "s.text"}, {447, 454, "b.items"}}
	checkOriginSource(t, memberProjectionSource, memberProjectionDigest, members...)
	f, analysis := analyzeOriginDependency(t, "member_projection", memberProjectionSource, func(f originalGenericFixture) {
		checkMemberProjectionTypes(t, f.unit, f.owner.File.ID, members)
	})
	checkOriginBodyLeaves(t, analysis, f, memberProjectionSource, memberProjectionDigest, []originBodyLeaf{
		{name: "read_text", body: "read_text", function: originSpan{50, 106, "fn read_text(n: &Note) -> &string {\n    return n.text;\n}"}, clean: true, slots: []uint32{0}},
		{name: "copy_text", body: "copy_text", function: originSpan{107, 172, "fn copy_text(n: &Note) -> string {\n    return n.text.__clone();\n}"}, clean: true},
		{name: "nested_control", body: "read_nested", function: originSpan{202, 266, "fn read_nested(s: &Shelf) -> &string {\n    return s.note.text;\n}"}, stays: []originRefusal{{originSpan{252, 263, "s.note.text"}, originProjectionRefusal}, {originSpan{245, 264, "return s.note.text;"}, originOutgoingRefusal}, {originSpan{228, 238, "-> &string"}, originResultRefusal}}, cleared: []originRefusal{{originSpan{252, 258, "s.note"}, originProjectionRefusal}}},
		{name: "sealed_control", body: "read_sealed", function: originSpan{307, 367, "fn read_sealed(s: &Sealed) -> &string {\n    return s.text;\n}"}, stays: []originRefusal{{originSpan{358, 364, "s.text"}, originProjectionRefusal}}},
		{name: "loan_carrier_control", body: "read_items", function: originSpan{399, 458, "fn read_items(b: &Bag) -> &int64[] {\n    return b.items;\n}\n"}, stays: []originRefusal{{originSpan{447, 454, "b.items"}, originProjectionRefusal}}},
	})
}

// The same projection through a local reference reports its owner's escape.
const memberProjectionEscapeSource = `pragma no_std;
type Note = { text: string };
fn leak_text() -> &string {
    let owned: Note = Note { text = "local" };
    let view: &Note = &owned;
    return view.text;
}
`

func TestAnalyzeMemberProjectionEscape(t *testing.T) {
	escape := originSpan{154, 171, "return view.text;"}
	result := originSpan{60, 70, "-> &string"}
	checkOriginSource(t, memberProjectionEscapeSource, "7fe10bac321c3088d9387493874b3dba67b7f30f7ae6b8c6187aef8864c1b1f4", escape, result)
	f := originalGenericSignatureFixture(t, memberProjectionEscapeSource, true, false)
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	local := originPendingWithin(analysis, f.unit.SourceKey, 0, len(memberProjectionEscapeSource))
	logReturnOriginCallEvidence(t, map[string]any{"stage": "member_projection_escape", "pending": local, "diagnostics": analysis.Diagnostics})
	requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID, escape, "owned")
	if len(local) != 0 {
		t.Errorf("escaped projection pending = %+v, want none: the SEM3139 refusal completes the result at %q", local, result.snippet)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, "leak_text", true, nil)
}
