package sema

import "surge/internal/ast"

func (a *taskProvenanceAnalyzer) walkStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) []taskProvenance {
	stmt := a.tc.builder.Stmts.Get(id)
	if stmt == nil {
		return nil
	}
	if returns, handled := a.walkBasicStmt(id, stmt.Kind, env, current, mode); handled {
		return returns
	}
	if returns, handled := a.walkReturnStmt(id, stmt.Kind, env, current, mode); handled {
		return returns
	}
	return a.walkFlowStmt(id, stmt.Kind, env, current, mode)
}

func (a *taskProvenanceAnalyzer) walkBasicStmt(
	id ast.StmtID,
	kind ast.StmtKind,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) ([]taskProvenance, bool) {
	switch kind {
	case ast.StmtBlock:
		return a.walkBlockStmt(id, env, current, mode), true
	case ast.StmtLet:
		a.walkLetStmt(id, env, current)
	case ast.StmtConst:
		a.walkConstStmt(id, env, current)
	case ast.StmtExpr:
		a.walkExprStmt(id, env, current)
	case ast.StmtSignal:
		a.walkSignalStmt(id, env, current)
	case ast.StmtDrop:
		a.walkDropStmt(id, env, current)
	default:
		return nil, false
	}
	return nil, true
}

func (a *taskProvenanceAnalyzer) walkBlockStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) []taskProvenance {
	block := a.tc.builder.Stmts.Block(id)
	if block == nil {
		return nil
	}
	var returns []taskProvenance
	for _, child := range block.Stmts {
		returns = append(returns, a.walkStmt(child, env, current, mode)...)
	}
	return returns
}

func (a *taskProvenanceAnalyzer) walkLetStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
) {
	let := a.tc.builder.Stmts.Let(id)
	if let == nil {
		return
	}
	origin := a.walkExpr(let.Value, env, current)
	if let.Pattern.IsValid() {
		a.bindPattern(let.Pattern, origin, env)
	} else if sym := a.tc.symbolForStmt(id); sym.IsValid() {
		env[sym] = origin
	}
}

func (a *taskProvenanceAnalyzer) walkConstStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
) {
	if value := a.tc.builder.Stmts.Const(id); value != nil {
		a.walkExpr(value.Value, env, current)
	}
}

func (a *taskProvenanceAnalyzer) walkExprStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
) {
	if value := a.tc.builder.Stmts.Expr(id); value != nil {
		a.walkExpr(value.Expr, env, current)
	}
}

func (a *taskProvenanceAnalyzer) walkSignalStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
) {
	if value := a.tc.builder.Stmts.Signal(id); value != nil {
		a.walkExpr(value.Value, env, current)
	}
}

func (a *taskProvenanceAnalyzer) walkDropStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
) {
	if drop := a.tc.builder.Stmts.Drop(id); drop != nil {
		a.walkExpr(drop.Expr, env, current)
	}
}

func (a *taskProvenanceAnalyzer) walkReturnStmt(
	id ast.StmtID,
	kind ast.StmtKind,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) ([]taskProvenance, bool) {
	switch kind {
	case ast.StmtReturn:
		return a.walkReturnValue(id, env, current, mode, taskReturnFunction), true
	case ast.StmtRet:
		return a.walkReturnValue(id, env, current, mode, taskReturnBlock), true
	default:
		return nil, false
	}
}

func (a *taskProvenanceAnalyzer) walkReturnValue(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
	want taskReturnMode,
) []taskProvenance {
	var expr ast.ExprID
	if want == taskReturnFunction {
		if value := a.tc.builder.Stmts.Return(id); value != nil {
			expr = value.Expr
		}
	} else if value := a.tc.builder.Stmts.Ret(id); value != nil {
		expr = value.Expr
	}
	if !expr.IsValid() {
		return nil
	}
	origin := a.walkExpr(expr, env, current)
	if mode != want {
		return nil
	}
	return []taskProvenance{origin}
}

func (a *taskProvenanceAnalyzer) walkFlowStmt(
	id ast.StmtID,
	kind ast.StmtKind,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) []taskProvenance {
	switch kind {
	case ast.StmtIf:
		return a.walkIfStmt(id, env, current, mode)
	case ast.StmtWhile:
		return a.walkWhileStmt(id, env, current, mode)
	case ast.StmtForClassic:
		return a.walkForClassicStmt(id, env, current, mode)
	case ast.StmtForIn:
		return a.walkForInStmt(id, env, current, mode)
	default:
		return nil
	}
}

func (a *taskProvenanceAnalyzer) walkIfStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) []taskProvenance {
	branch := a.tc.builder.Stmts.If(id)
	if branch == nil {
		return nil
	}
	a.walkExpr(branch.Cond, env, current)
	base := cloneTaskEnv(env)
	thenEnv := cloneTaskEnv(base)
	returns := a.walkStmt(branch.Then, thenEnv, current, mode)
	elseEnv := cloneTaskEnv(base)
	if branch.Else.IsValid() {
		returns = append(returns, a.walkStmt(branch.Else, elseEnv, current, mode)...)
	}
	mergeTaskEnvInto(env, base, thenEnv, elseEnv)
	return returns
}

func (a *taskProvenanceAnalyzer) walkWhileStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) []taskProvenance {
	loop := a.tc.builder.Stmts.While(id)
	if loop == nil {
		return nil
	}
	a.walkExpr(loop.Cond, env, current)
	base := cloneTaskEnv(env)
	bodyEnv := cloneTaskEnv(base)
	returns := a.walkStmt(loop.Body, bodyEnv, current, mode)
	mergeTaskEnvInto(env, base, base, bodyEnv)
	return returns
}

func (a *taskProvenanceAnalyzer) walkForClassicStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) []taskProvenance {
	loop := a.tc.builder.Stmts.ForClassic(id)
	if loop == nil {
		return nil
	}
	returns := a.walkStmt(loop.Init, env, current, mode)
	a.walkExpr(loop.Cond, env, current)
	base := cloneTaskEnv(env)
	bodyEnv := cloneTaskEnv(base)
	returns = append(returns, a.walkStmt(loop.Body, bodyEnv, current, mode)...)
	a.walkExpr(loop.Post, bodyEnv, current)
	mergeTaskEnvInto(env, base, base, bodyEnv)
	return returns
}

func (a *taskProvenanceAnalyzer) walkForInStmt(
	id ast.StmtID,
	env taskProvenanceEnv,
	current taskCreationScope,
	mode taskReturnMode,
) []taskProvenance {
	loop := a.tc.builder.Stmts.ForIn(id)
	if loop == nil {
		return nil
	}
	a.walkExpr(loop.Iterable, env, current)
	base := cloneTaskEnv(env)
	bodyEnv := cloneTaskEnv(base)
	returns := a.walkStmt(loop.Body, bodyEnv, current, mode)
	mergeTaskEnvInto(env, base, base, bodyEnv)
	return returns
}

func mergeTaskEnvInto(
	dst taskProvenanceEnv,
	base taskProvenanceEnv,
	left taskProvenanceEnv,
	right taskProvenanceEnv,
) {
	for sym, origin := range mergeTaskEnvs(base, left, right) {
		dst[sym] = origin
	}
}
