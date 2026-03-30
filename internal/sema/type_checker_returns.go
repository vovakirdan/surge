package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

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

func (tc *typeChecker) pushReturnContext(kind returnContextKind, expected types.TypeID, span source.Span, collect *[]collectedResult, bareRet *[]source.Span) {
	ctx := returnContext{kind: kind, expected: expected, span: span, collect: collect, bareRet: bareRet}
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

func (tc *typeChecker) currentBlockReturnContext() *returnContext {
	ctx := tc.currentReturnContext()
	if ctx == nil || ctx.kind != returnCtxBlockExpr || ctx.collect == nil {
		return nil
	}
	return ctx
}

func (tc *typeChecker) appendCollectedResult(ctx *returnContext, span source.Span, expr ast.ExprID, typ types.TypeID) {
	if tc == nil || ctx == nil || ctx.collect == nil || typ == types.NoTypeID {
		return
	}
	*ctx.collect = append(*ctx.collect, collectedResult{
		typ:    typ,
		span:   span,
		expr:   expr,
		abrupt: ctx.kind == returnCtxBlockExpr,
	})
}

func (tc *typeChecker) currentBlockReturnExpectedType() types.TypeID {
	ctx := tc.currentBlockReturnContext()
	if ctx == nil {
		return types.NoTypeID
	}
	if ctx.expected != types.NoTypeID {
		return ctx.expected
	}
	if ctx.collect == nil {
		return types.NoTypeID
	}
	results := *ctx.collect
	for i := len(results) - 1; i >= 0; i-- {
		result := results[i]
		if result.abrupt || result.typ == types.NoTypeID {
			continue
		}
		return result.typ
	}
	return types.NoTypeID
}

func (tc *typeChecker) validateReturn(span source.Span, expr ast.ExprID, actual types.TypeID) {
	ctx := tc.currentReturnContext()
	if ctx == nil || tc.types == nil {
		return
	}
	if ctx.collect != nil && ctx.kind != returnCtxFunction {
		// Returns inside block expressions still return from the enclosing function.
		// Apply implicit tag injection against the outer function return type when the
		// top collecting context is a block expression.
		if ctx.kind == returnCtxBlockExpr && expr.IsValid() && actual != types.NoTypeID && tc.types != nil {
			var outerExpected types.TypeID
			if len(tc.returnStack) > 1 {
				outerExpected = tc.returnStack[len(tc.returnStack)-2].expected
			}
			if outerExpected != types.NoTypeID && outerExpected != tc.types.Builtins().Nothing {
				if convType, kind, found := tc.tryTagInjection(actual, outerExpected); found {
					tc.recordImplicitConversionWithKind(expr, actual, convType, kind)
				}
			}
		}
		record := actual
		if !expr.IsValid() {
			record = tc.types.Builtins().Nothing
		}
		tc.appendCollectedResult(ctx, span, expr, record)
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
	actualOrig := actual
	actual = tc.coerceReturnType(expected, actual)
	if actual == expected && actualOrig != expected {
		if convType, kind, found := tc.tryTagInjection(actualOrig, expected); found {
			tc.recordImplicitConversionWithKind(expr, actualOrig, convType, kind)
		}
	}
	if tc.typesAssignable(expected, actual, false) {
		tc.dropImplicitBorrow(expr, expected, actual, span)
		if tc.recordTagUnionUpcast(expr, actual, expected) {
			return
		}
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
	// Try implicit tag injection for Option<T> and Erring<T, E> on returns.
	if convType, kind, found := tc.tryTagInjection(actual, expected); found {
		tc.recordImplicitConversionWithKind(expr, actual, convType, kind)
		return
	}
	tc.report(diag.SemaTypeMismatch, span, "return type mismatch: expected %s, got %s", tc.typeLabel(expected), tc.typeLabel(actual))
}

func (tc *typeChecker) validateRet(span source.Span, expr ast.ExprID, actual types.TypeID) {
	if tc.types == nil {
		return
	}
	ctx := tc.currentBlockReturnContext()
	if ctx == nil {
		if outer := tc.currentReturnContext(); outer != nil && outer.kind == returnCtxTaskPayload {
			tc.report(diag.SemaRetOutsideBlock, span, "'ret' is not supported inside async/blocking payloads; use 'return' for now")
			return
		}
		tc.report(diag.SemaRetOutsideBlock, span, "'ret' can only be used inside value-producing blocks")
		return
	}
	if expr.IsValid() && actual == types.NoTypeID && ctx.expected != types.NoTypeID {
		if tc.applyExpectedType(expr, ctx.expected) {
			actual = tc.result.ExprTypes[expr]
		} else if data, ok := tc.builder.Exprs.Struct(expr); ok && data != nil && !data.Type.IsValid() {
			tc.validateStructLiteralFields(ctx.expected, data, tc.exprSpan(expr))
		}
	}
	record := actual
	if !expr.IsValid() {
		record = tc.types.Builtins().Nothing
		if ctx.bareRet != nil {
			*ctx.bareRet = append(*ctx.bareRet, span)
		}
	}
	if record != types.NoTypeID {
		*ctx.collect = append(*ctx.collect, collectedResult{
			typ:    record,
			span:   span,
			expr:   expr,
			abrupt: false,
		})
	}
}

func (tc *typeChecker) validateImplicitBlockReturn(span source.Span, expr ast.ExprID, actual types.TypeID) {
	if tc.types == nil {
		return
	}
	ctx := tc.currentBlockReturnContext()
	if ctx == nil || ctx.collect == nil {
		return
	}
	if expr.IsValid() && actual == types.NoTypeID && ctx.expected != types.NoTypeID {
		if tc.applyExpectedType(expr, ctx.expected) {
			actual = tc.result.ExprTypes[expr]
		} else if data, ok := tc.builder.Exprs.Struct(expr); ok && data != nil && !data.Type.IsValid() {
			tc.validateStructLiteralFields(ctx.expected, data, tc.exprSpan(expr))
		}
	}
	if actual == types.NoTypeID {
		return
	}
	*ctx.collect = append(*ctx.collect, collectedResult{
		typ:    actual,
		span:   span,
		expr:   expr,
		abrupt: false,
	})
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
