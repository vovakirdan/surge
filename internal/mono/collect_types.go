package mono

import (
	"surge/internal/hir"
	"surge/internal/types"
)

func collectTypeFromFunc(fn *hir.Func, visit func(id types.TypeID)) {
	if fn == nil || visit == nil {
		return
	}
	for _, p := range fn.Params {
		visit(p.Type)
		if p.Default != nil {
			collectTypesFromExpr(p.Default, visit)
		}
	}
	visit(fn.Result)
	collectTypesFromBlock(fn.Body, visit)
}

func collectTypesFromBlock(b *hir.Block, visit func(id types.TypeID)) {
	if b == nil || visit == nil {
		return
	}
	for i := range b.Stmts {
		collectTypesFromStmt(&b.Stmts[i], visit)
	}
}

func collectTypesFromStmt(st *hir.Stmt, visit func(id types.TypeID)) {
	if st == nil || visit == nil {
		return
	}
	switch st.Kind {
	case hir.StmtLet:
		data, ok := st.Data.(hir.LetData)
		if !ok {
			return
		}
		visit(data.Type)
		collectTypesFromExpr(data.Value, visit)
		collectTypesFromExpr(data.Pattern, visit)
	case hir.StmtExpr:
		data, ok := st.Data.(hir.ExprStmtData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Expr, visit)
	case hir.StmtAssign:
		data, ok := st.Data.(hir.AssignData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Target, visit)
		collectTypesFromExpr(data.Value, visit)
	case hir.StmtReturn:
		data, ok := st.Data.(hir.ReturnData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Value, visit)
	case hir.StmtIf:
		data, ok := st.Data.(hir.IfStmtData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Cond, visit)
		collectTypesFromBlock(data.Then, visit)
		collectTypesFromBlock(data.Else, visit)
	case hir.StmtWhile:
		data, ok := st.Data.(hir.WhileData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Cond, visit)
		collectTypesFromBlock(data.Body, visit)
	case hir.StmtFor:
		data, ok := st.Data.(hir.ForData)
		if !ok {
			return
		}
		if data.Init != nil {
			collectTypesFromStmt(data.Init, visit)
		}
		visit(data.VarType)
		collectTypesFromExpr(data.Cond, visit)
		collectTypesFromExpr(data.Post, visit)
		collectTypesFromExpr(data.Iterable, visit)
		collectTypesFromBlock(data.Body, visit)
	case hir.StmtBlock:
		data, ok := st.Data.(hir.BlockStmtData)
		if !ok {
			return
		}
		collectTypesFromBlock(data.Block, visit)
	case hir.StmtDrop:
		data, ok := st.Data.(hir.DropData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Value, visit)
	default:
	}
}

func collectTypesFromExpr(e *hir.Expr, visit func(id types.TypeID)) {
	if e == nil || visit == nil {
		return
	}
	visit(e.Type)
	switch e.Kind {
	case hir.ExprUnaryOp:
		data, ok := e.Data.(hir.UnaryOpData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Operand, visit)
	case hir.ExprBinaryOp:
		data, ok := e.Data.(hir.BinaryOpData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Left, visit)
		collectTypesFromExpr(data.Right, visit)
	case hir.ExprCall:
		data, ok := e.Data.(hir.CallData)
		if !ok {
			return
		}
		// Note: We intentionally skip collecting the callee's type here.
		// For tag constructors like Some(1), the callee (the "Some" identifier)
		// has its type set to the generic union type (e.g., Option<T>), which
		// contains type parameters. Since we have data.SymbolID for dispatch
		// and e.Type for the result, we don't need the callee's type.
		// Collecting it would cause "type parameter leaked" errors in mono.
		for _, a := range data.Args {
			collectTypesFromExpr(a, visit)
		}
	case hir.ExprFieldAccess:
		data, ok := e.Data.(hir.FieldAccessData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Object, visit)
	case hir.ExprIndex:
		data, ok := e.Data.(hir.IndexData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Object, visit)
		collectTypesFromExpr(data.Index, visit)
	case hir.ExprStructLit:
		data, ok := e.Data.(hir.StructLitData)
		if !ok {
			return
		}
		visit(data.TypeID)
		for _, f := range data.Fields {
			collectTypesFromExpr(f.Value, visit)
		}
	case hir.ExprArrayLit:
		data, ok := e.Data.(hir.ArrayLitData)
		if !ok {
			return
		}
		for _, el := range data.Elements {
			collectTypesFromExpr(el, visit)
		}
	case hir.ExprTupleLit:
		data, ok := e.Data.(hir.TupleLitData)
		if !ok {
			return
		}
		for _, el := range data.Elements {
			collectTypesFromExpr(el, visit)
		}
	case hir.ExprCompare:
		data, ok := e.Data.(hir.CompareData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Value, visit)
		for _, arm := range data.Arms {
			collectTypesFromExpr(arm.Pattern, visit)
			collectTypesFromExpr(arm.Guard, visit)
			collectTypesFromExpr(arm.Result, visit)
		}
	case hir.ExprTagTest:
		data, ok := e.Data.(hir.TagTestData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Value, visit)
	case hir.ExprTagPayload:
		data, ok := e.Data.(hir.TagPayloadData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Value, visit)
	case hir.ExprIterInit:
		data, ok := e.Data.(hir.IterInitData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Iterable, visit)
	case hir.ExprIterNext:
		data, ok := e.Data.(hir.IterNextData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Iter, visit)
	case hir.ExprIf:
		data, ok := e.Data.(hir.IfData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Cond, visit)
		collectTypesFromExpr(data.Then, visit)
		collectTypesFromExpr(data.Else, visit)
	case hir.ExprAwait:
		data, ok := e.Data.(hir.AwaitData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Value, visit)
	case hir.ExprTask:
		data, ok := e.Data.(hir.TaskData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Value, visit)
	case hir.ExprSpawn:
		data, ok := e.Data.(hir.SpawnData)
		if !ok {
			return
		}
		collectTypesFromExpr(data.Value, visit)
	case hir.ExprAsync:
		data, ok := e.Data.(hir.AsyncData)
		if !ok {
			return
		}
		collectTypesFromBlock(data.Body, visit)
	case hir.ExprCast:
		data, ok := e.Data.(hir.CastData)
		if !ok {
			return
		}
		visit(data.TargetTy)
		collectTypesFromExpr(data.Value, visit)
	case hir.ExprBlock:
		data, ok := e.Data.(hir.BlockExprData)
		if !ok {
			return
		}
		collectTypesFromBlock(data.Block, visit)
	default:
	}
}
