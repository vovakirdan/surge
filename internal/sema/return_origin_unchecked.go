package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An anonymous record literal (`{ x: 1 }` with no type name) takes its type only
// from the context's expected struct type (type_expr_values.go typeExprStruct).
// Given none, it has no checked type and no diagnostic, and expressions that
// read it inherit NoTypeID silently, because the checker treats NoTypeID as an
// error already reported. Among them are its binding, a field of it, and a
// numeric operator over it (enforceSameNumericOperands). MIR validation refuses
// a function holding such a value ("local ...: unknown type"), so none runs.
//
// These are answered from the values themselves, never from a guessed type. A
// value with no roots is proven to carry no borrow (returnOriginValue), so it is
// no reference either, and nothing read out of it or computed from it by a
// builtin operator carries one. Every other unchecked expression is refused by
// name; none is skipped.
const (
	returnOriginUncheckedReason       = "untyped expression needs a checked type for its origins"
	returnOriginUncheckedFieldReason  = "field of an untyped value that may carry a borrow needs a checked type"
	returnOriginUncheckedBinaryReason = "operator on an untyped value that may carry a borrow needs a checked type"
)

func (b *returnOriginBody) uncheckedExpr(id ast.ExprID, node *ast.Expr, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	switch node.Kind {
	case ast.ExprStruct:
		if b.anonymousRecordLiteral(id) {
			return b.constructor(id, env, targets)
		}
	case ast.ExprGroup:
		data, _ := u.Builder.Exprs.Group(id)
		return b.expr(data.Inner, env, targets)
	case ast.ExprIdent:
		// A local binding's value is what the environment tracked for it, whatever its type.
		symID := u.Symbols.ExprSymbols[id]
		if sym := u.Symbols.Table.Symbols.Get(symID); sym != nil && sym.Kind == symbols.SymbolLet && b.within(sym.Scope, b.function.scope) {
			value := env.value(symID)
			b.checkExpired(value, node.Span)
			out := originExprValue(env, value)
			out.storage = returnOriginValueOf(returnOrigin{kind: returnOriginLocal, binding: symID, scope: sym.Scope})
			return out, nil
		}
	case ast.ExprMember:
		return b.uncheckedField(id, node, env, targets)
	case ast.ExprBinary:
		data, _ := u.Builder.Exprs.Binary(id)
		_, selected := u.Sema.MagicBinarySymbols[id]
		_, compound := binaryAssignmentBaseOp(data.Op)
		// HIR lowers an unselected operator as the builtin operation (hir/lower_expr.go
		// lowerBinaryExpr), which reads its operands as values and borrows neither.
		if !selected && !compound && isNumericBinaryOp(data.Op) {
			return b.uncheckedBuiltinBinary(id, data, env, targets)
		}
	}
	return b.unknownExpr(env, node.Span, returnOriginUncheckedReason), nil
}

// anonymousRecordLiteral reports an untyped struct literal written without a type name.
func (b *returnOriginBody) anonymousRecordLiteral(id ast.ExprID) bool {
	u := b.function.unit
	data, ok := u.Builder.Exprs.Struct(id)
	return ok && data != nil && !data.Type.IsValid() && u.Sema.ExprTypes[id] == types.NoTypeID
}

func (b *returnOriginBody) uncheckedField(id ast.ExprID, node *ast.Expr, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	data, _ := u.Builder.Exprs.Member(id)
	if target := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[data.Target]); target != nil && target.Kind == symbols.SymbolModule {
		return b.unknownExpr(env, node.Span, returnOriginUncheckedReason), nil
	}
	if _, variant := u.Sema.EnumVariantUses[id]; variant {
		return b.unknownExpr(env, node.Span, returnOriginUncheckedReason), nil
	}
	out, err := b.expr(data.Target, env, targets)
	if err != nil || !out.flow.normal.reachable {
		return out, err
	}
	if returnOriginProvenBorrowFree(out.value) {
		// Not a reference, so the field lies in the target's own storage.
		out.value = returnOriginValueOf()
		return out, nil
	}
	b.pending(node.Span, returnOriginUncheckedFieldReason)
	out.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	out.storage = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	return out, nil
}

func (b *returnOriginBody) uncheckedBuiltinBinary(id ast.ExprID, data *ast.ExprBinaryData, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	left, err := b.expr(data.Left, env, targets)
	if err != nil || !left.flow.normal.reachable {
		return left, err
	}
	right, err := b.expr(data.Right, left.flow.normal, targets)
	if err != nil {
		return returnOriginExprResult{}, err
	}
	prior := left.flow.clone()
	prior.normal = returnOriginEnv{}
	right.flow = prior.join(right.flow)
	if !right.flow.normal.reachable {
		return right, nil
	}
	if returnOriginProvenBorrowFree(left.value) && returnOriginProvenBorrowFree(right.value) {
		right.value = returnOriginValueOf()
	} else {
		b.pending(b.function.unit.Builder.Exprs.Get(id).Span, returnOriginUncheckedBinaryReason)
		right.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	}
	right.storage = returnOriginValue{}
	return right, nil
}

// returnOriginProvenBorrowFree is a normal value with no root and no callable.
func returnOriginProvenBorrowFree(value returnOriginValue) bool {
	return value.normal && len(value.roots) == 0 && len(value.callables) == 0
}
