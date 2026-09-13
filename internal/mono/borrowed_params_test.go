package mono

import (
	"reflect"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestPrepareBorrowedParamFiltersOnlyReadonlySyntheticCleanup(t *testing.T) {
	fn, in := borrowedTestFunc()
	p, q := fn.Params[0].SymbolID, fn.Params[1].SymbolID
	shadow := symbols.SymbolID(9)
	drops := []hir.DropLocal{
		{SymbolID: p, Type: in.Builtins().Int},
		{SymbolID: shadow, Type: in.Builtins().Int, Steps: []sema.DropStep{{Shallow: true}}},
		{SymbolID: q, Type: in.Builtins().Int},
	}
	fn.Body.Stmts = []hir.Stmt{
		borrowedTestDrop(in, p, true), borrowedTestDrop(in, q, true),
		borrowedTestDrop(in, q, false), borrowedTestDrop(in, shadow, true),
		{Kind: hir.StmtReturn, Data: hir.ReturnData{Value: borrowedTestVar(in, p), DropsAfterValue: drops}},
		{Kind: hir.StmtRet, Data: hir.RetData{Value: borrowedTestVar(in, p), DropsAfterValue: drops}},
	}
	before := cloneBlock(fn.Body)
	prepared, working, err := PrepareBorrowedParamBody(fn, in, []symbols.SymbolID{q, p})
	if err != nil || !slices.Equal(working, []symbols.SymbolID{q}) || prepared == fn {
		t.Fatalf("preparation: owners=%v error=%v", working, err)
	}
	if !reflect.DeepEqual(fn.Body, before) || len(prepared.Body.Stmts) != 5 {
		t.Fatal("source HIR changed or unrelated statements were removed")
	}
	for _, index := range []int{3, 4} {
		var got []hir.DropLocal
		switch d := prepared.Body.Stmts[index].Data.(type) {
		case hir.ReturnData:
			got = d.DropsAfterValue
		case hir.RetData:
			got = d.DropsAfterValue
		}
		if !reflect.DeepEqual(got, drops[1:]) {
			t.Fatalf("exit %d lost unrelated order/type/residual plan: %+v", index, got)
		}
	}
	for i, want := range []hir.Stmt{before.Stmts[1], before.Stmts[2], before.Stmts[3]} {
		if !reflect.DeepEqual(prepared.Body.Stmts[i], want) {
			t.Fatalf("working/explicit/shadow drop %d changed", i)
		}
	}
}

func TestPrepareBorrowedParamWholeSlotEffects(t *testing.T) {
	fn, in := borrowedTestFunc()
	p, q := fn.Params[0].SymbolID, fn.Params[1].SymbolID
	varP := borrowedTestVar(in, p)
	mutRef := in.Intern(types.MakeReference(in.Builtins().Int, true))
	wrappers := []struct {
		name string
		slot *hir.Expr
		want bool
	}{
		{"whole", varP, true},
		{"guard", &hir.Expr{Kind: hir.ExprRaiseReleaseGuard, Data: hir.RaiseReleaseGuardData{Inner: varP}}, true},
		{"owned_temp", &hir.Expr{Kind: hir.ExprOwnedTemp, Data: hir.OwnedTempData{Inner: varP}}, false},
		{"cast", &hir.Expr{Kind: hir.ExprCast, Data: hir.CastData{Value: varP}}, false},
		{"own", &hir.Expr{Kind: hir.ExprUnaryOp, Data: hir.UnaryOpData{Op: ast.ExprUnaryOwn, Operand: varP}}, false},
		{"dereference", &hir.Expr{Kind: hir.ExprUnaryOp, Data: hir.UnaryOpData{Op: ast.ExprUnaryDeref, Operand: varP}}, false},
		{"field", &hir.Expr{Kind: hir.ExprFieldAccess, Data: hir.FieldAccessData{Object: varP}}, false},
		{"index", &hir.Expr{Kind: hir.ExprIndex, Data: hir.IndexData{Object: varP, Index: varP}}, false},
		{"same_name_other_symbol", borrowedTestVar(in, 9), false},
	}
	for _, tc := range wrappers {
		t.Run(tc.name, func(t *testing.T) {
			address := &hir.Expr{Kind: hir.ExprUnaryOp, Type: mutRef, Data: hir.UnaryOpData{Op: ast.ExprUnaryRefMut, Operand: tc.slot}}
			fn.Body.Stmts = []hir.Stmt{{Kind: hir.StmtExpr, Data: hir.ExprStmtData{Expr: address}}}
			_, working, err := PrepareBorrowedParamBody(fn, in, []symbols.SymbolID{p})
			if err != nil || slices.Contains(working, p) != tc.want {
				t.Fatalf("whole-slot address: owners=%v error=%v", working, err)
			}
		})
	}
	t.Run("generated_assignment_without_overwrite_flag", func(t *testing.T) {
		fn.Body.Stmts = []hir.Stmt{
			{Kind: hir.StmtAssign, Data: hir.AssignData{Target: borrowedTestVar(in, q), Value: varP}},
			{Kind: hir.StmtExpr, Data: hir.ExprStmtData{Expr: &hir.Expr{Kind: hir.ExprBinaryOp, Data: hir.BinaryOpData{
				Op: ast.ExprBinaryAssign, Left: varP, Right: borrowedTestVar(in, q), DropOverwritten: false,
			}}}},
		}
		_, owners, err := PrepareBorrowedParamBody(fn, in, []symbols.SymbolID{q, p})
		if err != nil || !slices.Equal(owners, []symbols.SymbolID{p, q}) {
			t.Fatalf("generated assignment / parameter-order proof: %v, %v", owners, err)
		}
	})
}

func TestPrepareBorrowedParamActivationFoldAndFilter(t *testing.T) {
	// Each wrapper is tested twice with the SAME SymbolID: first a real effect,
	// then readonly cleanup. The latter forces a private clone and inspects the
	// wrapped block directly, so a shared omission in fold/filter cannot pass.
	cases := []struct {
		name   string
		kind   hir.ExprKind
		wrap   func(*hir.Expr, *hir.Block) hir.ExprData
		parent bool
	}{
		{"block", hir.ExprBlock, func(_ *hir.Expr, b *hir.Block) hir.ExprData { return hir.BlockExprData{Block: b} }, true},
		{"async_body", hir.ExprAsync, func(_ *hir.Expr, b *hir.Block) hir.ExprData { return hir.AsyncData{Body: b} }, false},
		{"blocking_body", hir.ExprBlocking, func(_ *hir.Expr, b *hir.Block) hir.ExprData { return hir.BlockingData{Body: b} }, false},
		{"on_body", hir.ExprCrossing, func(_ *hir.Expr, b *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringOnPlacement, Body: b}
		}, false},
		{"spawn_on_body", hir.ExprCrossing, func(_ *hir.Expr, b *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringSpawnOn, Body: b}
		}, false},
		{"on_remote_value", hir.ExprCrossing, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringOnFarHandle, RemoteOps: []hir.CrossingRemoteOp{{Value: e}}}
		}, false},
		{"on_remote_receiver", hir.ExprCrossing, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringOnFarHandle, RemoteOps: []hir.CrossingRemoteOp{{Receiver: e}}}
		}, false},
		{"channel_select_ops_value", hir.ExprCrossing, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringChannelSelect, RemoteOps: []hir.CrossingRemoteOp{{Value: e}}}
		}, true},
		{"channel_select_ops_receiver", hir.ExprCrossing, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringChannelSelect, RemoteOps: []hir.CrossingRemoteOp{{Receiver: e}}}
		}, true},
		{"on_destination", hir.ExprCrossing, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringOnPlacement, Destination: hir.CrossingDestination{Value: e}}
		}, true},
		{"on_capture", hir.ExprCrossing, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringOnPlacement, Captures: []hir.CrossingCapture{{Value: e}}}
		}, true},
		{"far_receiver", hir.ExprCrossing, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.CrossingData{Kind: sema.CrossingLoweringFarTaskAwait, Receiver: e}
		}, true},
		{"remote_select_value", hir.ExprSelect, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.SelectData{Crossing: &hir.CrossingData{Kind: sema.CrossingLoweringChannelSelect, RemoteOps: []hir.CrossingRemoteOp{{Value: e}}}}
		}, true},
		{"remote_select_receiver", hir.ExprSelect, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.SelectData{Crossing: &hir.CrossingData{Kind: sema.CrossingLoweringChannelSelect, RemoteOps: []hir.CrossingRemoteOp{{Receiver: e}}}}
		}, true},
		{"remote_select_result", hir.ExprSelect, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.SelectData{Crossing: &hir.CrossingData{Kind: sema.CrossingLoweringChannelSelect}, Arms: []hir.SelectArm{{Result: e}}}
		}, true},
		{"remote_select_stale_await", hir.ExprSelect, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.SelectData{Crossing: &hir.CrossingData{Kind: sema.CrossingLoweringChannelSelect}, Arms: []hir.SelectArm{{Await: e}}}
		}, false},
		{"local_select_await", hir.ExprSelect, func(e *hir.Expr, _ *hir.Block) hir.ExprData { return hir.SelectData{Arms: []hir.SelectArm{{Await: e}}} }, true},
		{"local_race_result", hir.ExprRace, func(e *hir.Expr, _ *hir.Block) hir.ExprData {
			return hir.SelectData{Arms: []hir.SelectArm{{Result: e}}}
		}, true},
		{"task_value", hir.ExprTask, func(e *hir.Expr, _ *hir.Block) hir.ExprData { return hir.TaskData{Value: e} }, true},
		{"spawn_value", hir.ExprSpawn, func(e *hir.Expr, _ *hir.Block) hir.ExprData { return hir.SpawnData{Value: e} }, true},
		{"owned_temp_inner", hir.ExprOwnedTemp, func(e *hir.Expr, _ *hir.Block) hir.ExprData { return hir.OwnedTempData{Inner: e} }, true},
		{"guard_inner", hir.ExprRaiseReleaseGuard, func(e *hir.Expr, _ *hir.Block) hir.ExprData { return hir.RaiseReleaseGuardData{Inner: e} }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn, in := borrowedTestFunc()
			p := fn.Params[0].SymbolID
			for _, effect := range []bool{true, false} {
				body := &hir.Block{Stmts: []hir.Stmt{borrowedTestDrop(in, p, true)}}
				if effect {
					body.Stmts = append(body.Stmts, hir.Stmt{Kind: hir.StmtAssign, Data: hir.AssignData{Target: borrowedTestVar(in, p), Value: borrowedTestVar(in, p)}})
				}
				expr := &hir.Expr{Kind: hir.ExprBlock, Data: hir.BlockExprData{Block: body}}
				wrapped := &hir.Expr{Kind: tc.kind, Data: tc.wrap(expr, body)}
				fn.Body.Stmts = []hir.Stmt{borrowedTestDrop(in, p, true), {Kind: hir.StmtExpr, Data: hir.ExprStmtData{Expr: wrapped}}}
				prepared, owners, err := PrepareBorrowedParamBody(fn, in, []symbols.SymbolID{p})
				if err != nil || (len(owners) == 1) != (effect && tc.parent) {
					t.Fatalf("effect=%t parent=%t owners=%v error=%v", effect, tc.parent, owners, err)
				}
				if effect {
					continue
				}
				if prepared == fn || len(body.Stmts) != 1 {
					t.Fatal("cleanup preparation failed to clone or mutated the original nested body")
				}
				got := prepared.Body.Stmts[0].Data.(hir.ExprStmtData).Expr
				inner := borrowedWrappedBlock(got)
				if inner == nil || (len(inner.Stmts) == 0) != tc.parent {
					t.Fatalf("filter crossed or missed activation boundary: parent=%t block=%+v", tc.parent, inner)
				}
			}
		})
	}
}

