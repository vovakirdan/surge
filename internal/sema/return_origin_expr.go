package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func originExprValue(env returnOriginEnv, value returnOriginValue) returnOriginExprResult {
	if !value.normal {
		env = returnOriginEnv{}
	}
	return returnOriginExprResult{flow: returnOriginFlow{normal: env}, value: value}
}

func (b *returnOriginBody) exprCore(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	if !env.reachable {
		return returnOriginExprResult{}, nil
	}
	if err := b.analyzer.ctx.Err(); err != nil {
		return returnOriginExprResult{}, err
	}
	u := b.function.unit
	if !id.IsValid() {
		return returnOriginExprResult{}, fmt.Errorf("return origins: missing expression in %s", u.SourceKey)
	}
	node := u.Builder.Exprs.Get(id)
	if node == nil || u.Sema.ExprTypes[id] == types.NoTypeID {
		return returnOriginExprResult{}, fmt.Errorf("return origins: expression %d is not typed in %s", id, u.SourceKey)
	}
	switch node.Kind {
	case ast.ExprIdent:
		symID := u.Symbols.ExprSymbols[id]
		sym := u.Symbols.Table.Symbols.Get(symID)
		if sym == nil {
			return b.unknownExpr(env, node.Span, "identifier has no resolved symbol"), nil
		}
		if returnOriginFnInfo(u.Sema.TypeInterner, u.Sema.ExprTypes[id]) != nil || sym.Kind == symbols.SymbolFunction {
			return b.callableIdent(id, env), nil
		}
		value := env.value(symID)
		if b.shape(id) == returnOriginRefFree && !b.analyzer.loanCarrier(sym.Type) {
			value = returnOriginValueOf()
		}
		if !b.within(sym.Scope, b.function.scope) {
			return b.unknownExpr(env, node.Span, "captured binding requires origin finalization"), nil
		}
		b.checkExpired(value, node.Span)
		out := originExprValue(env, value)
		out.storage = returnOriginValueOf(returnOrigin{kind: returnOriginLocal, binding: symID, scope: sym.Scope})
		return out, nil
	case ast.ExprLit:
		if b.shape(id) != returnOriginRefFree {
			return b.unknownExpr(env, node.Span, "literal has an unresolved reference-bearing type"), nil
		}
		return originExprValue(env, returnOriginValueOf()), nil
	case ast.ExprGroup:
		data, _ := u.Builder.Exprs.Group(id)
		return b.expr(data.Inner, env, targets)
	case ast.ExprUnary:
		return b.unary(id, env, targets)
	case ast.ExprBinary:
		return b.binary(id, env, targets)
	case ast.ExprBlock:
		return b.blockExpr(id, env, targets)
	case ast.ExprTernary:
		return b.ternary(id, env, targets)
	case ast.ExprCompare:
		return b.compareExpr(id, env, targets)
	case ast.ExprCall:
		return b.call(id, env, targets)
	case ast.ExprIndex:
		return b.index(id, env, targets)
	case ast.ExprRangeLit:
		return b.rangeLiteral(id, env, targets)
	case ast.ExprArray, ast.ExprTuple, ast.ExprStruct:
		return b.constructor(id, env, targets)
	case ast.ExprMember:
		data, _ := u.Builder.Exprs.Member(id)
		// A module-qualified free function is a function value, not a projection:
		// its target is syntax naming a module and has no runtime value to read.
		if target := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[data.Target]); target != nil && target.Kind == symbols.SymbolModule {
			selected := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[id])
			if selected != nil && selected.Kind == symbols.SymbolFunction && selected.Signature != nil && !selected.Signature.HasSelf &&
				selected.ReceiverKey == "" && selected.Flags&symbols.SymbolFlagMethod == 0 && selected.Type == u.Sema.ExprTypes[id] {
				return b.callableIdent(id, env), nil
			}
			return b.unknownExpr(env, node.Span, "module member value needs its selected free-function authority"), nil
		}
		out, err := b.expr(data.Target, env, targets)
		if err != nil || !out.flow.normal.reachable {
			return out, err
		}
		if b.shape(data.Target) == returnOriginCarriesRef {
			out.storage = out.value.clone()
		}
		if b.shape(id) == returnOriginRefFree {
			out.value = returnOriginValueOf()
		} else if b.memberBorrowsReferent(id, data) {
			out.value = out.storage.clone() // the referent's origins cover its field
		} else {
			b.pending(node.Span, "projected borrowed payload needs precise origin facts")
			out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
		return out, nil
	case ast.ExprCast:
		data, _ := u.Builder.Exprs.Cast(id)
		out, err := b.expr(data.Value, env, targets)
		if err != nil || !out.flow.normal.reachable {
			return out, err
		}
		// A present entry is a __to call even when invalid: HIR lowers it as one.
		if b.shape(id) != returnOriginRefFree || !b.castProven(id, data.Value) {
			b.pending(node.Span, "conversion retains its actual expression for origin finalization")
			out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		} else if _, selected := u.Sema.ToSymbols[id]; !selected && b.shape(data.Value) == returnOriginRefFree {
			out.value = b.discardLoans(out.value, node.Span) // native cast, no certification: G6-iv
		} else {
			out.value = returnOriginValueOf() // certified selection: no loan-discard refusal
		}
		out.storage = returnOriginValue{}
		return out, nil
	default:
		return b.unknownExpr(env, node.Span, fmt.Sprintf("expression kind %d needs an origin transfer", node.Kind)), nil
	}
}

