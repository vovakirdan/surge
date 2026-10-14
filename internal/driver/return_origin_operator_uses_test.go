package driver

import (
	"testing"

	"surge/internal/ast"
)

// A generic magic method SEMA selects for a binary operator is finalized at the
// operator expression, not at a call. The use is that typed operation when the
// expression's own selection is the finalized callee; core's `Array<T> + Array<T>`
// then answers it by identity for a borrow-free instance. Reference-bearing
// array elements are rejected earlier by the aggregate type rule; a user
// operator on the same receiver keeps a refusal.
const operatorUseSource = `fn joined(a: uint[], b: uint[]) -> uint[] {
    return a + b;
}
fn join_all<T>(a: T[], b: T[]) -> T[] {
    return a + b;
}
fn joined_generic(a: uint[], b: uint[]) -> uint[] {
    return join_all::<uint>(a, b);
}
extern<Array<T>> {
    fn __sub(self: &Array<T>, other: &Array<T>) -> Array<T> {
        let out: Array<T> = [];
        return out;
    }
}
fn removed(a: uint[], b: uint[]) -> uint[] {
    return a - b;
}
`

const operatorUseDigest = "b3ccf8729d3d731e5521a40884ebbd4aeda8bfc1ebec978f3d45b786fb12aa65"

const (
	operatorUseOther     = "generic use disagrees with its original typed operation"
	operatorUseTransfer  = "generic opaque use requires its type-dependent effect transfer"
	operatorUseCallable  = "binary callable needs an exact origin contract"
	operatorUseJoinedFn  = "fn joined(a: uint[], b: uint[]) -> uint[] {\n    return a + b;\n}"
	operatorUseRemovedFn = "fn removed(a: uint[], b: uint[]) -> uint[] {\n    return a - b;\n}"
)

var (
	operatorUseJoined  = originSpan{55, 60, "a + b"}
	operatorUseRemoved = originSpan{410, 415, "a - b"}
)

func TestAnalyzeOperatorGenericUses(t *testing.T) {
	checkOriginSource(t, operatorUseSource, operatorUseDigest)
	f, analysis := analyzeOriginRoot(t, "operator_uses", operatorUseSource, false, nil)
	checkOriginBodyLeaves(t, analysis, f, operatorUseSource, operatorUseDigest, []originBodyLeaf{
		// The whole body finishes, and its result names no source: the concatenation
		// is a fresh array of copied scalars.
		{name: "concrete_concat", body: "joined", clean: true,
			function: originSpan{0, 63, operatorUseJoinedFn},
			cleared:  []originRefusal{{operatorUseJoined, operatorUseOther}}},
		// A template caller's edge use is answered for its concrete binding; the
		// template body keeps its own refusal of `Array<T> + Array<T>`.
		{name: "template_caller_edge", body: "join_all",
			function: originSpan{64, 123, "fn join_all<T>(a: T[], b: T[]) -> T[] {\n    return a + b;\n}"},
			stays:    []originRefusal{{originSpan{115, 120, "a + b"}, operatorUseCallable}},
			cleared:  []originRefusal{{originSpan{115, 120, "a + b"}, operatorUseOther}}},
		// A user operator declared on the same receiver is not the core intrinsic.
		{name: "user_operator", body: "removed",
			function: originSpan{354, 418, operatorUseRemovedFn},
			stays: []originRefusal{
				{operatorUseRemoved, operatorUseCallable},
				{operatorUseRemoved, operatorUseOther},
			}},
	})
}

// Each counterfactual edits only the selection SEMA recorded on `joined`'s
// operator, and the finalized use there must fall back to exactly its old row.
func TestAnalyzeOperatorGenericUseCounterfactuals(t *testing.T) {
	checkOriginSource(t, operatorUseSource, operatorUseDigest, operatorUseJoined, operatorUseRemoved)
	for _, leaf := range []struct {
		name string
		edit func(t *testing.T, f originalGenericFixture, joined ast.ExprID)
	}{
		{name: "selection_missing", edit: func(t *testing.T, f originalGenericFixture, joined ast.ExprID) {
			delete(f.unit.Sema.MagicBinarySymbols, joined)
		}},
		{name: "selection_names_another_declaration", edit: func(t *testing.T, f originalGenericFixture, joined ast.ExprID) {
			removed := originExprAt(t, f.unit, f.owner.File.ID, operatorUseRemoved, ast.ExprBinary)
			other, present := f.unit.Sema.MagicBinarySymbols[removed]
			if !present || other == f.unit.Sema.MagicBinarySymbols[joined] {
				t.Fatalf("PRECONDITION: the user operator has no distinct selection")
			}
			f.unit.Sema.MagicBinarySymbols[joined] = other
		}},
	} {
		t.Run(leaf.name, func(t *testing.T) {
			f, analysis := analyzeOriginRoot(t, "operator_uses_"+leaf.name, operatorUseSource, false, func(f originalGenericFixture) {
				leaf.edit(t, f, originExprAt(t, f.unit, f.owner.File.ID, operatorUseJoined, ast.ExprBinary))
			})
			got := originPendingWithin(analysis, f.unit.SourceKey, 0, 63)
			if !originPendingAt(analysis, f.unit.SourceKey, operatorUseJoined, operatorUseOther) {
				t.Errorf("%q did not fall back to %q: %+v", operatorUseJoined.snippet, operatorUseOther, got)
			}
			// Without a selection the body still takes its borrow-free path, so the
			// restored use row is the only row of the body.
			if leaf.name == "selection_missing" && len(got) != 1 {
				t.Errorf("joined carries %+v, want only %q", got, operatorUseOther)
			}
		})
	}
}
