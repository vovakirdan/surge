package mono

import (
	"testing"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestMonoTraversalsCoverSelectRaceAndRemoteSendPayload(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	param := in.RegisterTypeParam(in.Strings.Intern("T"), 1, 0, false, types.NoTypeID)
	concrete := in.Builtins().Int

	for _, kind := range []hir.ExprKind{hir.ExprSelect, hir.ExprRace} {
		t.Run(kind.String(), func(t *testing.T) {
			original := monoTraversalSelectExpr(kind, param)
			cloned := cloneExpr(original)
			originalData := original.Data.(hir.SelectData)
			clonedData := cloned.Data.(hir.SelectData)
			if clonedData.Crossing == originalData.Crossing || clonedData.Arms[0].Await == originalData.Arms[0].Await ||
				clonedData.Crossing.RemoteOps[0].Value == originalData.Crossing.RemoteOps[0].Value {
				t.Fatalf("clone aliases select/race child storage")
			}

			subst := &Subst{Types: in, ExactArgs: map[types.TypeID]types.TypeID{param: concrete}}
			if err := subst.ApplyExpr(cloned); err != nil {
				t.Fatalf("substitute select/race: %v", err)
			}
			clonedData = cloned.Data.(hir.SelectData)
			remote := clonedData.Crossing.RemoteOps[0]
			if cloned.Type != concrete || clonedData.Arms[0].Await.Type != concrete ||
				remote.ReceiverType != concrete || remote.Value.Type != concrete || clonedData.Crossing.PayloadType != concrete {
				t.Fatalf("generic select/race child escaped substitution: %+v", clonedData)
			}

			callCount := 0
			if err := rewriteCallsInExpr(original, func(_ *hir.Expr, data *hir.CallData) error {
				callCount++
				data.SymbolID += 100
				return nil
			}); err != nil {
				t.Fatalf("rewrite select/race calls: %v", err)
			}
			if callCount != 2 {
				t.Fatalf("rewritten calls = %d, want arm and remote payload", callCount)
			}

			varRefCount := 0
			if err := rewriteVarRefsInExpr(original, func(_ *hir.Expr, data *hir.VarRefData) error {
				varRefCount++
				data.SymbolID += 1000
				return nil
			}); err != nil {
				t.Fatalf("rewrite select/race var refs: %v", err)
			}
			if varRefCount != 6 {
				t.Fatalf("rewritten var refs = %d, want all arm/receiver/payload refs", varRefCount)
			}

			seenTypes := make(map[types.TypeID]bool)
			collectTypesFromExpr(original, func(id types.TypeID) { seenTypes[id] = true })
			if !seenTypes[param] {
				t.Fatalf("type collector missed generic select/race children")
			}

			fn := &hir.Func{Params: []hir.Param{{Default: &hir.Expr{
				Kind: hir.ExprOwnedTemp,
				Data: hir.OwnedTempData{Inner: original},
			}}}}
			seenSymbols := make(map[symbols.SymbolID]bool)
			for _, id := range collectFuncCallSyms(fn) {
				seenSymbols[id] = true
			}
			for _, id := range []symbols.SymbolID{111, 132, 1033} {
				if !seenSymbols[id] {
					t.Fatalf("DCE missed symbol %d in default/select/remote payload: %v", id, seenSymbols)
				}
			}
		})
	}
}

func monoTraversalSelectExpr(kind hir.ExprKind, ty types.TypeID) *hir.Expr {
	ref := func(id symbols.SymbolID) *hir.Expr {
		return &hir.Expr{Kind: hir.ExprVarRef, Type: ty, Data: hir.VarRefData{Name: "value", SymbolID: id}}
	}
	call := func(id symbols.SymbolID, arg symbols.SymbolID) *hir.Expr {
		return &hir.Expr{
			Kind: hir.ExprCall,
			Type: ty,
			Data: hir.CallData{SymbolID: id, Callee: ref(id), Args: []*hir.Expr{ref(arg)}},
		}
	}
	return &hir.Expr{
		Kind: kind,
		Type: ty,
		Data: hir.SelectData{
			Arms: []hir.SelectArm{{Await: call(11, 12), Result: ref(13)}},
			Crossing: &hir.CrossingData{
				RemoteOps: []hir.CrossingRemoteOp{{
					Receiver: ref(31), ReceiverType: ty, Value: call(32, 33),
				}},
				PayloadType: ty,
			},
		},
	}
}
