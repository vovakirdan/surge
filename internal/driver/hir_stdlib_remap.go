package driver

import (
	"surge/internal/hir"
	"surge/internal/symbols"
)

// The symbol-remapping walk. Core's HIR is merged into the root module, so
// every symbol id it mentions has to be rewritten into the root's numbering.
//
// It sits beside CombineHIRWithCore rather than inside it because it is one
// thing: an exhaustive traversal of the HIR tree, every statement and
// expression kind once. Kinds are enumerated rather than defaulted on purpose
// — a new kind silently skipped here would carry core's numbering into the
// merged module.

type remapState struct {
	seenExpr map[*hir.Expr]struct{}
}

func remapHIRModule(mod *hir.Module, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if mod == nil || len(mapping) == 0 {
		return
	}
	state := &remapState{
		seenExpr: make(map[*hir.Expr]struct{}),
	}
	for i := range mod.Types {
		mod.Types[i].SymbolID = remapSymbol(mod.Types[i].SymbolID, mapping)
	}
	for i := range mod.Consts {
		mod.Consts[i].SymbolID = remapSymbol(mod.Consts[i].SymbolID, mapping)
		remapExpr(mod.Consts[i].Value, mapping, state)
	}
	for i := range mod.Globals {
		mod.Globals[i].SymbolID = remapSymbol(mod.Globals[i].SymbolID, mapping)
		remapExpr(mod.Globals[i].Value, mapping, state)
	}
	for _, fn := range mod.Funcs {
		remapFunc(fn, mapping, state)
	}
}

func remapFunc(fn *hir.Func, mapping map[symbols.SymbolID]symbols.SymbolID, state *remapState) {
	if fn == nil {
		return
	}
	fn.SymbolID = remapSymbol(fn.SymbolID, mapping)
	for i := range fn.Params {
		fn.Params[i].SymbolID = remapSymbol(fn.Params[i].SymbolID, mapping)
	}
	if fn.Body != nil {
		remapBlock(fn.Body, mapping, state)
	}
}

func remapBlock(block *hir.Block, mapping map[symbols.SymbolID]symbols.SymbolID, state *remapState) {
	if block == nil {
		return
	}
	for i := range block.Stmts {
		remapStmt(&block.Stmts[i], mapping, state)
	}
}

