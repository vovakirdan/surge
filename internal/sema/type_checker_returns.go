package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

func (tc *typeChecker) returnStatus(stmtID ast.StmtID) returnStatus {
	if !stmtID.IsValid() || tc.builder == nil {
		return returnOpen
	}
	stmt := tc.builder.Stmts.Get(stmtID)
	if stmt == nil {
		return returnOpen
	}
	switch stmt.Kind {
	case ast.StmtReturn:
		return returnClosed
	case ast.StmtBlock:
		if block := tc.builder.Stmts.Block(stmtID); block != nil {
			for _, child := range block.Stmts {
				if tc.returnStatus(child) == returnClosed {
					return returnClosed
				}
			}
		}
		return returnOpen
	case ast.StmtIf:
		ifStmt := tc.builder.Stmts.If(stmtID)
		if ifStmt == nil {
			return returnOpen
		}
		thenStatus := tc.returnStatus(ifStmt.Then)
		elseStatus := tc.returnStatus(ifStmt.Else)
		if ifStmt.Else.IsValid() && thenStatus == returnClosed && elseStatus == returnClosed {
			return returnClosed
		}
		return returnOpen
	case ast.StmtWhile:
		whileStmt := tc.builder.Stmts.While(stmtID)
		if whileStmt == nil {
			return returnOpen
		}
		if tc.isBoolLiteralTrue(whileStmt.Cond) && tc.returnStatus(whileStmt.Body) == returnClosed {
			return returnClosed
		}
		return returnOpen
	case ast.StmtForClassic:
		forStmt := tc.builder.Stmts.ForClassic(stmtID)
		if forStmt == nil {
			return returnOpen
		}
		// Classic for can skip the body unless condition is explicitly true/absent.
		infinite := !forStmt.Cond.IsValid() || tc.isBoolLiteralTrue(forStmt.Cond)
		if infinite && tc.returnStatus(forStmt.Body) == returnClosed {
			return returnClosed
		}
		return returnOpen
	case ast.StmtForIn:
		return returnOpen
	default:
		return returnOpen
	}
}

func (tc *typeChecker) isBoolLiteralTrue(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprLit:
		if lit, ok := tc.builder.Exprs.Literal(expr); ok && lit != nil {
			return lit.Kind == ast.ExprLitTrue
		}
	case ast.ExprGroup:
		if grp, ok := tc.builder.Exprs.Group(expr); ok && grp != nil {
			return tc.isBoolLiteralTrue(grp.Inner)
		}
	}
	return false
}

func (tc *typeChecker) pushReturnContext(expected types.TypeID, span source.Span, collect *[]types.TypeID) {
	ctx := returnContext{expected: expected, span: span, collect: collect}
	tc.returnStack = append(tc.returnStack, ctx)
}

func (tc *typeChecker) popReturnContext() {
	if len(tc.returnStack) == 0 {
		return
	}
	tc.returnStack = tc.returnStack[:len(tc.returnStack)-1]
}

func (tc *typeChecker) currentReturnContext() *returnContext {
	if len(tc.returnStack) == 0 {
		return nil
	}
	return &tc.returnStack[len(tc.returnStack)-1]
}

func (tc *typeChecker) validateReturn(span source.Span, expr ast.ExprID, actual types.TypeID) {
	ctx := tc.currentReturnContext()
	if ctx == nil || tc.types == nil {
		return
	}
	if ctx.collect != nil && ctx.expected == types.NoTypeID {
		record := actual
		if !expr.IsValid() {
			record = tc.types.Builtins().Nothing
		}
		if record != types.NoTypeID {
			*ctx.collect = append(*ctx.collect, record)
		}
		return
	}
	expected := ctx.expected
	if expected == types.NoTypeID {
		expected = tc.types.Builtins().Nothing
	}
	nothing := tc.types.Builtins().Nothing
	if !expr.IsValid() {
		if expected != nothing {
			tc.report(diag.SemaTypeMismatch, span, "return value must have type %s", tc.typeLabel(expected))
		}
		return
	}
	if expected == nothing {
		if actual == nothing {
			return
		}
		tc.report(diag.SemaTypeMismatch, span, "function returning nothing cannot return a value")
		return
	}
	if actual == types.NoTypeID {
		if tc.applyExpectedType(expr, expected) {
			return
		}
		// Handle bare struct literal - validate fields against expected return type
		if data, ok := tc.builder.Exprs.Struct(expr); ok && data != nil && !data.Type.IsValid() {
			tc.validateStructLiteralFields(expected, data, tc.exprSpan(expr))
		}
		return
	}
	if applied, ok := tc.materializeNumericLiteral(expr, expected); applied {
		actual = tc.result.ExprTypes[expr]
		if !ok {
			return
		}
	}
	if applied, ok := tc.materializeArrayLiteral(expr, expected); applied {
		if !ok {
			return
		}
		actual = tc.result.ExprTypes[expr]
	}
	actual = tc.coerceReturnType(expected, actual)
	if tc.typesAssignable(expected, actual, false) {
		tc.dropImplicitBorrow(expr, expected, actual, span)
		if tc.recordNumericWidening(expr, actual, expected) {
			return
		}
		return
	}
	// Try implicit conversion before reporting error
	if convType, found, ambiguous := tc.tryImplicitConversion(actual, expected); found {
		tc.recordImplicitConversion(expr, actual, convType)
		return
	} else if ambiguous {
		tc.report(diag.SemaAmbiguousConversion, span,
			"ambiguous conversion from %s to %s: multiple __to methods found",
			tc.typeLabel(actual), tc.typeLabel(expected))
		return
	}
	tc.report(diag.SemaTypeMismatch, span, "return type mismatch: expected %s, got %s", tc.typeLabel(expected), tc.typeLabel(actual))
}

