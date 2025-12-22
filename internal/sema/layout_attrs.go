package sema

import (
	"strconv"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/types"
)

func (tc *typeChecker) typeLayoutAttrsFromInfos(infos []AttrInfo) types.LayoutAttrs {
	var out types.LayoutAttrs
	if _, ok := hasAttr(infos, "packed"); ok {
		out.Packed = true
	}
	if alignInfo, ok := hasAttr(infos, "align"); ok {
		if n, ok := tc.parseAlignValue(alignInfo); ok {
			out.AlignOverride = &n
		}
	}
	return out
}

func (tc *typeChecker) fieldLayoutAttrsFromInfos(infos []AttrInfo) types.FieldLayoutAttrs {
	var out types.FieldLayoutAttrs
	if alignInfo, ok := hasAttr(infos, "align"); ok {
		if n, ok := tc.parseAlignValue(alignInfo); ok {
			out.AlignOverride = &n
		}
	}
	return out
}

func (tc *typeChecker) parseAlignValue(info AttrInfo) (int, bool) {
	if tc == nil || tc.builder == nil || len(info.Args) == 0 {
		return 0, false
	}
	argExpr := tc.builder.Exprs.Get(info.Args[0])
	if argExpr == nil || argExpr.Kind != ast.ExprLit {
		return 0, false
	}
	lit, ok := tc.builder.Exprs.Literal(info.Args[0])
	if !ok || lit.Kind != ast.ExprLitInt {
		return 0, false
	}
	valueStr := tc.lookupName(lit.Value)
	value, err := strconv.ParseUint(valueStr, 10, 64)
	if err != nil {
		return 0, false
	}
	if value == 0 || (value&(value-1)) != 0 {
		return 0, false
	}
	n, err := safecast.Conv[int](value)
	if err != nil {
		return 0, false
	}
	return n, true
}
