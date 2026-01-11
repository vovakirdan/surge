package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// memberReceiverType determines the receiver type for a method call target.
// It first tries to treat the target as a type operand (for static/associated methods),
// and falls back to value typing.
func (tc *typeChecker) memberReceiverType(target ast.ExprID) (types.TypeID, bool) {
	if t := tc.tryResolveTypeOperand(target); t != types.NoTypeID {
		return t, true
	}
	return tc.typeExpr(target), false
}

// unifyTernaryBranches determines the result type of a ternary expression
// by unifying the types of the true and false branches.
func (tc *typeChecker) unifyTernaryBranches(trueType, falseType types.TypeID, span source.Span) types.TypeID {
	if trueType == types.NoTypeID || falseType == types.NoTypeID {
		if trueType != types.NoTypeID {
			return trueType
		}
		return falseType
	}

	nothingType := tc.types.Builtins().Nothing

	switch {
	case trueType == nothingType:
		return falseType
	case falseType == nothingType:
		return trueType
	case tc.typesAssignable(trueType, falseType, true):
		return trueType
	case tc.typesAssignable(falseType, trueType, true):
		return falseType
	default:
		tc.report(diag.SemaTypeMismatch, span,
			"ternary branches have incompatible types: %s and %s",
			tc.typeLabel(trueType), tc.typeLabel(falseType))
		return types.NoTypeID
	}
}
