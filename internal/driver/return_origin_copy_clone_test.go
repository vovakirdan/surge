package driver

import (
	"testing"

	"surge/internal/ast"
)

const originCopyCloneRefusal = "copy clone of a reference-bearing value needs its referent's contents"

// A direct clone of a Copy value is a copy of the argument's value: typing
// selects no __clone and HIR loads through the one reference. When the result
// holds no reference and is no loan carrier the copy is fresh, so the body is
// clean; a callable result keeps a named refusal, and a clone through two
// references keeps the ordinary call refusal.
const copyCloneSource = `fn copy_local() -> int32 {
    let x: int32 = 5;
    return clone(&x);
}
fn copy_element(xs: &byte[]) -> byte {
    return clone(xs[0]);
}
fn copy_callable(f: &fn(int64) -> int64) -> fn(int64) -> int64 {
    return clone(f);
}
fn copy_through_two(n: &&int64) -> int64 {
    return clone(n);
}
`

const copyCloneDigest = "5ecba5e0037b49b48052be4697b5422c897e340ccb9fbe010c5a45fd8c36334b"

func TestAnalyzeCopyCloneOrigins(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "copy_clone", copyCloneSource, false, func(f originalGenericFixture) {
		for _, call := range []originSpan{{60, 69, "clone(&x)"}, {123, 135, "clone(xs[0])"}, {215, 223, "clone(f)"}, {281, 289, "clone(n)"}} {
			checkOriginSource(t, copyCloneSource, copyCloneDigest, call)
			if _, selected := f.unit.Sema.CloneSymbols[originExprAt(t, f.unit, f.owner.File.ID, call, ast.ExprCall)]; selected {
				t.Fatalf("PRECONDITION: Copy clone %q gained a selection", call.snippet)
			}
		}
	})
	checkOriginBodyLeaves(t, analysis, f, copyCloneSource, copyCloneDigest, []originBodyLeaf{
		{name: "local_copy", body: "copy_local", function: originSpan{0, 72, "fn copy_local() -> int32 {\n    let x: int32 = 5;\n    return clone(&x);\n}"}, clean: true},
		{name: "element_copy", body: "copy_element", function: originSpan{73, 138, "fn copy_element(xs: &byte[]) -> byte {\n    return clone(xs[0]);\n}"}, clean: true},
		{name: "callable_copy_stays_refused", body: "copy_callable", function: originSpan{139, 226, "fn copy_callable(f: &fn(int64) -> int64) -> fn(int64) -> int64 {\n    return clone(f);\n}"},
			stays: []originRefusal{{originSpan{215, 223, "clone(f)"}, originCopyCloneRefusal}}},
		{name: "two_references_stay_refused", body: "copy_through_two", function: originSpan{227, 292, "fn copy_through_two(n: &&int64) -> int64 {\n    return clone(n);\n}"},
			stays: []originRefusal{{originSpan{281, 289, "clone(n)"}, originCallRefusal}}},
	})
}

// A user overload of clone is not a core clone: typing selects the user
// declaration for its own argument type, so the call is an ordinary call of a
// body that returns a fresh int64 and the function is clean.
const copyCloneOverloadSource = `@overload
fn clone(value: &int64) -> int64 {
    return 42;
}
fn copy_number(n: &int64) -> int64 {
    return clone(n);
}
`

func TestAnalyzeCopyCloneUserOverloadIsAnOrdinaryCall(t *testing.T) {
	const digest = "473e4f3a37b5c698c51dda35954d6652285871157f114c1e1aa6260e69084d96"
	f, analysis := analyzeOriginRoot(t, "copy_clone_overload", copyCloneOverloadSource, false, nil)
	checkOriginBodyLeaves(t, analysis, f, copyCloneOverloadSource, digest, []originBodyLeaf{
		{name: "user_overload_is_its_own_call", body: "copy_number", function: originSpan{62, 121, "fn copy_number(n: &int64) -> int64 {\n    return clone(n);\n}"},
			clean: true},
	})
}

const copyCloneAddressSource = `fn keep_copy(n: &int64) -> &int64 {
    let saved: int64 = clone(n);
    return &saved;
}
`

const copyCloneArgumentSource = `fn probe(xs: int64[], outside: &int64) -> int64 {
    let mut saved: &int64 = outside;
    let copied: int64 = clone(xs[{ let owned: int64 = 1; saved = &owned; ret 0; }]);
    return 1;
}
`

// Answering a Copy clone keeps every escape: the address of its fresh result,
// and a borrow made while evaluating its argument.
func TestAnalyzeCopyCloneEscapes(t *testing.T) {
	for _, tc := range []struct {
		name, text, digest, owner string
		escape, call              originSpan
	}{
		{"local_address_escape", copyCloneAddressSource, "ed95f8579d6321de2eae35dd49916cf148645cfcd48c1531642ca22694e732bc", "saved",
			originSpan{73, 87, "return &saved;"}, originSpan{59, 67, "clone(n)"}},
		{"argument_effect_escape", copyCloneArgumentSource, "c16faf9a483a73631d144901ed29b32fb19afdfcfae3f469f87154ef00e4d26b", "owned",
			originSpan{160, 166, "ret 0;"}, originSpan{111, 170, "clone(xs[{ let owned: int64 = 1; saved = &owned; ret 0; }])"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkOriginSource(t, tc.text, tc.digest, tc.escape, tc.call)
			f, analysis := analyzeOriginRoot(t, "copy_clone_escape_"+tc.name, tc.text, true, nil)
			requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID, tc.escape, tc.owner)
			if originPendingAt(analysis, f.unit.SourceKey, tc.call, "") {
				t.Errorf("answered Copy clone %q still refuses", tc.call.snippet)
			}
		})
	}
}
