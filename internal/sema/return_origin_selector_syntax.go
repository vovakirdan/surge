package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// `is` and `heir` test a value against a type or tag written as syntax. The
// checker resolves that operand into IsOperands/HeirOperands and never types it,
// and lowering reads the record, so the right side is never evaluated and has no
// origin. The tested value is only read; a bool keeps none of its loans (G6).
// left is the caller's evaluated left operand, written only once the record is found.
func (b *returnOriginBody) typeTest(id ast.ExprID, data *ast.ExprBinaryData, left *returnOriginExprResult) (returnOriginExprResult, bool) {
	u := b.function.unit
	recorded := false
	switch data.Op {
	case ast.ExprBinaryIs:
		_, recorded = u.Sema.IsOperands[id]
	case ast.ExprBinaryHeir:
		_, recorded = u.Sema.HeirOperands[id]
	}
	if !recorded {
		return returnOriginExprResult{}, false
	}
	span := u.Builder.Exprs.Get(id).Span
	if b.shape(id) != returnOriginRefFree {
		b.pending(span, "type test result has an unresolved type")
		left.value = returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	} else {
		left.value = b.discardLoans(left.value, span)
	}
	left.storage = returnOriginValue{}
	return *left, true
}

// Enum::Variant is a compile-time constant: lowering emits a literal and never
// reads the target, so the member has no origin.
func (b *returnOriginBody) enumVariant(id ast.ExprID, data *ast.ExprMemberData, use EnumVariantUse, env returnOriginEnv) returnOriginExprResult {
	span := b.function.unit.Builder.Exprs.Get(id).Span
	if use.Target != data.Target || use.Enum == types.NoTypeID || b.shape(id) != returnOriginRefFree {
		return b.unknownExpr(env, span, "enum variant lacks its checked enum target")
	}
	return originExprValue(env, returnOriginValueOf())
}
