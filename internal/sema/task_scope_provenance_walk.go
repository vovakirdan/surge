package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
)

func (a *taskProvenanceAnalyzer) walkExpr(
	id ast.ExprID,
	env taskProvenanceEnv,
	current taskCreationScope,
) taskProvenance {
	if !id.IsValid() {
		return taskProvenance{}
	}
	expr := a.tc.builder.Exprs.Get(id)
	if expr == nil {
		return taskProvenance{}
	}
	if origin, handled := a.walkValueExpr(id, expr, env, current); handled {
		return origin
	}
	if origin, handled := a.walkTaskControlExpr(id, expr, env, current); handled {
		return origin
	}
	a.walkTraversalExpr(id, expr, env, current)
	return taskProvenance{}
}

func (a *taskProvenanceAnalyzer) walkValueExpr(
	id ast.ExprID,
	expr *ast.Expr,
	env taskProvenanceEnv,
	current taskCreationScope,
) (taskProvenance, bool) {
	if origin, handled := a.walkPrimaryValueExpr(id, expr.Kind, env, current); handled {
		return origin, true
	}
	if origin, handled := a.walkWrappedValueExpr(id, expr.Kind, env, current); handled {
		return origin, true
	}
	return a.walkTupleValueExpr(id, expr.Kind, env, current)
}

func (a *taskProvenanceAnalyzer) walkPrimaryValueExpr(
	id ast.ExprID,
	kind ast.ExprKind,
	env taskProvenanceEnv,
	current taskCreationScope,
) (taskProvenance, bool) {
	switch kind {
	case ast.ExprIdent:
		return env[a.tc.symbolForExpr(id)], true
	case ast.ExprCall:
		return a.walkCall(id, env, current), true
	case ast.ExprBinary:
		if data, ok := a.tc.builder.Exprs.Binary(id); ok && data != nil {
			if data.Op == ast.ExprBinaryAssign {
				a.walkExpr(data.Left, env, current)
				origin := a.walkExpr(data.Right, env, current)
				a.bindPattern(data.Left, origin, env)
				return origin, true
			}
			a.walkExpr(data.Left, env, current)
			a.walkExpr(data.Right, env, current)
		}
	default:
		return taskProvenance{}, false
	}
	return taskProvenance{}, true
}

func (a *taskProvenanceAnalyzer) walkWrappedValueExpr(
	id ast.ExprID,
	kind ast.ExprKind,
	env taskProvenanceEnv,
	current taskCreationScope,
) (taskProvenance, bool) {
	switch kind {
	case ast.ExprUnary:
		if data, ok := a.tc.builder.Exprs.Unary(id); ok && data != nil {
			return a.walkExpr(data.Operand, env, current), true
		}
	case ast.ExprCast:
		if data, ok := a.tc.builder.Exprs.Cast(id); ok && data != nil {
			return a.walkExpr(data.Value, env, current), true
		}
	case ast.ExprGroup:
		if data, ok := a.tc.builder.Exprs.Group(id); ok && data != nil {
			return a.walkExpr(data.Inner, env, current), true
		}
	case ast.ExprMember:
		if data, ok := a.tc.builder.Exprs.Member(id); ok && data != nil {
			return a.walkExpr(data.Target, env, current), true
		}
	case ast.ExprSpread:
		if data, ok := a.tc.builder.Exprs.Spread(id); ok && data != nil {
			return a.walkExpr(data.Value, env, current), true
		}
	default:
		return taskProvenance{}, false
	}
	return taskProvenance{}, true
}

func (a *taskProvenanceAnalyzer) walkTupleValueExpr(
	id ast.ExprID,
	kind ast.ExprKind,
	env taskProvenanceEnv,
	current taskCreationScope,
) (taskProvenance, bool) {
	switch kind {
	case ast.ExprTuple:
		if data, ok := a.tc.builder.Exprs.Tuple(id); ok && data != nil {
			elems := make([]taskProvenance, len(data.Elements))
			for i, elem := range data.Elements {
				elems[i] = a.walkExpr(elem, env, current)
			}
			return tupleTaskOrigin(elems), true
		}
	case ast.ExprTupleIndex:
		if data, ok := a.tc.builder.Exprs.TupleIndex(id); ok && data != nil {
			origin := a.walkExpr(data.Target, env, current)
			if origin.kind == taskProvenanceTuple && int(data.Index) < len(origin.elems) {
				return origin.elems[data.Index], true
			}
		}
	default:
		return taskProvenance{}, false
	}
	return taskProvenance{}, true
}