func TestPrepareBorrowedParamDefaultsAreNotBodyEffects(t *testing.T) {
	fn, in := borrowedTestFunc()
	p := fn.Params[0].SymbolID
	defaultBody := &hir.Block{Stmts: []hir.Stmt{borrowedTestDrop(in, p, false)}}
	fn.Params[1].HasDefault = true
	fn.Params[1].Default = &hir.Expr{Kind: hir.ExprBlock, Data: hir.BlockExprData{Block: defaultBody}}
	fn.Body.Stmts = []hir.Stmt{borrowedTestDrop(in, p, true)}
	prepared, owners, err := PrepareBorrowedParamBody(fn, in, []symbols.SymbolID{p})
	if err != nil || len(owners) != 0 || len(prepared.Body.Stmts) != 0 {
		t.Fatalf("default effect entered function body: %v, %v", owners, err)
	}
	if !reflect.DeepEqual(fn.Params, prepared.Params) || len(defaultBody.Stmts) != 1 {
		t.Fatal("cleanup filtering changed default expression")
	}
}

func TestPrepareBorrowedParamRejectsMalformedInputs(t *testing.T) {
	cases := []struct {
		name   string
		change func(*hir.Func, *types.Interner, *[]symbols.SymbolID)
	}{
		{"missing_type", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) { f.Params[0].Type = types.NoTypeID }},
		{"missing_named_symbol", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) { f.Params[0].SymbolID = symbols.NoSymbolID }},
		{"duplicate_param", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) {
			f.Params[1].SymbolID = f.Params[0].SymbolID
		}},
		{"duplicate_candidate", func(_ *hir.Func, _ *types.Interner, c *[]symbols.SymbolID) { *c = append(*c, (*c)[0]) }},
		{"unknown_candidate", func(_ *hir.Func, _ *types.Interner, c *[]symbols.SymbolID) { *c = []symbols.SymbolID{99} }},
		{"reference_candidate", func(f *hir.Func, in *types.Interner, _ *[]symbols.SymbolID) {
			f.Params[0].Type = in.Intern(types.MakeReference(in.Builtins().Int, true))
		}},
		{"generic_function", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) {
			f.GenericParams = []hir.GenericParam{{Name: "T"}}
		}},
		{"async_function", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) { f.Flags |= hir.FuncAsync }},
		{"intrinsic_function", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) { f.Flags |= hir.FuncIntrinsic }},
		{"missing_body", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) { f.Body = nil }},
		{"unknown_expression", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtExpr, Data: hir.ExprStmtData{Expr: &hir.Expr{Kind: 255}}}}
		}},
		{"payload_kind_mismatch", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtReturn, Data: hir.DropData{}}}
		}},
		{"expression_payload_mismatch", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtExpr, Data: hir.ExprStmtData{Expr: &hir.Expr{Kind: hir.ExprLiteral, Data: hir.VarRefData{}}}}}
		}},
		{"unknown_crossing", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtExpr, Data: hir.ExprStmtData{Expr: &hir.Expr{Kind: hir.ExprCrossing, Data: hir.CrossingData{Kind: 255}}}}}
		}},
		{"unknown_for", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtFor, Data: hir.ForData{Kind: 255}}}
		}},
		{"missing_assignment_target", func(f *hir.Func, in *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtAssign, Data: hir.AssignData{Value: borrowedTestVar(in, f.Params[0].SymbolID)}}}
		}},
		{"missing_drop_value", func(f *hir.Func, _ *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtDrop, Data: hir.DropData{Synthetic: true}}}
		}},
		{"missing_mutable_element", func(f *hir.Func, in *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtExpr, Data: hir.ExprStmtData{Expr: &hir.Expr{
				Kind: hir.ExprUnaryOp, Type: in.Intern(types.MakeReference(types.NoTypeID, true)),
				Data: hir.UnaryOpData{Op: ast.ExprUnaryRefMut, Operand: borrowedTestVar(in, f.Params[0].SymbolID)},
			}}}}
		}},
		{"readonly_residual_plan", func(f *hir.Func, in *types.Interner, _ *[]symbols.SymbolID) {
			d := borrowedTestDrop(in, f.Params[0].SymbolID, true).Data.(hir.DropData)
			d.Steps = []sema.DropStep{{Shallow: true}}
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtDrop, Data: d}}
		}},
		{"untyped_mutable_address", func(f *hir.Func, in *types.Interner, _ *[]symbols.SymbolID) {
			f.Body.Stmts = []hir.Stmt{{Kind: hir.StmtExpr, Data: hir.ExprStmtData{Expr: &hir.Expr{
				Kind: hir.ExprUnaryOp, Type: in.Builtins().Int,
				Data: hir.UnaryOpData{Op: ast.ExprUnaryRefMut, Operand: borrowedTestVar(in, f.Params[0].SymbolID)},
			}}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn, in := borrowedTestFunc()
			candidates := []symbols.SymbolID{fn.Params[0].SymbolID}
			tc.change(fn, in, &candidates)
			if _, _, err := PrepareBorrowedParamBody(fn, in, candidates); err == nil {
				t.Fatal("malformed preparation input silently classified readonly")
			}
		})
	}
}

