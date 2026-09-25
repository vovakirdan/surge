package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An anonymous record literal (`{ x: 1 }` with no type name) takes its type only
// from the context's expected struct type (type_expr_values.go typeExprStruct),
// and an empty array literal `[]` from its expected array type (typeExprArray).
// Given none, the literal has no checked type and no diagnostic, and expressions
// that read it inherit NoTypeID silently, because the checker treats NoTypeID as
// an error already reported. Among them are its binding, a field of it, and a
// numeric operator over it (enforceSameNumericOperands). MIR validation refuses
// a function holding such a value ("local ...: unknown type"), so none runs.
//
// These are answered from the values themselves, never from a guessed type. A
// value with no roots is proven to carry no borrow (returnOriginValue), so it is
// no reference either, and nothing read out of it or computed from it by a
// builtin operator carries one. Every other such expression is refused by name.
// An untyped expression that no such literal explains is a lost checker record,
// and exprCore still stops on it.
const (
	returnOriginUncheckedReason       = "untyped expression needs a checked type for its origins"
	returnOriginUncheckedFieldReason  = "field of an untyped value that may carry a borrow needs a checked type"
	returnOriginUncheckedBinaryReason = "operator on an untyped value that may carry a borrow needs a checked type"
)

// uncheckedByLiteral reports an untyped expression whose missing type comes from
// an untyped anonymous record or empty array literal, directly or through the
// expressions below that read one: an unannotated `let` bound to it, a group, a
// field, a unary or binary operator, an array or tuple literal, or a ternary arm.
func (b *returnOriginBody) uncheckedByLiteral(id ast.ExprID) bool {
	u := b.function.unit
	if !id.IsValid() || u.Sema.ExprTypes[id] != types.NoTypeID {
		return false
	}
	node := u.Builder.Exprs.Get(id)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprStruct:
		return b.anonymousRecordLiteral(id)
	case ast.ExprArray:
		data, ok := u.Builder.Exprs.Array(id)
		return ok && data != nil && (len(data.Elements) == 0 || b.anyUncheckedByLiteral(data.Elements...))
	case ast.ExprTuple:
		data, ok := u.Builder.Exprs.Tuple(id)
		return ok && data != nil && b.anyUncheckedByLiteral(data.Elements...)
	case ast.ExprGroup:
		data, ok := u.Builder.Exprs.Group(id)
		return ok && data != nil && b.uncheckedByLiteral(data.Inner)
	case ast.ExprMember:
		data, ok := u.Builder.Exprs.Member(id)
		return ok && data != nil && b.uncheckedByLiteral(data.Target)
	case ast.ExprUnary:
		data, ok := u.Builder.Exprs.Unary(id)
		return ok && data != nil && b.uncheckedByLiteral(data.Operand)
	case ast.ExprBinary:
		data, ok := u.Builder.Exprs.Binary(id)
		return ok && data != nil && b.anyUncheckedByLiteral(data.Left, data.Right)
	case ast.ExprTernary:
		data, ok := u.Builder.Exprs.Ternary(id)
		return ok && data != nil && b.anyUncheckedByLiteral(data.TrueExpr, data.FalseExpr)
	case ast.ExprIdent:
		// Only a binding's own declaration explains it; a later assignment cannot type it.
		sym := u.Symbols.Table.Symbols.Get(u.Symbols.ExprSymbols[id])
		if sym == nil || sym.Kind != symbols.SymbolLet || sym.Decl.ASTFile != u.FileID || !sym.Decl.Stmt.IsValid() {
			return false
		}
		if stmt := u.Builder.Stmts.Get(sym.Decl.Stmt); stmt == nil || stmt.Kind != ast.StmtLet {
			return false
		}
		decl := u.Builder.Stmts.Let(sym.Decl.Stmt)
		return decl != nil && !decl.Type.IsValid() && !decl.Pattern.IsValid() && b.uncheckedByLiteral(decl.Value)
	}
	return false
}

func (b *returnOriginBody) anyUncheckedByLiteral(ids ...ast.ExprID) bool {
	for _, id := range ids {
		if b.uncheckedByLiteral(id) {
			return true
		}
	}
	return false
}

// uncheckedExpr answers an expression uncheckedByLiteral admitted.
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
		if sym := u.Symbols.Table.Symbols.Get(symID); sym != nil && b.within(sym.Scope, b.function.scope) {
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
	data, _ := b.function.unit.Builder.Exprs.Member(id)
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
