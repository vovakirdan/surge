package sema

// Recognising the DRAIN SHAPE: whether a loop condition says "while this task
// container still holds something". This is a syntactic question about one
// expression, separate from tracking what a container holds, which is
// task_container.go's subject -- and separating them is what let that file stop
// growing every time either question gained a case.

import "surge/internal/ast"

func (tc *typeChecker) taskContainerDrainLoop(cond ast.ExprID) (Place, bool) {
	if !cond.IsValid() || tc.builder == nil {
		return Place{}, false
	}
	cond = tc.unwrapGroupExpr(cond)
	bin, ok := tc.builder.Exprs.Binary(cond)
	if !ok || bin == nil {
		return Place{}, false
	}
	if place, ok := tc.taskContainerLenCall(bin.Left); ok {
		if tc.lenNonEmptyComparison(bin.Op, bin.Right, true) {
			return place, true
		}
	}
	if place, ok := tc.taskContainerLenCall(bin.Right); ok {
		if tc.lenNonEmptyComparison(bin.Op, bin.Left, false) {
			return place, true
		}
	}
	return Place{}, false
}

func (tc *typeChecker) taskContainerLenCall(expr ast.ExprID) (Place, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return Place{}, false
	}
	expr = tc.unwrapGroupExpr(expr)
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		return Place{}, false
	}
	if len(call.Args) != 0 {
		return Place{}, false
	}
	member, ok := tc.builder.Exprs.Member(call.Target)
	if !ok || member == nil {
		return Place{}, false
	}
	if tc.lookupName(member.Field) != "__len" {
		return Place{}, false
	}
	recvType := tc.typeExpr(member.Target)
	if !tc.isTaskContainerType(recvType) {
		return Place{}, false
	}
	place, ok := tc.taskContainerPlace(member.Target)
	if !ok {
		return Place{}, false
	}
	return place, true
}

func (tc *typeChecker) lenNonEmptyComparison(op ast.ExprBinaryOp, other ast.ExprID, lenOnLeft bool) bool {
	if !lenOnLeft {
		op = swapComparisonOp(op)
	}
	val, ok := tc.literalIntValue(other)
	if !ok {
		return false
	}
	switch op {
	case ast.ExprBinaryNotEq:
		return val == 0
	case ast.ExprBinaryGreater:
		return val == 0
	case ast.ExprBinaryGreaterEq:
		return val == 1
	default:
		return false
	}
}

func swapComparisonOp(op ast.ExprBinaryOp) ast.ExprBinaryOp {
	switch op {
	case ast.ExprBinaryLess:
		return ast.ExprBinaryGreater
	case ast.ExprBinaryLessEq:
		return ast.ExprBinaryGreaterEq
	case ast.ExprBinaryGreater:
		return ast.ExprBinaryLess
	case ast.ExprBinaryGreaterEq:
		return ast.ExprBinaryLessEq
	default:
		return op
	}
}

func (tc *typeChecker) literalIntValue(expr ast.ExprID) (int64, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return 0, false
	}
	expr = tc.unwrapGroupExpr(expr)
	if cast, ok := tc.builder.Exprs.Cast(expr); ok && cast != nil {
		return tc.literalIntValue(cast.Value)
	}
	if unary, ok := tc.builder.Exprs.Unary(expr); ok && unary != nil {
		switch unary.Op {
		case ast.ExprUnaryPlus:
			return tc.literalIntValue(unary.Operand)
		case ast.ExprUnaryMinus:
			if val, ok := tc.literalIntValue(unary.Operand); ok {
				return -val, true
			}
		}
		return 0, false
	}
	lit, ok := tc.builder.Exprs.Literal(expr)
	if !ok || lit == nil {
		return 0, false
	}
	if lit.Kind != ast.ExprLitInt && lit.Kind != ast.ExprLitUint {
		return 0, false
	}
	raw := tc.lookupName(lit.Value)
	val, err := parseIntLiteral(raw)
	if err != nil {
		return 0, false
	}
	return val, true
}
