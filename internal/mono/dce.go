package mono

import (
	"slices"

	"surge/internal/hir"
	"surge/internal/symbols"
)

func (b *monoBuilder) applyDCE() {
	if b == nil || b.mm == nil {
		return
	}

	roots := b.dceRoots()
	if len(roots) == 0 {
		return
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
		for _, callee := range collectCallSyms(mf.Func.Body) {
			if callee.IsValid() {
				work = append(work, callee)
			}
		}
	}

	for k, mf := range b.mm.Funcs {
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
}

func (b *monoBuilder) dceRoots() []symbols.SymbolID {
	if b == nil || b.mm == nil || b.mod == nil {
		return nil
	}

	var roots []symbols.SymbolID
	for _, fn := range b.mod.Funcs {
		if fn == nil || fn.IsGeneric() || !fn.SymbolID.IsValid() {
			continue
		}
		if fn.Flags.HasFlag(hir.FuncEntrypoint) || fn.Flags.HasFlag(hir.FuncPublic) || fn.Name == "main" {
			mf := b.mm.Funcs[MonoKey{Sym: fn.SymbolID, ArgsKey: ""}]
			if mf != nil && mf.InstanceSym.IsValid() {
				roots = append(roots, mf.InstanceSym)
			}
		}
	}
	return roots
}

func collectCallSyms(b *hir.Block) []symbols.SymbolID {
	if b == nil {
		return nil
	}
	var out []symbols.SymbolID
	var walkExpr func(e *hir.Expr)
	var walkBlock func(bl *hir.Block)
	var walkStmt func(st *hir.Stmt)

	walkExpr = func(e *hir.Expr) {
		if e == nil {
			return
		}
		switch e.Kind {
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
		case hir.ExprAwait:
			data, ok := e.Data.(hir.AwaitData)
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
		case hir.ExprAsync:
			data, ok := e.Data.(hir.AsyncData)
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

	walkBlock(b)
	return out
}
