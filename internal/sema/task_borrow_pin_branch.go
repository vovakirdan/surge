package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// typeExprMaybeSkipped types an operand that some path to the code after it
// does not evaluate first: the right side of `&&` and `||`, or a `for` post
// clause, which has not run when the body first does. A join inside it must not
// release a pin for the path that skipped it, so the state after it is the
// UNION with the state before -- the may-be-live rule `if` and the ternary
// already apply.
func (tc *typeChecker) typeExprMaybeSkipped(expr ast.ExprID) types.TypeID {
	before := tc.snapshotTaskBorrowPins()
	ty := tc.typeExpr(expr)
	tc.taskBorrowPins = mergeTaskBorrowPins(tc.taskBorrowPins, before)
	return ty
}

// typeBinaryRightOperand types a binary operator's right side, as an operand
// that may be skipped when the operator short-circuits.
func (tc *typeChecker) typeBinaryRightOperand(op ast.ExprBinaryOp, right ast.ExprID) types.TypeID {
	if op == ast.ExprBinaryLogicalAnd || op == ast.ExprBinaryLogicalOr {
		return tc.typeExprMaybeSkipped(right)
	}
	return tc.typeExpr(right)
}
