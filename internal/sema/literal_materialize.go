package sema

import (
	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

type numericLiteralInfo struct {
	exprIDs []ast.ExprID
	kind    ast.ExprLitKind
	text    string
	neg     bool
}

func (tc *typeChecker) numericLiteralInfo(expr ast.ExprID) (numericLiteralInfo, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return numericLiteralInfo{}, false
	}
	var (
		exprIDs []ast.ExprID
		neg     bool
		current = expr
	)
	for current.IsValid() {
		node := tc.builder.Exprs.Get(current)
		if node == nil {
			return numericLiteralInfo{}, false
		}
		exprIDs = append(exprIDs, current)
		switch node.Kind {
		case ast.ExprGroup:
			group, ok := tc.builder.Exprs.Group(current)
			if !ok || group == nil {
				return numericLiteralInfo{}, false
			}
			current = group.Inner
		case ast.ExprUnary:
			unary, ok := tc.builder.Exprs.Unary(current)
			if !ok || unary == nil {
				return numericLiteralInfo{}, false
			}
			switch unary.Op {
			case ast.ExprUnaryPlus:
				current = unary.Operand
			case ast.ExprUnaryMinus:
				neg = !neg
				current = unary.Operand
			default:
				return numericLiteralInfo{}, false
			}
		case ast.ExprLit:
			lit, ok := tc.builder.Exprs.Literal(current)
			if !ok || lit == nil {
				return numericLiteralInfo{}, false
			}
			return numericLiteralInfo{
				exprIDs: exprIDs,
				kind:    lit.Kind,
				text:    tc.lookupName(lit.Value),
				neg:     neg,
			}, true
		default:
			return numericLiteralInfo{}, false
		}
	}
	return numericLiteralInfo{}, false
}

type arrayLiteralInfo struct {
	exprIDs []ast.ExprID
	data    *ast.ExprArrayData
}

func (tc *typeChecker) arrayLiteralInfo(expr ast.ExprID) (arrayLiteralInfo, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return arrayLiteralInfo{}, false
	}
	var (
		exprIDs []ast.ExprID
		current = expr
	)
	for current.IsValid() {
		node := tc.builder.Exprs.Get(current)
		if node == nil {
			return arrayLiteralInfo{}, false
		}
		exprIDs = append(exprIDs, current)
		switch node.Kind {
		case ast.ExprGroup:
			group, ok := tc.builder.Exprs.Group(current)
			if !ok || group == nil {
				return arrayLiteralInfo{}, false
			}
			current = group.Inner
		case ast.ExprArray:
			arr, ok := tc.builder.Exprs.Array(current)
			if !ok || arr == nil {
				return arrayLiteralInfo{}, false
			}
			return arrayLiteralInfo{exprIDs: exprIDs, data: arr}, true
		default:
			return arrayLiteralInfo{}, false
		}
	}
	return arrayLiteralInfo{}, false
}

func (tc *typeChecker) setExprTypes(exprIDs []ast.ExprID, expected types.TypeID) {
	if expected == types.NoTypeID || tc == nil {
		return
	}
	for _, id := range exprIDs {
		if id.IsValid() {
			tc.result.ExprTypes[id] = expected
		}
	}
}

func parseBigIntLiteral(text string) (bignum.BigInt, bool) {
	if text == "" {
		return bignum.BigInt{}, false
	}
	u, err := bignum.ParseUintLiteral(text)
	if err != nil {
		return bignum.BigInt{}, false
	}
	if u.IsZero() {
		return bignum.BigInt{}, true
	}
	return bignum.BigInt{Limbs: u.Limbs}, true
}

func (tc *typeChecker) intMinMax(width types.Width, signed bool) (minVal, maxVal bignum.BigInt, ok bool) {
	if width == types.WidthAny {
		return bignum.BigInt{}, bignum.BigInt{}, false
	}
	bits := uint(width)
	switch width {
	case types.Width8, types.Width16, types.Width32, types.Width64:
	default:
		return bignum.BigInt{}, bignum.BigInt{}, false
	}
	if signed {
		if bits == 64 {
			maxU := ^uint64(0) >> 1
			maxVal = bignum.IntFromInt64(int64(maxU))
			minVal = bignum.IntFromInt64(-int64(maxU) - 1)
			return minVal, maxVal, true
		}
		maxInt := int64(1<<(bits-1)) - 1
		minInt := -int64(1 << (bits - 1))
		return bignum.IntFromInt64(minInt), bignum.IntFromInt64(maxInt), true
	}
	var maxU uint64
	if bits == 64 {
		maxU = ^uint64(0)
	} else {
		maxU = (uint64(1) << bits) - 1
	}
	return bignum.IntFromInt64(0), bignum.IntFromUint64(maxU), true
}