func (a *taskProvenanceAnalyzer) walkTaskControlExpr(
	id ast.ExprID,
	expr *ast.Expr,
	env taskProvenanceEnv,
	current taskCreationScope,
) (taskProvenance, bool) {
	switch expr.Kind {
	case ast.ExprTernary:
		if data, ok := a.tc.builder.Exprs.Ternary(id); ok && data != nil {
			a.walkExpr(data.Cond, env, current)
			left := a.walkExpr(data.TrueExpr, cloneTaskEnv(env), current)
			right := a.walkExpr(data.FalseExpr, cloneTaskEnv(env), current)
			return mergeTaskProvenance(left, right), true
		}
	case ast.ExprAsync:
		if data, ok := a.tc.builder.Exprs.Async(id); ok && data != nil {
			a.walkStmt(data.Body, cloneTaskEnv(env), a.freshScope(), taskReturnIgnore)
			return taskOrigin(current, expr.Span), true
		}
	case ast.ExprBlocking:
		if data, ok := a.tc.builder.Exprs.Blocking(id); ok && data != nil {
			a.walkStmt(data.Body, cloneTaskEnv(env), a.freshScope(), taskReturnIgnore)
			return taskOrigin(current, expr.Span), true
		}
	case ast.ExprTask, ast.ExprSpawn:
		return a.walkSpawnLike(id, env, current), true
	case ast.ExprCompare:
		return a.walkCompare(id, env, current), true
	case ast.ExprSelect, ast.ExprRace:
		return a.walkSelect(id, env, current), true
	case ast.ExprBlock:
		if data, ok := a.tc.builder.Exprs.Block(id); ok && data != nil {
			var returns []taskProvenance
			for _, stmt := range data.Stmts {
				returns = append(returns, a.walkStmt(stmt, env, current, taskReturnBlock)...)
			}
			return mergeTaskProvenances(returns), true
		}
	default:
		return taskProvenance{}, false
	}
	return taskProvenance{}, true
}

func (a *taskProvenanceAnalyzer) walkTraversalExpr(
	id ast.ExprID,
	expr *ast.Expr,
	env taskProvenanceEnv,
	current taskCreationScope,
) {
	if a.walkCollectionExpr(id, expr.Kind, env, current) {
		return
	}
	if a.walkAccessExpr(id, expr.Kind, env, current) {
		return
	}
	a.walkConcurrentExpr(id, expr.Kind, env, current)
}

func (a *taskProvenanceAnalyzer) walkCollectionExpr(
	id ast.ExprID,
	kind ast.ExprKind,
	env taskProvenanceEnv,
	current taskCreationScope,
) bool {
	switch kind {
	case ast.ExprArray:
		if data, ok := a.tc.builder.Exprs.Array(id); ok && data != nil {
			for _, elem := range data.Elements {
				a.walkExpr(elem, env, current)
			}
		}
	case ast.ExprMap:
		if data, ok := a.tc.builder.Exprs.Map(id); ok && data != nil {
			for _, entry := range data.Entries {
				a.walkExpr(entry.Key, env, current)
				a.walkExpr(entry.Value, env, current)
			}
		}
	case ast.ExprStruct:
		if data, ok := a.tc.builder.Exprs.Struct(id); ok && data != nil {
			for _, field := range data.Fields {
				a.walkExpr(field.Value, env, current)
			}
		}
	default:
		return false
	}
	return true
}

func (a *taskProvenanceAnalyzer) walkAccessExpr(
	id ast.ExprID,
	kind ast.ExprKind,
	env taskProvenanceEnv,
	current taskCreationScope,
) bool {
	switch kind {
	case ast.ExprIndex:
		if data, ok := a.tc.builder.Exprs.Index(id); ok && data != nil {
			a.walkExpr(data.Target, env, current)
			a.walkExpr(data.Index, env, current)
		}
	case ast.ExprAwait:
		if data, ok := a.tc.builder.Exprs.Await(id); ok && data != nil {
			a.walkExpr(data.Value, env, current)
		}
	case ast.ExprRangeLit:
		if data, ok := a.tc.builder.Exprs.RangeLit(id); ok && data != nil {
			a.walkExpr(data.Start, env, current)
			a.walkExpr(data.End, env, current)
		}
	default:
		return false
	}
	return true
}

func (a *taskProvenanceAnalyzer) walkConcurrentExpr(
	id ast.ExprID,
	kind ast.ExprKind,
	env taskProvenanceEnv,
	current taskCreationScope,
) {
	switch kind {
	case ast.ExprOn:
		if data, ok := a.tc.builder.Exprs.On(id); ok && data != nil {
			a.walkExpr(data.Dest, env, current)
			a.walkStmt(data.Body, cloneTaskEnv(env), a.freshScope(), taskReturnIgnore)
		}
	case ast.ExprParallel:
		if data, ok := a.tc.builder.Exprs.Parallel(id); ok && data != nil {
			a.walkExpr(data.Iterable, env, current)
			a.walkExpr(data.Init, env, current)
			for _, arg := range data.Args {
				a.walkExpr(arg, env, current)
			}
			a.walkExpr(data.Body, env, current)
		}
	}
}