func (tc *typeChecker) coerceLiteralForBinding(declared, actual types.TypeID, expr ast.ExprID) types.TypeID {
	if !tc.isLiteralExpr(expr) {
		return actual
	}
	if tc.literalCoercible(declared, actual) {
		return declared
	}
	return actual
}

func (tc *typeChecker) isLiteralExpr(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprLit:
		return true
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(expr); ok && group != nil {
			return tc.isLiteralExpr(group.Inner)
		}
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(expr); ok && data != nil {
			switch data.Op {
			case ast.ExprUnaryPlus, ast.ExprUnaryMinus:
				return tc.isLiteralExpr(data.Operand)
			}
		}
	}
	return false
}

func (tc *typeChecker) literalCoercible(target, from types.TypeID) bool {
	if target == types.NoTypeID || from == types.NoTypeID || tc.types == nil {
		return false
	}
	targetKind, ok := tc.typeKind(target)
	if !ok {
		return false
	}
	sourceKind, ok := tc.typeKind(from)
	if !ok {
		return false
	}
	switch sourceKind {
	case types.KindInt:
		return targetKind == types.KindInt || targetKind == types.KindUint
	case types.KindUint:
		return targetKind == types.KindUint
	case types.KindFloat:
		return targetKind == types.KindFloat
	case types.KindBool:
		return targetKind == types.KindBool
	case types.KindString:
		return targetKind == types.KindString
	default:
		return false
	}
}

func (tc *typeChecker) typeKind(id types.TypeID) (types.Kind, bool) {
	if id == types.NoTypeID || tc.types == nil {
		return types.KindInvalid, false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return types.KindInvalid, false
	}
	return tt.Kind, true
}

func (tc *typeChecker) coerceReturnType(expected, actual types.TypeID) types.TypeID {
	if expected == types.NoTypeID || actual == types.NoTypeID || tc.types == nil {
		return actual
	}
	actualResolved := tc.resolveAlias(actual)
	if elem, ok := tc.optionPayload(expected); ok {
		if actualResolved == tc.types.Builtins().Nothing {
			return expected
		}
		if tc.typesAssignable(elem, actualResolved, true) {
			return expected
		}
		if payload := tc.unwrapTaggedPayload(actualResolved, "Some"); payload != types.NoTypeID && tc.typesAssignable(elem, payload, true) {
			return expected
		}
	}
	if okType, errType, ok := tc.resultPayload(expected); ok {
		if tc.typesAssignable(okType, actualResolved, true) {
			return expected
		}
		if payload := tc.unwrapTaggedPayload(actualResolved, "Success"); payload != types.NoTypeID && tc.typesAssignable(okType, payload, true) {
			return expected
		}
		// Auto-wrap: Error types pass through directly (no tag wrapper needed)
		if tc.typesAssignable(errType, actualResolved, true) {
			return expected
		}
	}
	return actual
}

func (tc *typeChecker) unwrapTaggedPayload(id types.TypeID, tag string) types.TypeID {
	if id == types.NoTypeID || tc.types == nil || tag == "" {
		return types.NoTypeID
	}
	info, ok := tc.types.UnionInfo(id)
	if !ok || info == nil {
		return types.NoTypeID
	}
	for _, member := range info.Members {
		if member.Kind != types.UnionMemberTag {
			continue
		}
		if tc.lookupExportedName(member.TagName) != tag {
			continue
		}
		if len(member.TagArgs) == 0 {
			return types.NoTypeID
		}
		return member.TagArgs[0]
	}
	return types.NoTypeID
}