func (tc *typeChecker) intLiteralInRange(value bignum.BigInt, expected types.TypeID, span source.Span, valueLabel string) bool {
	if expected == types.NoTypeID || tc.types == nil {
		return true
	}
	info, ok := tc.numericInfo(expected)
	if !ok {
		return true
	}
	switch info.kind {
	case numericUnsigned:
		if value.Neg {
			minLabel := "0"
			maxLabel := "inf"
			if _, maxVal, okBounds := tc.intMinMax(info.width, false); okBounds {
				maxLabel = bignum.FormatInt(maxVal)
			}
			tc.report(diag.SemaIntLiteralOutOfRange, span, "integer literal out of range for %s: %s (allowed %s..%s)", tc.typeLabel(expected), valueLabel, minLabel, maxLabel)
			return false
		}
		if info.width == types.WidthAny {
			return true
		}
	case numericSigned:
		if info.width == types.WidthAny {
			return true
		}
	default:
		return true
	}
	minVal, maxVal, okBounds := tc.intMinMax(info.width, info.kind == numericSigned)
	if !okBounds {
		return true
	}
	if value.Cmp(minVal) < 0 || value.Cmp(maxVal) > 0 {
		tc.report(diag.SemaIntLiteralOutOfRange, span, "integer literal out of range for %s: %s (allowed %s..%s)", tc.typeLabel(expected), valueLabel, bignum.FormatInt(minVal), bignum.FormatInt(maxVal))
		return false
	}
	return true
}

func (tc *typeChecker) materializeNumericLiteral(expr ast.ExprID, expected types.TypeID) (applied, ok bool) {
	if expected == types.NoTypeID || !expr.IsValid() || tc.types == nil {
		return false, true
	}
	info, ok := tc.numericLiteralInfo(expr)
	if !ok {
		return false, true
	}
	sourceType := tc.literalType(info.kind)
	if sourceType == types.NoTypeID || !tc.literalCoercible(expected, sourceType) {
		return false, true
	}
	expInfo, ok := tc.numericInfo(expected)
	if !ok {
		return false, true
	}
	switch info.kind {
	case ast.ExprLitFloat:
		if expInfo.kind != numericFloat {
			return false, true
		}
		tc.setExprTypes(info.exprIDs, expected)
		return true, true
	case ast.ExprLitInt, ast.ExprLitUint:
		if expInfo.kind != numericSigned && expInfo.kind != numericUnsigned {
			return false, true
		}
		value, ok := parseBigIntLiteral(info.text)
		if !ok {
			return false, true
		}
		if info.neg {
			value = value.Negated()
		}
		valueLabel := info.text
		if info.neg && valueLabel != "" && valueLabel[0] != '-' {
			valueLabel = "-" + valueLabel
		}
		rangeOK := tc.intLiteralInRange(value, expected, tc.exprSpan(expr), valueLabel)
		tc.setExprTypes(info.exprIDs, expected)
		return true, rangeOK
	default:
		return false, true
	}
}

func (tc *typeChecker) materializeArrayLiteral(expr ast.ExprID, expected types.TypeID) (applied, ok bool) {
	if expected == types.NoTypeID || !expr.IsValid() || tc.types == nil {
		return false, false
	}
	info, ok := tc.arrayLiteralInfo(expr)
	if !ok || info.data == nil {
		return false, false
	}
	expElem, expLen, expFixed, ok := tc.arrayInfo(expected)
	if !ok {
		return false, false
	}
	okAll := true
	reported := false
	if expFixed {
		length, err := safecast.Conv[uint32](len(info.data.Elements))
		if err != nil || length != expLen {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(expr),
				"array literal length %d does not match expected length %d", len(info.data.Elements), expLen)
			okAll = false
			reported = true
		}
	}
	needsConversion := false
	for _, elem := range info.data.Elements {
		if !elem.IsValid() {
			continue
		}
		if applied, ok := tc.materializeNumericLiteral(elem, expElem); applied && !ok {
			okAll = false
			reported = true
		}
		elemType := tc.result.ExprTypes[elem]
		if elemType == types.NoTypeID {
			elemType = tc.typeExpr(elem)
		}
		if elemType == types.NoTypeID {
			okAll = false
			continue
		}
		if tc.typesAssignable(expElem, elemType, true) {
			if tc.needsNumericWidening(elemType, expElem) {
				needsConversion = true
			}
			continue
		}
		if _, found, ambiguous := tc.tryImplicitConversion(elemType, expElem); found {
			needsConversion = true
			continue
		} else if ambiguous {
			tc.report(diag.SemaAmbiguousConversion, tc.exprSpan(elem),
				"ambiguous conversion from %s to %s: multiple __to methods found",
				tc.typeLabel(elemType), tc.typeLabel(expElem))
			okAll = false
			reported = true
			continue
		}
		okAll = false
	}
	if okAll {
		tc.setExprTypes(info.exprIDs, expected)
		if needsConversion {
			tc.recordArrayElementConversions(info.data, expElem)
		}
		return true, true
	}
	if reported {
		tc.setExprTypes(info.exprIDs, expected)
		return true, false
	}
	return false, false
}