func (a *taskProvenanceAnalyzer) walkSpawnLike(
	id ast.ExprID,
	env taskProvenanceEnv,
	current taskCreationScope,
) taskProvenance {
	var value ast.ExprID
	if data, ok := a.tc.builder.Exprs.Spawn(id); ok && data != nil {
		value = data.Value
	} else if data, ok := a.tc.builder.Exprs.Task(id); ok && data != nil {
		value = data.Value
	}
	origin := a.walkExpr(value, env, current)
	if outside, found := outsideTaskOrigin(origin, current); found {
		a.reportOutsideScope(a.tc.exprSpan(id), outside)
	}
	return origin
}

func (a *taskProvenanceAnalyzer) walkCall(
	id ast.ExprID,
	env taskProvenanceEnv,
	current taskCreationScope,
) taskProvenance {
	call, ok := a.tc.builder.Exprs.Call(id)
	if !ok || call == nil {
		return taskProvenance{}
	}
	targetOrigin := a.walkExpr(call.Target, env, current)
	args := make([]taskProvenance, len(call.Args))
	for i, arg := range call.Args {
		args[i] = a.walkExpr(arg.Value, env, current)
	}
	if member, ok := a.tc.builder.Exprs.Member(call.Target); ok && member != nil &&
		a.tc.lookupName(member.Field) == "clone" {
		return targetOrigin
	}
	symID := a.tc.symbolForExpr(id)
	fn, itemID := a.functionForSymbol(symID)
	if fn == nil {
		return taskProvenance{}
	}
	if fn.Flags&ast.FnModifierAsync != 0 {
		a.analyzeFunction(itemID, fn, a.freshScope(), args)
		return taskOrigin(current, a.tc.exprSpan(id))
	}
	return a.analyzeFunction(itemID, fn, current, args)
}

func (a *taskProvenanceAnalyzer) functionForSymbol(symID symbols.SymbolID) (*ast.FnItem, ast.ItemID) {
	if !symID.IsValid() || a.tc.symbols == nil || a.tc.symbols.Table == nil ||
		a.tc.symbols.Table.Symbols == nil {
		return nil, ast.NoItemID
	}
	sym := a.tc.symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Kind != symbols.SymbolFunction || !sym.Decl.Item.IsValid() {
		return nil, ast.NoItemID
	}
	fn, ok := a.tc.builder.Items.Fn(sym.Decl.Item)
	if !ok {
		return nil, ast.NoItemID
	}
	return fn, sym.Decl.Item
}

func (a *taskProvenanceAnalyzer) bindPattern(
	pattern ast.ExprID,
	origin taskProvenance,
	env taskProvenanceEnv,
) {
	pattern = a.tc.unwrapGroupExpr(pattern)
	node := a.tc.builder.Exprs.Get(pattern)
	if node == nil {
		return
	}
	switch node.Kind {
	case ast.ExprIdent:
		if sym := a.tc.symbolForExpr(pattern); sym.IsValid() {
			env[sym] = origin
		}
	case ast.ExprTuple:
		data, ok := a.tc.builder.Exprs.Tuple(pattern)
		if !ok || data == nil {
			return
		}
		for i, elem := range data.Elements {
			value := taskProvenance{}
			if origin.kind == taskProvenanceTuple && i < len(origin.elems) {
				value = origin.elems[i]
			}
			a.bindPattern(elem, value, env)
		}
	}
}

func (a *taskProvenanceAnalyzer) walkCompare(
	id ast.ExprID,
	env taskProvenanceEnv,
	current taskCreationScope,
) taskProvenance {
	data, ok := a.tc.builder.Exprs.Compare(id)
	if !ok || data == nil {
		return taskProvenance{}
	}
	a.walkExpr(data.Value, env, current)
	values := make([]taskProvenance, 0, len(data.Arms))
	for _, arm := range data.Arms {
		armEnv := cloneTaskEnv(env)
		a.walkExpr(arm.Guard, armEnv, current)
		values = append(values, a.walkExpr(arm.Result, armEnv, current))
	}
	return mergeTaskProvenances(values)
}

func (a *taskProvenanceAnalyzer) walkSelect(
	id ast.ExprID,
	env taskProvenanceEnv,
	current taskCreationScope,
) taskProvenance {
	data, ok := a.tc.builder.Exprs.Select(id)
	if !ok || data == nil {
		data, ok = a.tc.builder.Exprs.Race(id)
	}
	if !ok || data == nil {
		return taskProvenance{}
	}
	values := make([]taskProvenance, 0, len(data.Arms))
	for _, arm := range data.Arms {
		a.walkExpr(arm.Await, env, current)
		values = append(values, a.walkExpr(arm.Result, cloneTaskEnv(env), current))
	}
	return mergeTaskProvenances(values)
}
