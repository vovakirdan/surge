package mono

import (
	"testing"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Ownership metadata has to survive cloning and substitution, and the reason it
// needs its own test rather than a program is that most of it is INVISIBLE from
// the surface. A drop plan that arrives empty at a backend emits a whole-value
// drop and prints the right answer; a drop type that arrives unsubstituted
// selects the wrong glue. Neither shows up as a diagnostic, and reaching some of
// these shapes from source needs a generic crossing body holding a
// type-parameter-typed value — a shape the language may not even allow today.
//
// So these assert the two operations directly. Every payload that carries
// ownership information gets one row, and a row fails if someone adds a field
// to it and forgets these functions.

// clonedFieldRead keeps its transfer mode. A field read that TAKES its value and
// one that duplicates it are the same shape, so a clone that loses the flag
// turns a move into a second holder — a leak, silently.
func TestCloneKeepsFieldReadTransferMode(t *testing.T) {
	original := &hir.Expr{
		Kind: hir.ExprFieldAccess,
		Type: types.TypeID(7),
		Data: hir.FieldAccessData{
			Object:    &hir.Expr{Kind: hir.ExprVarRef, Data: hir.VarRefData{}},
			FieldName: "inner",
			FieldIdx:  -1,
			MoveOut:   true,
		},
	}
	cloned := cloneExpr(original)
	data, ok := cloned.Data.(hir.FieldAccessData)
	if !ok {
		t.Fatalf("clone changed the payload type: %T", cloned.Data)
	}
	if !data.MoveOut {
		t.Fatal("clone lost the move-out mode: the copy duplicates a value the original took")
	}
}

// A substituted field read keeps it too, for the same reason.
func TestSubstKeepsFieldReadTransferMode(t *testing.T) {
	expr := &hir.Expr{
		Kind: hir.ExprFieldAccess,
		Type: types.TypeID(7),
		Data: hir.FieldAccessData{
			Object:    &hir.Expr{Kind: hir.ExprVarRef, Data: hir.VarRefData{}},
			FieldName: "inner",
			FieldIdx:  -1,
			MoveOut:   true,
		},
	}
	s := &Subst{}
	if err := s.ApplyExpr(expr); err != nil {
		t.Fatalf("ApplyExpr: %v", err)
	}
	data, ok := expr.Data.(hir.FieldAccessData)
	if !ok {
		t.Fatalf("substitution changed the payload type: %T", expr.Data)
	}
	if !data.MoveOut {
		t.Fatal("substitution lost the move-out mode")
	}
}

// A statement-end temporary carries the plan that narrows its release to what is
// LEFT of it after fields were taken out. An empty plan releases the whole
// value, which frees storage the taker now owns.
func TestCloneAndSubstKeepTemporaryReleasePlan(t *testing.T) {
	plan := []sema.DropStep{{Path: []sema.PlaceSegment{{Kind: sema.PlaceSegmentField, Name: source.StringID(3)}}}}
	build := func() *hir.Expr {
		return &hir.Expr{
			Kind: hir.ExprOwnedTemp,
			Type: types.TypeID(11),
			Data: hir.OwnedTempData{
				Inner: &hir.Expr{Kind: hir.ExprVarRef, Data: hir.VarRefData{}},
				Steps: plan,
			},
		}
	}

	cloned := cloneExpr(build())
	clonedData, ok := cloned.Data.(hir.OwnedTempData)
	if !ok {
		t.Fatalf("clone changed the payload type: %T", cloned.Data)
	}
	if len(clonedData.Steps) != len(plan) {
		t.Fatalf("clone lost the temporary's release plan: got %d steps, want %d", len(clonedData.Steps), len(plan))
	}

	substituted := build()
	s := &Subst{}
	if err := s.ApplyExpr(substituted); err != nil {
		t.Fatalf("ApplyExpr: %v", err)
	}
	substData, ok := substituted.Data.(hir.OwnedTempData)
	if !ok {
		t.Fatalf("substitution changed the payload type: %T", substituted.Data)
	}
	if len(substData.Steps) != len(plan) {
		t.Fatalf("substitution lost the temporary's release plan: got %d steps, want %d", len(substData.Steps), len(plan))
	}
}

// An explicit drop carries the same kind of plan, and it reaches a backend that
// will otherwise drop the whole binding over a place that has already gone.
func TestCloneAndSubstKeepExplicitDropPlan(t *testing.T) {
	plan := []sema.DropStep{
		{Path: []sema.PlaceSegment{{Kind: sema.PlaceSegmentField, Name: source.StringID(4)}}},
		{Path: nil, Shallow: true},
	}
	build := func() hir.Stmt {
		return hir.Stmt{
			Kind: hir.StmtDrop,
			Data: hir.DropData{
				Value: &hir.Expr{Kind: hir.ExprVarRef, Data: hir.VarRefData{}},
				Steps: plan,
			},
		}
	}

	cloned := cloneStmt(build())
	clonedData, ok := cloned.Data.(hir.DropData)
	if !ok {
		t.Fatalf("clone changed the payload type: %T", cloned.Data)
	}
	if len(clonedData.Steps) != len(plan) {
		t.Fatalf("clone lost the drop plan: got %d steps, want %d", len(clonedData.Steps), len(plan))
	}

	substituted := build()
	s := &Subst{}
	if err := s.ApplyStmt(&substituted); err != nil {
		t.Fatalf("ApplyStmt: %v", err)
	}
	substData, ok := substituted.Data.(hir.DropData)
	if !ok {
		t.Fatalf("substitution changed the payload type: %T", substituted.Data)
	}
	if len(substData.Steps) != len(plan) {
		t.Fatalf("substitution lost the drop plan: got %d steps, want %d", len(substData.Steps), len(plan))
	}
}

// An exit's drop list carries both a TYPE, which picks the drop glue, and a
// PLAN, which narrows what the drop reclaims. `return` and `ret` are separate
// statement kinds with separate handling, and a crossing body reaches its exit
// through `ret` and nothing else — so a gap on that side abandons everything
// such a body still holds, in exactly the case a program is hardest to write.
func TestCloneAndSubstKeepExitDropListsOnBothExitKinds(t *testing.T) {
	typeParam := types.TypeID(101)
	concrete := types.TypeID(202)
	plan := []sema.DropStep{{Path: []sema.PlaceSegment{{Kind: sema.PlaceSegmentField, Name: source.StringID(5)}}}}
	drops := []hir.DropLocal{{
		SymbolID: symbols.SymbolID(9),
		Type:     typeParam,
		Steps:    plan,
	}}

	cases := []struct {
		name  string
		build func() hir.Stmt
		read  func(hir.Stmt) ([]hir.DropLocal, bool)
	}{
		{
			name: "return",
			build: func() hir.Stmt {
				return hir.Stmt{Kind: hir.StmtReturn, Data: hir.ReturnData{DropsAfterValue: drops}}
			},
			read: func(st hir.Stmt) ([]hir.DropLocal, bool) {
				data, ok := st.Data.(hir.ReturnData)
				return data.DropsAfterValue, ok
			},
		},
		{
			name: "ret",
			build: func() hir.Stmt {
				return hir.Stmt{Kind: hir.StmtRet, Data: hir.RetData{DropsAfterValue: drops}}
			},
			read: func(st hir.Stmt) ([]hir.DropLocal, bool) {
				data, ok := st.Data.(hir.RetData)
				return data.DropsAfterValue, ok
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cloned := cloneStmt(tc.build())
			clonedDrops, ok := tc.read(cloned)
			if !ok {
				t.Fatalf("clone changed the payload type: %T", cloned.Data)
			}
			if len(clonedDrops) != 1 {
				t.Fatalf("clone lost the exit drop list: got %d entries, want 1", len(clonedDrops))
			}
			if len(clonedDrops[0].Steps) != len(plan) {
				t.Fatalf("clone lost the residual plan on an exit drop")
			}
			// The clone must own its slice: two instantiations share the
			// original otherwise, and the substitution below writes into it.
			if len(drops) > 0 && &clonedDrops[0] == &drops[0] {
				t.Fatal("clone aliased the exit drop list, so substituting one instantiation rewrites another's")
			}

			substituted := tc.build()
			// A pre-seeded cache stands in for a real instantiation: what is
			// under test is whether the exit drop's type is asked about at all,
			// not how the answer is computed.
			s := &Subst{Types: types.NewInterner()}
			s.cache = map[types.TypeID]types.TypeID{typeParam: concrete}
			if err := s.ApplyStmt(&substituted); err != nil {
				t.Fatalf("ApplyStmt: %v", err)
			}
			substDrops, ok := tc.read(substituted)
			if !ok {
				t.Fatalf("substitution changed the payload type: %T", substituted.Data)
			}
			if len(substDrops) != 1 {
				t.Fatalf("substitution lost the exit drop list: got %d entries, want 1", len(substDrops))
			}
			if substDrops[0].Type != concrete {
				t.Fatalf("substitution left an exit drop's type unsubstituted: got %v, want %v — the backend picks drop glue from it",
					substDrops[0].Type, concrete)
			}
			if len(substDrops[0].Steps) != len(plan) {
				t.Fatalf("substitution lost the residual plan on an exit drop")
			}
		})
	}
}
