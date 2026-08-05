package mono

import (
	"fmt"
	"slices"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/symbols"
)

func (b *monoBuilder) applyDCE() error {
	if b == nil || b.mm == nil {
		return nil
	}

	roots, err := b.dceRoots()
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return nil
	}

	reachable := make(map[symbols.SymbolID]struct{}, len(roots))
	work := slices.Clone(roots)
	for len(work) > 0 {
		last := len(work) - 1
		sym := work[last]
		work = work[:last]
		if _, ok := reachable[sym]; ok {
			continue
		}
		reachable[sym] = struct{}{}
		mf := b.mm.FuncBySym[sym]
		if mf == nil || mf.Func == nil || mf.Func.Body == nil {
			continue
		}
		for _, callee := range collectFuncCallSyms(mf.Func) {
			if callee.IsValid() {
				work = append(work, callee)
			}
		}
	}

	for _, k := range b.mm.SortedFuncKeys() {
		mf := b.mm.Funcs[k]
		if mf == nil {
			delete(b.mm.Funcs, k)
			continue
		}
		if _, ok := reachable[mf.InstanceSym]; !ok {
			delete(b.mm.FuncBySym, mf.InstanceSym)
			delete(b.mm.Funcs, k)
		}
	}

	b.mm.Types = make(map[MonoKey]*MonoType)
	b.collectTypesFromFuncs()
	b.mm.Callables = newCallableMap(b.identity)
	for _, key := range b.mm.SortedFuncKeys() {
		if fn := b.mm.Funcs[key]; fn != nil {
			if err := b.mm.Callables.bind(fn.OrigSym, fn.TypeArgs, fn.InstanceSym); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *monoBuilder) dceRoots() ([]symbols.SymbolID, error) {
	if b == nil || b.mm == nil || b.mod == nil {
		return nil, nil
	}

	var roots []symbols.SymbolID
	for _, fn := range b.mod.Funcs {
		if fn == nil || fn.IsGeneric() || !fn.SymbolID.IsValid() {
			continue
		}
		if fn.Flags.HasFlag(hir.FuncEntrypoint) || fn.Flags.HasFlag(hir.FuncPublic) || fn.Name == "main" {
			if b.closure != nil {
				_, retained, retainedErr := b.retainedCallableFor(fn.SymbolID, nil)
				if retainedErr != nil {
					return nil, retainedErr
				}
				if !retained {
					continue
				}
			}
			instance, ok, err := b.mm.Callables.LookupChecked(fn.SymbolID, nil)
			if err != nil {
				return nil, err
			}
			if !ok || !instance.IsValid() {
				return nil, fmt.Errorf("mono: retained DCE root %s has no emitted callable instance", fn.Name)
			}
			roots = append(roots, instance)
		}
	}
	for i := range b.entrypointBindings {
		binding := &b.entrypointBindings[i]
		if binding.Outcome != sema.EntrypointCallableUser {
			continue
		}
		instance, ok, err := b.mm.Callables.LookupChecked(binding.Callee, binding.TemplateArgs)
		if err != nil {
			return nil, err
		}
		if !ok || !instance.IsValid() {
			return nil, fmt.Errorf("mono: sema-selected entrypoint callable %s has no emitted instance", binding.CalleeKey)
		}
		roots = append(roots, instance)
	}
	return roots, nil
}

func collectFuncCallSyms(fn *hir.Func) []symbols.SymbolID {
	if fn == nil {
		return nil
	}
	defaults := make([]*hir.Expr, 0, len(fn.Params))
	for i := range fn.Params {
		defaults = append(defaults, fn.Params[i].Default)
	}
	return collectCallSymsFrom(defaults, fn.Body)
}

func collectCallSymsFrom(defaults []*hir.Expr, b *hir.Block) []symbols.SymbolID {
	if b == nil && len(defaults) == 0 {
		return nil
	}
	var out []symbols.SymbolID
	var walkExpr func(e *hir.Expr)
	var walkBlock func(bl *hir.Block)
	var walkStmt func(st *hir.Stmt)
	var walkCrossing func(data *hir.CrossingData)

	walkExpr = func(e *hir.Expr) {
		if e == nil {
			return
		}
		switch e.Kind {
		case hir.ExprOwnedTemp:
			data, ok := e.Data.(hir.OwnedTempData)
			if !ok {
				return
			}
			walkExpr(data.Inner)
		case hir.ExprVarRef:
			data, ok := e.Data.(hir.VarRefData)
			if ok && data.SymbolID.IsValid() {
				out = append(out, data.SymbolID)
			}
		case hir.ExprCall:
			data, ok := e.Data.(hir.CallData)
			if !ok {
				return
			}
			if data.SymbolID.IsValid() {
				out = append(out, data.SymbolID)
			}
			walkExpr(data.Callee)
			for _, a := range data.Args {
				walkExpr(a)
			}
		case hir.ExprUnaryOp:
			data, ok := e.Data.(hir.UnaryOpData)
			if !ok {
				return
			}
			walkExpr(data.Operand)
		case hir.ExprBinaryOp:
			data, ok := e.Data.(hir.BinaryOpData)
			if !ok {
				return
			}
			walkExpr(data.Left)
			walkExpr(data.Right)
		case hir.ExprFieldAccess:
			data, ok := e.Data.(hir.FieldAccessData)
			if !ok {
				return
			}
			walkExpr(data.Object)
		case hir.ExprIndex:
			data, ok := e.Data.(hir.IndexData)
			if !ok {
				return
			}
			walkExpr(data.Object)
			walkExpr(data.Index)
		case hir.ExprStructLit:
			data, ok := e.Data.(hir.StructLitData)
			if !ok {
				return
			}
			for _, f := range data.Fields {
				walkExpr(f.Value)
			}
		case hir.ExprArrayLit:
			data, ok := e.Data.(hir.ArrayLitData)
			if !ok {
				return
			}
			for _, el := range data.Elements {
				walkExpr(el)
			}
		case hir.ExprMapLit:
			data, ok := e.Data.(hir.MapLitData)
			if !ok {
				return
			}
			for _, entry := range data.Entries {
				walkExpr(entry.Key)
				walkExpr(entry.Value)
			}
		case hir.ExprTupleLit:
			data, ok := e.Data.(hir.TupleLitData)
			if !ok {
				return
			}
			for _, el := range data.Elements {
				walkExpr(el)
			}
		case hir.ExprCompare:
			data, ok := e.Data.(hir.CompareData)
			if !ok {
				return
			}
			walkExpr(data.Value)
			for _, arm := range data.Arms {
				walkExpr(arm.Pattern)
				walkExpr(arm.Guard)
				walkExpr(arm.Result)
			}
		case hir.ExprSelect, hir.ExprRace:
			data, ok := e.Data.(hir.SelectData)
			if !ok {
				return
			}
			for i := range data.Arms {
				walkExpr(data.Arms[i].Await)
				walkExpr(data.Arms[i].Result)
			}
			walkCrossing(data.Crossing)
		case hir.ExprTagTest:
			data, ok := e.Data.(hir.TagTestData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.ExprTagPayload:
			data, ok := e.Data.(hir.TagPayloadData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.ExprIterInit:
			data, ok := e.Data.(hir.IterInitData)
			if !ok {
				return
			}
			walkExpr(data.Iterable)
		case hir.ExprIterNext:
			data, ok := e.Data.(hir.IterNextData)
			if !ok {
				return
			}
			walkExpr(data.Iter)
		case hir.ExprIf:
			data, ok := e.Data.(hir.IfData)
			if !ok {
				return
			}
			walkExpr(data.Cond)
			walkExpr(data.Then)
			walkExpr(data.Else)
		case hir.ExprRaiseReleaseGuard:
			data, ok := e.Data.(hir.RaiseReleaseGuardData)
			if !ok {
				return
			}
			walkExpr(data.Inner)
		case hir.ExprAwait:
			data, ok := e.Data.(hir.AwaitData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.ExprTask:
			data, ok := e.Data.(hir.TaskData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.ExprSpawn:
			data, ok := e.Data.(hir.SpawnData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.ExprCrossing:
			data, ok := e.Data.(hir.CrossingData)
			if !ok {
				return
			}
			walkCrossing(&data)
		case hir.ExprAsync:
			data, ok := e.Data.(hir.AsyncData)
			if !ok {
				return
			}
			walkBlock(data.Body)
		case hir.ExprBlocking:
			data, ok := e.Data.(hir.BlockingData)
			if !ok {
				return
			}
			walkBlock(data.Body)
		case hir.ExprCast:
			data, ok := e.Data.(hir.CastData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.ExprBlock:
			data, ok := e.Data.(hir.BlockExprData)
			if !ok {
				return
			}
			walkBlock(data.Block)
		default:
		}
	}

	walkStmt = func(st *hir.Stmt) {
		if st == nil {
			return
		}
		switch st.Kind {
		case hir.StmtLet:
			data, ok := st.Data.(hir.LetData)
			if !ok {
				return
			}
			walkExpr(data.Value)
			walkExpr(data.Pattern)
		case hir.StmtExpr:
			data, ok := st.Data.(hir.ExprStmtData)
			if !ok {
				return
			}
			walkExpr(data.Expr)
		case hir.StmtAssign:
			data, ok := st.Data.(hir.AssignData)
			if !ok {
				return
			}
			walkExpr(data.Target)
			walkExpr(data.Value)
		case hir.StmtReturn:
			data, ok := st.Data.(hir.ReturnData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.StmtRet:
			data, ok := st.Data.(hir.RetData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.StmtIf:
			data, ok := st.Data.(hir.IfStmtData)
			if !ok {
				return
			}
			walkExpr(data.Cond)
			walkBlock(data.Then)
			walkBlock(data.Else)
		case hir.StmtWhile:
			data, ok := st.Data.(hir.WhileData)
			if !ok {
				return
			}
			walkExpr(data.Cond)
			walkBlock(data.Body)
		case hir.StmtFor:
			data, ok := st.Data.(hir.ForData)
			if !ok {
				return
			}
			if data.Init != nil {
				walkStmt(data.Init)
			}
			walkExpr(data.Cond)
			walkExpr(data.Post)
			walkExpr(data.Iterable)
			walkBlock(data.Body)
		case hir.StmtBlock:
			data, ok := st.Data.(hir.BlockStmtData)
			if !ok {
				return
			}
			walkBlock(data.Block)
		case hir.StmtDrop:
			data, ok := st.Data.(hir.DropData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		case hir.StmtEnvelopeRelease:
			data, ok := st.Data.(hir.EnvelopeReleaseData)
			if !ok {
				return
			}
			walkExpr(data.Value)
		default:
		}
	}

	walkBlock = func(bl *hir.Block) {
		if bl == nil {
			return
		}
		for i := range bl.Stmts {
			walkStmt(&bl.Stmts[i])
		}
	}
	walkCrossing = func(data *hir.CrossingData) {
		if data == nil {
			return
		}
		walkExpr(data.Destination.Value)
		for i := range data.Captures {
			walkExpr(data.Captures[i].Value)
		}
		for i := range data.RemoteOps {
			walkExpr(data.RemoteOps[i].Receiver)
			walkExpr(data.RemoteOps[i].Value)
		}
		walkExpr(data.Receiver)
		walkBlock(data.Body)
	}

	for _, expr := range defaults {
		walkExpr(expr)
	}
	walkBlock(b)
	return out
}
