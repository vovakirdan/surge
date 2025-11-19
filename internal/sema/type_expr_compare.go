package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

func (tc *typeChecker) inferComparePatternTypes(pattern ast.ExprID, subject types.TypeID) {
	if !pattern.IsValid() || tc.builder == nil {
		return
	}
	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil {
		return
	}
	switch expr.Kind {
	case ast.ExprIdent:
		symID := tc.symbolForExpr(pattern)
		tc.setBindingType(symID, subject)
	case ast.ExprCall:
		call, _ := tc.builder.Exprs.Call(pattern)
		if call == nil {
			return
		}
		tagName := source.NoStringID
		if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
			tagName = ident.Name
		}
		argTypes := tc.unionTagPayloadTypes(subject, tagName)
		for i, arg := range call.Args {
			argType := types.NoTypeID
			if i < len(argTypes) {
				argType = argTypes[i]
			}
			tc.inferComparePatternTypes(arg, argType)
		}
	case ast.ExprTuple:
		tuple, _ := tc.builder.Exprs.Tuple(pattern)
		if tuple == nil {
			return
		}
		for _, elem := range tuple.Elements {
			tc.inferComparePatternTypes(elem, types.NoTypeID)
		}
	}
}

func (tc *typeChecker) unionTagPayloadTypes(subject types.TypeID, tag source.StringID) []types.TypeID {
	if tag == source.NoStringID || tc.types == nil {
		return nil
	}
	normalized := tc.resolveAlias(subject)
	info, ok := tc.types.UnionInfo(normalized)
	if !ok || info == nil {
		return nil
	}
	for _, member := range info.Members {
		if member.Kind != types.UnionMemberTag {
			continue
		}
		if member.TagName == tag {
			return member.TagArgs
		}
	}
	return nil
}