func remapStmt(st *hir.Stmt, mapping map[symbols.SymbolID]symbols.SymbolID, state *remapState) {
	if st == nil {
		return
	}
	switch st.Kind {
	case hir.StmtLet:
		data, ok := st.Data.(hir.LetData)
		if !ok {
			return
		}
		data.SymbolID = remapSymbol(data.SymbolID, mapping)
		remapExpr(data.Value, mapping, state)
		remapExpr(data.Pattern, mapping, state)
		st.Data = data
	case hir.StmtExpr:
		data, ok := st.Data.(hir.ExprStmtData)
		if !ok {
			return
		}
		remapExpr(data.Expr, mapping, state)
		st.Data = data
	case hir.StmtAssign:
		data, ok := st.Data.(hir.AssignData)
		if !ok {
			return
		}
		remapExpr(data.Target, mapping, state)
		remapExpr(data.Value, mapping, state)
		st.Data = data
	case hir.StmtReturn:
		data, ok := st.Data.(hir.ReturnData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		for i := range data.DropsAfterValue {
			data.DropsAfterValue[i].SymbolID = remapSymbol(data.DropsAfterValue[i].SymbolID, mapping)
		}
		st.Data = data
	case hir.StmtRet:
		data, ok := st.Data.(hir.RetData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		// Its drop list needs remapping for the same reason StmtReturn's does:
		// an un-remapped symbol names whatever now occupies that slot, so the
		// exit drops something it was never given. `ret` acquired a drop list
		// later than `return` did, and this was one of the sites that did not
		// hear about it.
		for i := range data.DropsAfterValue {
			data.DropsAfterValue[i].SymbolID = remapSymbol(data.DropsAfterValue[i].SymbolID, mapping)
		}
		st.Data = data
	case hir.StmtIf:
		data, ok := st.Data.(hir.IfStmtData)
		if !ok {
			return
		}
		remapExpr(data.Cond, mapping, state)
		remapBlock(data.Then, mapping, state)
		remapBlock(data.Else, mapping, state)
		st.Data = data
	case hir.StmtWhile:
		data, ok := st.Data.(hir.WhileData)
		if !ok {
			return
		}
		remapExpr(data.Cond, mapping, state)
		remapBlock(data.Body, mapping, state)
		st.Data = data
	case hir.StmtFor:
		data, ok := st.Data.(hir.ForData)
		if !ok {
			return
		}
		data.VarSym = remapSymbol(data.VarSym, mapping)
		if data.Init != nil {
			remapStmt(data.Init, mapping, state)
		}
		remapExpr(data.Cond, mapping, state)
		remapExpr(data.Post, mapping, state)
		remapExpr(data.Iterable, mapping, state)
		remapBlock(data.Body, mapping, state)
		st.Data = data
	case hir.StmtBlock:
		data, ok := st.Data.(hir.BlockStmtData)
		if !ok {
			return
		}
		remapBlock(data.Block, mapping, state)
		st.Data = data
	case hir.StmtDrop:
		data, ok := st.Data.(hir.DropData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		st.Data = data
	default:
	}
}

func remapExpr(expr *hir.Expr, mapping map[symbols.SymbolID]symbols.SymbolID, state *remapState) {
	if expr == nil {
		return
	}
	if state != nil {
		if _, ok := state.seenExpr[expr]; ok {
			return
		}
		state.seenExpr[expr] = struct{}{}
	}
	switch expr.Kind {
	case hir.ExprOwnedTemp:
		data, ok := expr.Data.(hir.OwnedTempData)
		if !ok {
			return
		}
		remapExpr(data.Inner, mapping, state)
		expr.Data = data
	case hir.ExprRaiseReleaseGuard:
		data, ok := expr.Data.(hir.RaiseReleaseGuardData)
		if !ok {
			return
		}
		remapExpr(data.Inner, mapping, state)
		expr.Data = data
	case hir.ExprVarRef:
		data, ok := expr.Data.(hir.VarRefData)
		if !ok {
			return
		}
		data.SymbolID = remapSymbol(data.SymbolID, mapping)
		expr.Data = data
	case hir.ExprUnaryOp:
		data, ok := expr.Data.(hir.UnaryOpData)
		if !ok {
			return
		}
		remapExpr(data.Operand, mapping, state)
		expr.Data = data
	case hir.ExprBinaryOp:
		data, ok := expr.Data.(hir.BinaryOpData)
		if !ok {
			return
		}
		remapExpr(data.Left, mapping, state)
		remapExpr(data.Right, mapping, state)
		expr.Data = data
	case hir.ExprCall:
		data, ok := expr.Data.(hir.CallData)
		if !ok {
			return
		}
		data.SymbolID = remapSymbol(data.SymbolID, mapping)
		remapExpr(data.Callee, mapping, state)
		for _, arg := range data.Args {
			remapExpr(arg, mapping, state)
		}
		expr.Data = data
	case hir.ExprFieldAccess:
		data, ok := expr.Data.(hir.FieldAccessData)
		if !ok {
			return
		}
		remapExpr(data.Object, mapping, state)
		expr.Data = data
	case hir.ExprIndex:
		data, ok := expr.Data.(hir.IndexData)
		if !ok {
			return
		}
		remapExpr(data.Object, mapping, state)
		remapExpr(data.Index, mapping, state)
		expr.Data = data
	case hir.ExprStructLit:
		data, ok := expr.Data.(hir.StructLitData)
		if !ok {
			return
		}
		for i := range data.Fields {
			remapExpr(data.Fields[i].Value, mapping, state)
		}
		expr.Data = data
	case hir.ExprArrayLit:
		data, ok := expr.Data.(hir.ArrayLitData)
		if !ok {
			return
		}
		for _, el := range data.Elements {
			remapExpr(el, mapping, state)
		}
		expr.Data = data
	case hir.ExprMapLit:
		data, ok := expr.Data.(hir.MapLitData)
		if !ok {
			return
		}
		for _, entry := range data.Entries {
			remapExpr(entry.Key, mapping, state)
			remapExpr(entry.Value, mapping, state)
		}
		expr.Data = data
	case hir.ExprTupleLit:
		data, ok := expr.Data.(hir.TupleLitData)
		if !ok {
			return
		}
		for _, el := range data.Elements {
			remapExpr(el, mapping, state)
		}
		expr.Data = data
	case hir.ExprCompare:
		data, ok := expr.Data.(hir.CompareData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		for i := range data.Arms {
			remapExpr(data.Arms[i].Pattern, mapping, state)
			remapExpr(data.Arms[i].Guard, mapping, state)
			remapExpr(data.Arms[i].Result, mapping, state)
		}
		expr.Data = data
	case hir.ExprTagTest:
		data, ok := expr.Data.(hir.TagTestData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		expr.Data = data
	case hir.ExprTagPayload:
		data, ok := expr.Data.(hir.TagPayloadData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		expr.Data = data
	case hir.ExprIterInit:
		data, ok := expr.Data.(hir.IterInitData)
		if !ok {
			return
		}
		remapExpr(data.Iterable, mapping, state)
		expr.Data = data
	case hir.ExprIterNext:
		data, ok := expr.Data.(hir.IterNextData)
		if !ok {
			return
		}
		remapExpr(data.Iter, mapping, state)
		expr.Data = data
	case hir.ExprIf:
		data, ok := expr.Data.(hir.IfData)
		if !ok {
			return
		}
		remapExpr(data.Cond, mapping, state)
		remapExpr(data.Then, mapping, state)
		remapExpr(data.Else, mapping, state)
		expr.Data = data
	case hir.ExprAwait:
		data, ok := expr.Data.(hir.AwaitData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		expr.Data = data
	case hir.ExprTask:
		data, ok := expr.Data.(hir.TaskData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		expr.Data = data
	case hir.ExprSpawn:
		data, ok := expr.Data.(hir.SpawnData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		expr.Data = data
	case hir.ExprAsync:
		data, ok := expr.Data.(hir.AsyncData)
		if !ok {
			return
		}
		remapBlock(data.Body, mapping, state)
		expr.Data = data
	case hir.ExprBlocking:
		data, ok := expr.Data.(hir.BlockingData)
		if !ok {
			return
		}
		remapBlock(data.Body, mapping, state)
		for i := range data.Captures {
			data.Captures[i].SymbolID = remapSymbol(data.Captures[i].SymbolID, mapping)
		}
		expr.Data = data
	case hir.ExprCrossing:
		data, ok := expr.Data.(hir.CrossingData)
		if !ok {
			return
		}
		data.Destination.AnchorSymbol = remapSymbol(data.Destination.AnchorSymbol, mapping)
		remapExpr(data.Destination.Value, mapping, state)
		remapBlock(data.Body, mapping, state)
		for i := range data.Captures {
			data.Captures[i].Symbol = remapSymbol(data.Captures[i].Symbol, mapping)
			remapExpr(data.Captures[i].Value, mapping, state)
		}
		for i := range data.RemoteOps {
			data.RemoteOps[i].ReceiverSymbol = remapSymbol(data.RemoteOps[i].ReceiverSymbol, mapping)
			remapExpr(data.RemoteOps[i].Receiver, mapping, state)
		}
		data.ReceiverSymbol = remapSymbol(data.ReceiverSymbol, mapping)
		remapExpr(data.Receiver, mapping, state)
		expr.Data = data
	case hir.ExprCast:
		data, ok := expr.Data.(hir.CastData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping, state)
		expr.Data = data
	case hir.ExprBlock:
		data, ok := expr.Data.(hir.BlockExprData)
		if !ok {
			return
		}
		remapBlock(data.Block, mapping, state)
		expr.Data = data
	default:
	}
}