func TestPrepareBorrowedParamPreservesNonCandidates(t *testing.T) {
	for _, name := range []string{"anonymous", "async", "intrinsic", "no_body"} {
		t.Run(name, func(t *testing.T) {
			fn, in := borrowedTestFunc()
			switch name {
			case "anonymous":
				fn.Params[0].Name, fn.Params[0].SymbolID = "_", symbols.NoSymbolID
			case "async":
				fn.Flags |= hir.FuncAsync
			case "intrinsic":
				fn.Flags |= hir.FuncIntrinsic
			case "no_body":
				fn.Body = nil
			}
			prepared, owners, err := PrepareBorrowedParamBody(fn, in, nil)
			if err != nil || prepared != fn || len(owners) != 0 {
				t.Fatalf("non-candidate function changed: owners=%v error=%v", owners, err)
			}
		})
	}
}

func borrowedTestFunc() (*hir.Func, *types.Interner) {
	in := types.NewInterner()
	return &hir.Func{Name: "test", Body: &hir.Block{}, Params: []hir.Param{
		{Name: "p", SymbolID: 7, Type: in.Builtins().Int},
		{Name: "q", SymbolID: 8, Type: in.Builtins().Int},
	}}, in
}

func borrowedTestVar(in *types.Interner, sym symbols.SymbolID) *hir.Expr {
	return &hir.Expr{Kind: hir.ExprVarRef, Type: in.Builtins().Int, Data: hir.VarRefData{Name: "p", SymbolID: sym}}
}

