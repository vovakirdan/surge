package hir

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (l *lowerer) lowerSelectExpr(expr *ast.Expr, ty types.TypeID, isRace bool) *Expr {
	var selData *ast.ExprSelectData
	if isRace {
		selData = l.builder.Exprs.Races.Get(uint32(expr.Payload))
	} else {
		selData = l.builder.Exprs.Selects.Get(uint32(expr.Payload))
	}
	if selData == nil {
		return nil
	}

	arms := make([]SelectArm, len(selData.Arms))
	for i, arm := range selData.Arms {
		arms[i] = SelectArm{
			Await:     l.lowerExpr(arm.Await),
			Result:    l.lowerExpr(arm.Result),
			IsDefault: arm.IsDefault,
			Span:      arm.Span,
		}
	}

	kind := ExprSelect
	if isRace {
		kind = ExprRace
	}

	return &Expr{
		Kind: kind,
		Type: ty,
		Span: expr.Span,
		Data: SelectData{
			Arms: arms,
		},
	}
}

// lowerAwaitExpr lowers an await expression.
func (l *lowerer) lowerAwaitExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	awaitData := l.builder.Exprs.Awaits.Get(uint32(expr.Payload))
	if awaitData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprAwait,
		Type: ty,
		Span: expr.Span,
		Data: AwaitData{Value: l.lowerExpr(awaitData.Value)},
	}
}

// lowerTaskExpr lowers a task expression.
func (l *lowerer) lowerTaskExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	taskData := l.builder.Exprs.Tasks.Get(uint32(expr.Payload))
	if taskData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprTask,
		Type: ty,
		Span: expr.Span,
		Data: TaskData{Value: l.lowerExpr(taskData.Value)},
	}
}

// lowerSpawnExpr lowers a spawn expression.
func (l *lowerer) lowerSpawnExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	spawnData := l.builder.Exprs.Spawns.Get(uint32(expr.Payload))
	if spawnData == nil {
		return nil
	}

	return &Expr{
		Kind: ExprSpawn,
		Type: ty,
		Span: expr.Span,
		Data: SpawnData{Value: l.lowerExpr(spawnData.Value)},
	}
}

// lowerAsyncExpr lowers an async block expression.
func (l *lowerer) lowerAsyncExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	asyncData := l.builder.Exprs.Asyncs.Get(uint32(expr.Payload))
	if asyncData == nil {
		return nil
	}

	var body *Block
	if asyncData.Body.IsValid() {
		body = l.lowerBlockOrWrap(asyncData.Body)
	}
	l.markTailReturn(body)

	failfast := false
	if asyncData.AttrCount > 0 && asyncData.AttrStart.IsValid() {
		attrs := l.builder.Items.CollectAttrs(asyncData.AttrStart, asyncData.AttrCount)
		for _, attr := range attrs {
			if l.lookupString(attr.Name) == "failfast" {
				failfast = true
				break
			}
		}
	}

	return &Expr{
		Kind: ExprAsync,
		Type: ty,
		Span: expr.Span,
		Data: AsyncData{Body: body, Failfast: failfast},
	}
}

// lowerBlockingExpr lowers a blocking block expression.
func (l *lowerer) lowerBlockingExpr(exprID ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	blockingData := l.builder.Exprs.Blockings.Get(uint32(expr.Payload))
	if blockingData == nil {
		return nil
	}

	var body *Block
	if blockingData.Body.IsValid() {
		body = l.lowerBlockOrWrap(blockingData.Body)
	}
	l.markTailReturn(body)

	var captures []BlockingCapture
	if l.semaRes != nil && l.semaRes.BlockingCaptures != nil {
		if caps, ok := l.semaRes.BlockingCaptures[exprID]; ok {
			captures = l.blockingCaptureInfo(caps)
		}
	}

	return &Expr{
		Kind: ExprBlocking,
		Type: ty,
		Span: expr.Span,
		Data: BlockingData{Body: body, Captures: captures},
	}
}

// lowerCastExpr lowers a cast expression.
func (l *lowerer) lowerCastExpr(exprID ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	castData := l.builder.Exprs.Casts.Get(uint32(expr.Payload))
	if castData == nil {
		return nil
	}

	targetTy := l.lookupTypeFromAST(castData.Type)
	if l.semaRes != nil && l.semaRes.ToSymbols != nil {
		if symID, ok := l.semaRes.ToSymbols[exprID]; ok {
			return l.toCallExpr(expr.Span, l.lowerExpr(castData.Value), ty, symID)
		}
	}

	return &Expr{
		Kind: ExprCast,
		Type: ty,
		Span: expr.Span,
		Data: CastData{
			Value:    l.lowerExpr(castData.Value),
			TargetTy: targetTy,
		},
	}
}

// lowerBlockExpr lowers a block expression.
func (l *lowerer) lowerBlockExpr(expr *ast.Expr, ty types.TypeID) *Expr {
	blockData := l.builder.Exprs.Blocks.Get(uint32(expr.Payload))
	if blockData == nil {
		return nil
	}

	block := &Block{Span: expr.Span}
	for _, stmtID := range blockData.Stmts {
		if s := l.lowerStmt(stmtID); s != nil {
			block.Stmts = append(block.Stmts, *s)
		}
	}
	l.rewriteLegacyBlockTailRet(block, ty)

	return &Expr{
		Kind: ExprBlock,
		Type: ty,
		Span: expr.Span,
		Data: BlockExprData{Block: block},
	}
}

func (l *lowerer) blockingCaptureInfo(captures []symbols.SymbolID) []BlockingCapture {
	if len(captures) == 0 {
		return nil
	}
	out := make([]BlockingCapture, 0, len(captures))
	for _, symID := range captures {
		name := ""
		if l.symRes != nil && l.symRes.Table != nil && l.symRes.Table.Symbols != nil && l.strings != nil {
			if sym := l.symRes.Table.Symbols.Get(symID); sym != nil && sym.Name != source.NoStringID {
				name = l.strings.MustLookup(sym.Name)
			}
		}
		out = append(out, BlockingCapture{SymbolID: symID, Name: name})
	}
	return out
}