func (b *returnOriginBody) shape(id ast.ExprID) returnOriginShape {
	return returnOriginTypeShape(b.function.unit.Sema.TypeInterner, b.function.unit.Sema.ExprTypes[id], nil)
}

func (b *returnOriginBody) unknownExpr(env returnOriginEnv, span source.Span, reason string) returnOriginExprResult {
	b.pending(span, reason)
	return originExprValue(env, returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
}

func (b *returnOriginBody) unary(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	data, _ := u.Builder.Exprs.Unary(id)
	out, err := b.expr(data.Operand, env, targets)
	if err != nil || !out.flow.normal.reachable {
		return out, err
	}
	span := u.Builder.Exprs.Get(id).Span
	switch data.Op {
	case ast.ExprUnaryRef, ast.ExprUnaryRefMut:
		out.value = out.storage.clone()
		if !out.value.normal {
			b.pending(span, "borrowed temporary has no proven storage owner")
			out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
		out.storage = returnOriginValue{}
	case ast.ExprUnaryDeref:
		out.storage = out.value.clone()
		if b.shape(id) == returnOriginRefFree && b.analyzer.loanCarrier(u.Sema.ExprTypes[id]) {
			out.value = b.containerLoans(out.storage, out.flow.normal, span)
		} else if b.shape(id) == returnOriginRefFree {
			out.value = returnOriginValueOf()
		} else if loaded, handled := b.loadExternalCells(u.Sema.ExprTypes[data.Operand], out.storage, out.flow.normal, span); handled {
			out.value = loaded
		} else {
			b.pending(span, "reference loaded through another reference needs content provenance")
			out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
	case ast.ExprUnaryOwn:
		out.storage = returnOriginValue{}
	default:
		if b.shape(id) == returnOriginRefFree && b.shape(data.Operand) == returnOriginRefFree {
			out.value = b.discardLoans(out.value, span)
		} else {
			b.pending(span, "unary callable needs an exact origin contract")
			out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
		}
		out.storage = returnOriginValue{}
	}
	return out, nil
}

func (b *returnOriginBody) binary(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	data, _ := u.Builder.Exprs.Binary(id)
	left, err := b.expr(data.Left, env, targets)
	if err != nil || !left.flow.normal.reachable {
		return left, err
	}
	if (data.Op == ast.ExprBinaryLogicalAnd && b.literalBool(data.Left, false)) ||
		(data.Op == ast.ExprBinaryLogicalOr && b.literalBool(data.Left, true)) {
		left.value, left.storage = returnOriginValueOf(), returnOriginValue{}
		return left, nil
	}
	right, err := b.expr(data.Right, left.flow.normal, targets)
	if err != nil {
		return returnOriginExprResult{}, err
	}
	prior := left.flow.clone()
	prior.normal = returnOriginEnv{}
	right.flow = prior.join(right.flow)
	if data.Op == ast.ExprBinaryLogicalAnd || data.Op == ast.ExprBinaryLogicalOr {
		// The RHS need not run. Its side effects join with the LHS-only edge.
		right.flow.normal = right.flow.normal.join(left.flow.normal)
	}
	if !right.flow.normal.reachable {
		return right, nil
	}
	if data.Op == ast.ExprBinaryAssign {
		symID := u.Symbols.ExprSymbols[data.Left]
		node := u.Builder.Exprs.Get(data.Left)
		if node.Kind != ast.ExprIdent || !symID.IsValid() {
			if next, stored := b.storeExternalCells(data.Left, left.storage, right.value, right.flow.normal); stored {
				right.flow.normal = next
			} else if next, stored := b.indexStore(data.Left, left.storage, right.value, data.Right, right.flow.normal, u.Builder.Exprs.Get(id).Span); stored {
				right.flow.normal = next
			} else {
				right.flow.normal, right.value = b.placeStore(data.Left, data.Right, right.value, right.flow.normal, u.Builder.Exprs.Get(id).Span)
			}
		} else if sym := u.Symbols.Table.Symbols.Get(symID); sym != nil {
			annotation := ast.NoTypeID
			if decl := u.Builder.Stmts.Get(sym.Decl.Stmt); decl != nil && decl.Kind == ast.StmtLet {
				annotation = u.Builder.Stmts.Let(sym.Decl.Stmt).Type
			}
			right.value = b.bindCallable(right.value, symID, annotation, data.Right, right.flow.normal.value(symID), true)
			right.flow.normal = right.flow.normal.assign(symID, sym.Scope, right.value)
		}
		right.storage = returnOriginValue{}
		return right, nil
	}
	borrowFree := b.shape(id) == returnOriginRefFree && b.shape(data.Left) == returnOriginRefFree && b.shape(data.Right) == returnOriginRefFree
	// A certified operation drops operand origins; the borrow-free path needs no proof.
	if !borrowFree && (b.shape(id) != returnOriginRefFree ||
		!b.selectedOperation(u.Sema.MagicBinarySymbols, id, magicNameForBinaryOp(data.Op), 2, data.Left, data.Right)) {
		b.pending(u.Builder.Exprs.Get(id).Span, "binary callable needs an exact origin contract")
		right.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	} else if borrowFree {
		// No certification was computed: every operand is ref-free typed (G6-iv).
		b.discardLoans(left.value, u.Builder.Exprs.Get(id).Span)
		right.value = b.discardLoans(right.value, u.Builder.Exprs.Get(id).Span)
	} else {
		right.value = returnOriginValueOf() // certified operation: no loan-discard refusal
	}
	right.storage = returnOriginValue{}
	return right, nil
}

func (b *returnOriginBody) blockExpr(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	scope := u.exprScopes[id]
	if !scope.IsValid() {
		return returnOriginExprResult{}, fmt.Errorf("return origins: block expression %d has no owning scope", id)
	}
	targets.scope, targets.block = scope, scope
	data, _ := u.Builder.Exprs.Block(id)
	stmts, tail := data.Stmts, b.legacyExprTail(id, data.Stmts)
	if tail.IsValid() {
		stmts = stmts[:len(stmts)-1]
	}
	flow, err := b.sequence(stmts, env, targets)
	if err == nil && tail.IsValid() {
		// The legacy tail leaves the block like `ret`, so its value is checked where it exits.
		flow, err = flow.then(func(next returnOriginEnv) (returnOriginFlow, error) {
			out, tailErr := b.expr(u.Builder.Stmts.Expr(tail).Expr, next, targets)
			key := returnOriginExit{kind: returnOriginBlockResult, target: scope, site: u.Builder.Stmts.Get(tail).Span}
			return out.flow.end(key, out.value), tailErr
		})
	}
	if err != nil {
		return returnOriginExprResult{}, err
	}
	flow = b.closeFlow(flow, scope, u.Builder.Exprs.Get(id).Span)
	value := returnOriginValue{}
	if flow.normal.reachable {
		value = returnOriginValueOf()
	}
	for key, outcome := range flow.exits {
		if key.kind == returnOriginBlockResult && key.target == scope {
			flow.normal = flow.normal.join(outcome.env)
			value = value.join(outcome.value)
			delete(flow.exits, key)
		}
	}
	return returnOriginExprResult{flow: flow, value: value}, nil
}

func (b *returnOriginBody) ternary(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	data, _ := b.function.unit.Builder.Exprs.Ternary(id)
	cond, err := b.expr(data.Cond, env, targets)
	if err != nil || !cond.flow.normal.reachable {
		return cond, err
	}
	if b.literalBool(data.Cond, true) || b.literalBool(data.Cond, false) {
		selected := data.FalseExpr
		if b.literalBool(data.Cond, true) {
			selected = data.TrueExpr
		}
		out, branchErr := b.expr(selected, cond.flow.normal, targets)
		cond.flow.normal = returnOriginEnv{}
		out.flow = cond.flow.join(out.flow)
		return out, branchErr
	}
	left, err := b.expr(data.TrueExpr, cond.flow.normal.clone(), targets)
	if err != nil {
		return returnOriginExprResult{}, err
	}
	right, err := b.expr(data.FalseExpr, cond.flow.normal.clone(), targets)
	if err != nil {
		return returnOriginExprResult{}, err
	}
	cond.flow.normal = returnOriginEnv{}
	return returnOriginExprResult{flow: cond.flow.join(left.flow).join(right.flow), value: left.value.join(right.value)}, nil
}