func borrowedTestDrop(in *types.Interner, sym symbols.SymbolID, synthetic bool) hir.Stmt {
	return hir.Stmt{Kind: hir.StmtDrop, Data: hir.DropData{Value: borrowedTestVar(in, sym), Synthetic: synthetic}}
}

// Independent fixture projection; this is deliberately not the production walk.
func borrowedWrappedBlock(expr *hir.Expr) *hir.Block {
	switch d := expr.Data.(type) {
	case hir.BlockExprData:
		return d.Block
	case hir.AsyncData:
		return d.Body
	case hir.BlockingData:
		return d.Body
	case hir.CrossingData:
		if d.Body != nil {
			return d.Body
		}
		if len(d.RemoteOps) != 0 {
			if d.RemoteOps[0].Receiver != nil {
				return borrowedWrappedBlock(d.RemoteOps[0].Receiver)
			}
			return borrowedWrappedBlock(d.RemoteOps[0].Value)
		}
		if d.Destination.Value != nil {
			return borrowedWrappedBlock(d.Destination.Value)
		}
		if d.Receiver != nil {
			return borrowedWrappedBlock(d.Receiver)
		}
		return borrowedWrappedBlock(d.Captures[0].Value)
	case hir.SelectData:
		if d.Crossing != nil && len(d.Crossing.RemoteOps) != 0 {
			return borrowedWrappedBlock(&hir.Expr{Data: *d.Crossing})
		}
		if d.Arms[0].Await != nil {
			return borrowedWrappedBlock(d.Arms[0].Await)
		}
		return borrowedWrappedBlock(d.Arms[0].Result)
	case hir.TaskData:
		return borrowedWrappedBlock(d.Value)
	case hir.SpawnData:
		return borrowedWrappedBlock(d.Value)
	case hir.OwnedTempData:
		return borrowedWrappedBlock(d.Inner)
	case hir.RaiseReleaseGuardData:
		return borrowedWrappedBlock(d.Inner)
	}
	return nil
}
