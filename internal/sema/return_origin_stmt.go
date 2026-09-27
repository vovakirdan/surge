package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *returnOriginBody) sequence(stmts []ast.StmtID, env returnOriginEnv, targets returnOriginTargets) (returnOriginFlow, error) {
	flow := returnOriginFlow{normal: env}
	for _, id := range stmts {
		var err error
		flow, err = flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
			return b.stmt(id, next, targets)
		})
		if err != nil {
			return returnOriginFlow{}, err
		}
	}
	return flow, nil
}

func (b *returnOriginBody) stmt(id ast.StmtID, env returnOriginEnv, targets returnOriginTargets) (returnOriginFlow, error) {
	if !env.reachable {
		return returnOriginFlow{}, nil
	}
	if err := b.analyzer.ctx.Err(); err != nil {
		return returnOriginFlow{}, err
	}
	u := b.function.unit
	node := u.Builder.Stmts.Get(id)
	if node == nil {
		return returnOriginFlow{}, fmt.Errorf("return origins: missing statement %d", id)
	}
	switch node.Kind {
	case ast.StmtBlock:
		scope := u.stmtScopes[id]
		if !scope.IsValid() {
			return returnOriginFlow{}, fmt.Errorf("return origins: block %d has no owning scope", id)
		}
		targets.scope = scope
		flow, err := b.sequence(u.Builder.Stmts.Block(id).Stmts, env, targets)
		if err != nil {
			return returnOriginFlow{}, err
		}
		return b.closeFlow(flow, scope, node.Span), nil
	case ast.StmtLet, ast.StmtConst:
		var value ast.ExprID
		var annotation ast.TypeID
		if node.Kind == ast.StmtLet {
			decl := u.Builder.Stmts.Let(id)
			value, annotation = decl.Value, decl.Type
			if decl.Pattern.IsValid() {
				b.pending(node.Span, "destructuring needs projected origin facts")
			}
		} else {
			decl := u.Builder.Stmts.Const(id)
			value, annotation = decl.Value, decl.Type
		}
		out := originExprValue(env, returnOriginValueOf())
		if value.IsValid() {
			var err error
			out, err = b.expr(value, env, targets)
			if err != nil {
				return returnOriginFlow{}, err
			}
		} else if decl := u.Builder.Stmts.Let(id); node.Kind == ast.StmtLet && !decl.Pattern.IsValid() && decl.Type.IsValid() {
			// HIR synthesizes `default::<T>()` for exactly this shape (internal/hir/lower_stmt.go:204-205).
			out.value = b.defaultInitValue(id, node.Span)
		} else {
			b.pending(node.Span, "uninitialized binding has no proven reference contents")
			out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
		id := u.stmtSymbols[id]
		if id.IsValid() {
			sym := u.Symbols.Table.Symbols.Get(id)
			if value.IsValid() && out.flow.normal.reachable {
				out.value = b.bindCallable(out.value, id, annotation, value, returnOriginValue{}, false)
			}
			if node.Kind == ast.StmtLet && !value.IsValid() && annotation.IsValid() {
				out.flow.normal = out.flow.normal.assignDefault(id, sym.Scope, out.value)
			} else {
				out.flow.normal = out.flow.normal.assign(id, sym.Scope, out.value)
			}
		}
		return out.flow, nil
	case ast.StmtExpr:
		out, err := b.expr(u.Builder.Stmts.Expr(id).Expr, env, targets)
		return out.flow, err
	case ast.StmtReturn, ast.StmtRet:
		var value ast.ExprID
		key := returnOriginExit{kind: returnOriginFunctionReturn, target: b.function.scope, site: node.Span}
		if node.Kind == ast.StmtRet {
			value = u.Builder.Stmts.Ret(id).Expr
		} else {
			value = u.Builder.Stmts.Return(id).Expr
		}
		// A parser-synthesized arm tail is its block's value, exactly as sema and HIR read it.
		function := node.Kind == ast.StmtReturn && (!targets.block.IsValid() || explicitReturnStmt(u.Builder, id))
		if !function {
			key.kind, key.target = returnOriginBlockResult, targets.block
		}
		if !key.target.IsValid() {
			return returnOriginFlow{}, fmt.Errorf("return origins: return at %v has no target", node.Span)
		}
		out := originExprValue(env, returnOriginValueOf())
		if value.IsValid() {
			var err error
			out, err = b.expr(value, env, targets)
			if err != nil {
				return returnOriginFlow{}, err
			}
		}
		if function && out.flow.normal.reachable &&
			(len(out.value.callables) != 0 || returnOriginFnInfo(u.Sema.TypeInterner, b.function.info.Result) != nil) {
			b.pending(node.Span, "callable return conversion needs its destination and capture contract")
			out.value = out.value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
		}
		if function && b.canLoadScalarReturn(value, out.value, out.flow.normal, targets.scope) {
			out.value = returnOriginValueOf()
		}
		return out.flow.end(key, out.value), nil
	case ast.StmtBreak, ast.StmtContinue:
		if !targets.loop.IsValid() {
			return returnOriginFlow{}, fmt.Errorf("return origins: loop exit at %v has no target", node.Span)
		}
		kind := returnOriginBreak
		if node.Kind == ast.StmtContinue {
			kind = returnOriginContinue
		}
		flow := returnOriginFlow{normal: env}
		return flow.end(returnOriginExit{kind: kind, target: targets.loop, site: node.Span}, returnOriginValueOf()), nil
	case ast.StmtIf:
		data := u.Builder.Stmts.If(id)
		return b.ifStmt(data, env, targets)
	case ast.StmtWhile:
		data := u.Builder.Stmts.While(id)
		return b.loop(id, data.Cond, ast.NoExprID, data.Body, env, targets)
	case ast.StmtForClassic:
		data := u.Builder.Stmts.ForClassic(id)
		return b.classicLoop(id, data, env, targets)
	case ast.StmtForIn:
		return b.forIn(id, env, targets)
	case ast.StmtDrop:
		target := u.Builder.Stmts.Drop(id).Expr
		out, err := b.expr(target, env, targets)
		if err != nil {
			return out.flow, err
		}
		owner, releases, admitted := b.droppedBinding(target)
		if !admitted {
			b.pending(node.Span, "explicit drop needs owner-incarnation invalidation")
			return out.flow, nil
		}
		if releases && out.flow.normal.reachable {
			out.flow.normal = out.flow.normal.expireBinding(owner)
		}
		return out.flow, nil
	default:
		b.pending(node.Span, fmt.Sprintf("statement kind %d needs an origin transfer", node.Kind))
		return returnOriginFlow{normal: env}, nil
	}
}

// MIR loads a scalar return through its reference before exit drops. Only
// proven live origins may become the copied value; unresolved storage must
// remain visible to the ordinary scope-exit checks.
func (b *returnOriginBody) canLoadScalarReturn(expr ast.ExprID, value returnOriginValue, env returnOriginEnv, scope symbols.ScopeID) bool {
	fn, u := b.function, b.function.unit
	in := u.Sema.TypeInterner
	if !expr.IsValid() || !env.reachable || !value.normal || len(value.roots) == 0 || len(value.callables) != 0 {
		return false
	}
	expected := resolveAlias(in, fn.info.Result)
	actual, ok := in.Lookup(resolveAlias(in, u.Sema.ExprTypes[expr]))
	if !ok || actual.Kind != types.KindReference || resolveAlias(in, actual.Elem) != expected || !u.Sema.IsCopyType(expected) {
		return false
	}
	want, ok := in.Lookup(expected)
	if !ok {
		return false
	}
	switch want.Kind {
	case types.KindBool, types.KindInt, types.KindUint, types.KindFloat:
	default:
		return false
	}
	if _, converted := u.Sema.ImplicitConversions[expr]; converted {
		return false
	}
	for _, root := range value.roots {
		if root.expired {
			return false
		}
		id, ownerScope := root.binding, root.scope
		switch root.kind {
		case returnOriginParam:
			if int64(root.param) >= int64(len(fn.params)) || returnOriginTypeShape(in, fn.info.Params[root.param], nil) != returnOriginCarriesRef {
				return false
			}
			id, ownerScope = fn.params[root.param], fn.scope
		case returnOriginLocal:
		default:
			return false
		}
		symbol := u.Symbols.Table.Symbols.Get(id)
		binding, live := env.bindings[id]
		if symbol == nil || !live || symbol.Scope != ownerScope || binding.scope != ownerScope ||
			symbol.Decl.ASTFile != u.FileID || symbol.Decl.SourceFile != fn.item.NameSpan.File ||
			!b.within(ownerScope, fn.scope) || !b.within(scope, ownerScope) ||
			(root.kind == returnOriginParam && symbol.Kind != symbols.SymbolParam) {
			return false
		}
	}
	return true
}

func (b *returnOriginBody) ifStmt(data *ast.IfStmt, env returnOriginEnv, targets returnOriginTargets) (returnOriginFlow, error) {
	cond, err := b.expr(data.Cond, env, targets)
	if err != nil || !cond.flow.normal.reachable {
		return cond.flow, err
	}
	if b.literalBool(data.Cond, true) {
		branch, branchErr := b.stmt(data.Then, cond.flow.normal, targets)
		cond.flow.normal = returnOriginEnv{}
		return cond.flow.join(branch), branchErr
	}
	if b.literalBool(data.Cond, false) {
		if !data.Else.IsValid() {
			return cond.flow, nil
		}
		branch, branchErr := b.stmt(data.Else, cond.flow.normal, targets)
		cond.flow.normal = returnOriginEnv{}
		return cond.flow.join(branch), branchErr
	}
	left, err := b.stmt(data.Then, cond.flow.normal.clone(), targets)
	if err != nil {
		return returnOriginFlow{}, err
	}
	right := returnOriginFlow{normal: cond.flow.normal.clone()}
	if data.Else.IsValid() {
		right, err = b.stmt(data.Else, right.normal, targets)
		if err != nil {
			return returnOriginFlow{}, err
		}
	}
	cond.flow.normal = returnOriginEnv{}
	return cond.flow.join(left).join(right), nil
}

func (b *returnOriginBody) loop(id ast.StmtID, condID, postID ast.ExprID, bodyID ast.StmtID, env returnOriginEnv, targets returnOriginTargets) (returnOriginFlow, error) {
	u := b.function.unit
	target := u.stmtScopes[id]
	if !target.IsValid() {
		// While owns no extra resolver scope; its body supplies a unique loop target.
		target = u.stmtScopes[bodyID]
	}
	if !target.IsValid() {
		return returnOriginFlow{}, fmt.Errorf("return origins: loop at %v has no body scope", u.Builder.Stmts.Get(id).Span)
	}
	targets.loop = target
	return solveReturnOriginLoop(b.analyzer.ctx, env, target, func(header returnOriginEnv) (returnOriginLoopStep, error) {
		cond := originExprValue(header, returnOriginValueOf())
		if condID.IsValid() {
			var err error
			cond, err = b.expr(condID, header, targets)
			if err != nil {
				return returnOriginLoopStep{}, err
			}
		}
		done := cond.flow.normal.clone()
		if !condID.IsValid() || b.literalBool(condID, true) {
			done = returnOriginEnv{}
		}
		body := returnOriginFlow{}
		if !b.literalBool(condID, false) {
			var err error
			body, err = b.stmt(bodyID, cond.flow.normal, targets)
			if err != nil {
				return returnOriginLoopStep{}, err
			}
			if postID.IsValid() {
				body, err = b.loopPost(body, postID, target, targets)
				if err != nil {
					return returnOriginLoopStep{}, err
				}
			}
		}
		cond.flow.normal = returnOriginEnv{}
		return returnOriginLoopStep{done: done, body: cond.flow.join(body)}, nil
	})
}

func (b *returnOriginBody) literalBool(id ast.ExprID, want bool) bool {
	if !id.IsValid() {
		return false
	}
	if group, ok := b.function.unit.Builder.Exprs.Group(id); ok && group != nil {
		return b.literalBool(group.Inner, want)
	}
	data, ok := b.function.unit.Builder.Exprs.Literal(id)
	return ok && data != nil && ((want && data.Kind == ast.ExprLitTrue) || (!want && data.Kind == ast.ExprLitFalse))
}

func (b *returnOriginBody) classicLoop(id ast.StmtID, data *ast.ForClassicStmt, env returnOriginEnv, targets returnOriginTargets) (returnOriginFlow, error) {
	flow := returnOriginFlow{normal: env}
	if scope := b.function.unit.stmtScopes[id]; scope.IsValid() {
		targets.scope = scope
	}
	if data.Init.IsValid() {
		var err error
		flow, err = b.stmt(data.Init, env, targets)
		if err != nil {
			return returnOriginFlow{}, err
		}
	}
	flow, err := flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
		return b.loop(id, data.Cond, data.Post, data.Body, next, targets)
	})
	if err == nil {
		if scope := b.function.unit.stmtScopes[id]; scope.IsValid() {
			flow = b.closeFlow(flow, scope, b.function.unit.Builder.Stmts.Get(id).Span)
		}
	}
	return flow, err
}

func (b *returnOriginBody) loopPost(flow returnOriginFlow, post ast.ExprID, target symbols.ScopeID, targets returnOriginTargets) (returnOriginFlow, error) {
	for key, exit := range flow.exits {
		if key.kind == returnOriginContinue && key.target == target {
			flow.normal = flow.normal.join(exit.env)
			delete(flow.exits, key)
		}
	}
	return flow.then(func(env returnOriginEnv) (returnOriginFlow, error) {
		out, err := b.expr(post, env, targets)
		return out.flow, err
	})
}
