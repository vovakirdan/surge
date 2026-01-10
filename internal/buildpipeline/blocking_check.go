package buildpipeline

import (
	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/hir"
	"surge/internal/source"
)

const blockingNotSupportedMsg = "blocking { } is not supported in the VM backend; VM is single-threaded and has no blocking pool"

func addBlockingVMErrors(req *CompileRequest, diagRes *driver.DiagnoseResult) {
	if req == nil || diagRes == nil || diagRes.Bag == nil || diagRes.HIR == nil {
		return
	}
	if req.Backend != BackendVM {
		return
	}
	spans := collectBlockingSpans(diagRes.HIR)
	if len(spans) == 0 {
		return
	}
	for _, sp := range spans {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutBlockingNotSupported,
			Message:  blockingNotSupportedMsg,
			Primary:  sp,
		})
	}
}

func collectBlockingSpans(mod *hir.Module) []source.Span {
	if mod == nil {
		return nil
	}
	var spans []source.Span
	var scanExpr func(*hir.Expr)
	var scanBlock func(*hir.Block)
	var scanStmt func(hir.Stmt)

	scanExpr = func(e *hir.Expr) {
		if e == nil {
			return
		}
		switch e.Kind {
		case hir.ExprUnaryOp:
			if data, ok := e.Data.(hir.UnaryOpData); ok {
				scanExpr(data.Operand)
			}
		case hir.ExprBinaryOp:
			if data, ok := e.Data.(hir.BinaryOpData); ok {
				scanExpr(data.Left)
				scanExpr(data.Right)
			}
		case hir.ExprCall:
			if data, ok := e.Data.(hir.CallData); ok {
				scanExpr(data.Callee)
				for _, arg := range data.Args {
					scanExpr(arg)
				}
			}
		case hir.ExprFieldAccess:
			if data, ok := e.Data.(hir.FieldAccessData); ok {
				scanExpr(data.Object)
			}
		case hir.ExprIndex:
			if data, ok := e.Data.(hir.IndexData); ok {
				scanExpr(data.Object)
				scanExpr(data.Index)
			}
		case hir.ExprStructLit:
			if data, ok := e.Data.(hir.StructLitData); ok {
				for _, field := range data.Fields {
					scanExpr(field.Value)
				}
			}
		case hir.ExprArrayLit:
			if data, ok := e.Data.(hir.ArrayLitData); ok {
				for _, elem := range data.Elements {
					scanExpr(elem)
				}
			}
		case hir.ExprMapLit:
			if data, ok := e.Data.(hir.MapLitData); ok {
				for _, entry := range data.Entries {
					scanExpr(entry.Key)
					scanExpr(entry.Value)
				}
			}
		case hir.ExprTupleLit:
			if data, ok := e.Data.(hir.TupleLitData); ok {
				for _, elem := range data.Elements {
					scanExpr(elem)
				}
			}
		case hir.ExprCompare:
			if data, ok := e.Data.(hir.CompareData); ok {
				scanExpr(data.Value)
				for _, arm := range data.Arms {
					scanExpr(arm.Pattern)
					scanExpr(arm.Guard)
					scanExpr(arm.Result)
				}
			}
		case hir.ExprSelect:
			if data, ok := e.Data.(hir.SelectData); ok {
				for _, arm := range data.Arms {
					scanExpr(arm.Await)
					scanExpr(arm.Result)
				}
			}
		case hir.ExprRace:
			if data, ok := e.Data.(hir.SelectData); ok {
				for _, arm := range data.Arms {
					scanExpr(arm.Await)
					scanExpr(arm.Result)
				}
			}
		case hir.ExprTagTest:
			if data, ok := e.Data.(hir.TagTestData); ok {
				scanExpr(data.Value)
			}
		case hir.ExprTagPayload:
			if data, ok := e.Data.(hir.TagPayloadData); ok {
				scanExpr(data.Value)
			}
		case hir.ExprIterInit:
			if data, ok := e.Data.(hir.IterInitData); ok {
				scanExpr(data.Iterable)
			}
		case hir.ExprIterNext:
			if data, ok := e.Data.(hir.IterNextData); ok {
				scanExpr(data.Iter)
			}
		case hir.ExprIf:
			if data, ok := e.Data.(hir.IfData); ok {
				scanExpr(data.Cond)
				scanExpr(data.Then)
				if data.Else != nil {
					scanExpr(data.Else)
				}
			}
		case hir.ExprAwait:
			if data, ok := e.Data.(hir.AwaitData); ok {
				scanExpr(data.Value)
			}
		case hir.ExprTask:
			if data, ok := e.Data.(hir.TaskData); ok {
				scanExpr(data.Value)
			}
		case hir.ExprSpawn:
			if data, ok := e.Data.(hir.SpawnData); ok {
				scanExpr(data.Value)
			}
		case hir.ExprAsync:
			if data, ok := e.Data.(hir.AsyncData); ok {
				scanBlock(data.Body)
			}
		case hir.ExprBlocking:
			spans = append(spans, e.Span)
			if data, ok := e.Data.(hir.BlockingData); ok {
				scanBlock(data.Body)
			}
		case hir.ExprCast:
			if data, ok := e.Data.(hir.CastData); ok {
				scanExpr(data.Value)
			}
		case hir.ExprBlock:
			if data, ok := e.Data.(hir.BlockExprData); ok {
				scanBlock(data.Block)
			}
		}
	}

	scanStmt = func(stmt hir.Stmt) {
		switch stmt.Kind {
		case hir.StmtLet:
			if data, ok := stmt.Data.(hir.LetData); ok {
				if data.Pattern != nil {
					scanExpr(data.Pattern)
				}
				scanExpr(data.Value)
			}
		case hir.StmtExpr:
			if data, ok := stmt.Data.(hir.ExprStmtData); ok {
				scanExpr(data.Expr)
			}
		case hir.StmtAssign:
			if data, ok := stmt.Data.(hir.AssignData); ok {
				scanExpr(data.Target)
				scanExpr(data.Value)
			}
		case hir.StmtReturn:
			if data, ok := stmt.Data.(hir.ReturnData); ok {
				scanExpr(data.Value)
			}
		case hir.StmtIf:
			if data, ok := stmt.Data.(hir.IfStmtData); ok {
				scanExpr(data.Cond)
				scanBlock(data.Then)
				if data.Else != nil {
					scanBlock(data.Else)
				}
			}
		case hir.StmtWhile:
			if data, ok := stmt.Data.(hir.WhileData); ok {
				scanExpr(data.Cond)
				scanBlock(data.Body)
			}
		case hir.StmtFor:
			if data, ok := stmt.Data.(hir.ForData); ok {
				switch data.Kind {
				case hir.ForClassic:
					if data.Init != nil {
						scanStmt(*data.Init)
					}
					scanExpr(data.Cond)
					scanExpr(data.Post)
				case hir.ForIn:
					scanExpr(data.Iterable)
				}
				scanBlock(data.Body)
			}
		case hir.StmtBlock:
			if data, ok := stmt.Data.(hir.BlockStmtData); ok {
				scanBlock(data.Block)
			}
		case hir.StmtDrop:
			if data, ok := stmt.Data.(hir.DropData); ok {
				scanExpr(data.Value)
			}
		}
	}

	scanBlock = func(block *hir.Block) {
		if block == nil {
			return
		}
		for _, stmt := range block.Stmts {
			scanStmt(stmt)
		}
	}

	for _, c := range mod.Consts {
		if c.Value != nil {
			scanExpr(c.Value)
		}
	}
	for _, g := range mod.Globals {
		if g.Value != nil {
			scanExpr(g.Value)
		}
	}
	for _, f := range mod.Funcs {
		if f != nil && f.Body != nil {
			scanBlock(f.Body)
		}
	}

	return spans
}
