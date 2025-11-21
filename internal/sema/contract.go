package sema

import (
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) checkContract(id ast.ItemID, decl *ast.ContractDecl) {
	if decl == nil {
		return
	}

	scope := tc.scopeForItem(id)
	if scope == symbols.NoScopeID {
		scope = tc.fileScope()
	}

	typeParamsPushed := tc.pushTypeParams(symbols.NoSymbolID, decl.Generics, nil)
	typeParamSet := make(map[source.StringID]struct{}, len(decl.Generics))
	typeParamUsage := make(map[source.StringID]bool, len(decl.Generics))
	var typeParamIDs []types.TypeID
	if typeParamsPushed {
		for _, name := range decl.Generics {
			typeParamSet[name] = struct{}{}
			typeParamUsage[name] = false
			typeParamIDs = append(typeParamIDs, tc.lookupTypeParam(name))
		}
	}

	markUsage := func(typeID ast.TypeID) {
		tc.markTypeParamUsage(typeID, typeParamSet, typeParamUsage)
	}

	fieldNames := make(map[source.StringID]source.Span)
	methodNames := make(map[source.StringID]struct {
		span     source.Span
		overload bool
	})

	members := tc.builder.Items.GetContractItemIDs(decl)
	for _, cid := range members {
		member := tc.builder.Items.ContractItem(cid)
		if member == nil {
			continue
		}
		switch member.Kind {
		case ast.ContractItemField:
			field := tc.builder.Items.ContractField(ast.ContractFieldID(member.Payload))
			if field == nil {
				continue
			}
			if prev, exists := fieldNames[field.Name]; exists {
				tc.report(diag.SemaContractDuplicateField, field.NameSpan, "duplicate field '%s'", tc.lookupName(field.Name))
				tc.report(diag.SemaContractDuplicateField, prev, "previous declaration of '%s' is here", tc.lookupName(field.Name))
			} else {
				fieldNames[field.Name] = field.NameSpan
			}
			tc.validateAttrs(field.AttrStart, field.AttrCount, ast.AttrTargetField)
			if field.Type.IsValid() {
				tc.resolveTypeExprWithScope(field.Type, scope)
				markUsage(field.Type)
			}
		case ast.ContractItemFn:
			fn := tc.builder.Items.ContractFn(ast.ContractFnID(member.Payload))
			if fn == nil {
				continue
			}
			currentOverload := tc.hasOverloadAttr(fn.AttrStart, fn.AttrCount)
			if prev, exists := methodNames[fn.Name]; exists {
				if !(prev.overload || currentOverload) {
					tc.report(diag.SemaContractDuplicateMethod, fn.NameSpan, "duplicate method '%s'", tc.lookupName(fn.Name))
					tc.report(diag.SemaContractDuplicateMethod, prev.span, "previous declaration of '%s' is here", tc.lookupName(fn.Name))
				}
			} else {
				methodNames[fn.Name] = struct {
					span     source.Span
					overload bool
				}{span: fn.NameSpan, overload: currentOverload}
			}
			tc.validateAttrs(fn.AttrStart, fn.AttrCount, ast.AttrTargetFn)
			tc.checkContractMethod(fn, typeParamIDs, scope, markUsage)
		}
	}

	if typeParamsPushed {
		tc.popTypeParams()
	}

	for name, used := range typeParamUsage {
		if !used {
			tc.report(diag.SemaContractUnusedTypeParam, decl.GenericsSpan, "unused generic parameter '%s'", tc.lookupName(name))
		}
	}
}

func (tc *typeChecker) checkContractMethod(fn *ast.ContractFnReq, typeParamIDs []types.TypeID, scope symbols.ScopeID, markUsage func(ast.TypeID)) {
	if fn == nil {
		return
	}
	if fn.Body.IsValid() {
		tc.report(diag.SemaContractMethodBody, fn.Span, "contract methods must not have bodies")
	}

	// resolve return type
	if fn.ReturnType.IsValid() {
		tc.resolveTypeExprWithScope(fn.ReturnType, scope)
		markUsage(fn.ReturnType)
	}

	paramIDs := tc.getContractFnParamIDs(fn)
	if len(paramIDs) > 0 {
		expectedSelf := types.NoTypeID
		if len(typeParamIDs) > 0 {
			expectedSelf = typeParamIDs[0]
		}
		first := tc.builder.Items.FnParam(paramIDs[0])
		name := ""
		if first != nil {
			name = tc.lookupName(first.Name)
		}
		allowedName := name == "self" || name == "_" || name == ""
		if first == nil || !allowedName || !first.Type.IsValid() || first.Default.IsValid() || first.Variadic {
			tc.report(diag.SemaContractSelfType, fn.ParamsSpan, "first parameter of method must be 'self: T'")
		} else {
			selfType := tc.resolveTypeExprWithScope(first.Type, scope)
			markUsage(first.Type)
			if !tc.matchesSelfType(expectedSelf, selfType) {
				tc.report(diag.SemaContractSelfType, fn.ParamsSpan, "first parameter of method must be 'self: T'")
			}
		}
	}

	for idx, pid := range paramIDs {
		if idx == 0 {
			continue
		}
		param := tc.builder.Items.FnParam(pid)
		if param == nil || !param.Type.IsValid() {
			continue
		}
		tc.resolveTypeExprWithScope(param.Type, scope)
		markUsage(param.Type)
	}
}

func (tc *typeChecker) validateAttrs(start ast.AttrID, count uint32, target ast.AttrTargetMask) {
	if count == 0 || !start.IsValid() {
		return
	}
	attrs := tc.builder.Items.CollectAttrs(start, count)
	for _, attr := range attrs {
		if spec, ok := ast.LookupAttrID(tc.builder.StringsInterner, attr.Name); ok {
			if !spec.Allows(target) {
				tc.report(diag.SemaContractUnknownAttr, attr.Span, "attribute '@%s' is not allowed here", tc.lookupName(attr.Name))
			}
			continue
		}
		tc.report(diag.SemaContractUnknownAttr, attr.Span, "unknown attribute '@%s'", tc.lookupName(attr.Name))
	}
}

func (tc *typeChecker) hasOverloadAttr(start ast.AttrID, count uint32) bool {
	if count == 0 || !start.IsValid() {
		return false
	}
	attrs := tc.builder.Items.CollectAttrs(start, count)
	for _, attr := range attrs {
		if strings.EqualFold(tc.lookupName(attr.Name), "overload") {
			return true
		}
	}
	return false
}

func (tc *typeChecker) markTypeParamUsage(typeID ast.TypeID, paramNames map[source.StringID]struct{}, usage map[source.StringID]bool) {
	if !typeID.IsValid() || tc.builder == nil {
		return
	}
	expr := tc.builder.Types.Get(typeID)
	if expr == nil {
		return
	}
	switch expr.Kind {
	case ast.TypeExprPath:
		if path, ok := tc.builder.Types.Path(typeID); ok && path != nil {
			for _, seg := range path.Segments {
				if _, ok := paramNames[seg.Name]; ok {
					usage[seg.Name] = true
				}
				for _, arg := range seg.Generics {
					tc.markTypeParamUsage(arg, paramNames, usage)
				}
			}
		}
	case ast.TypeExprUnary:
		if unary, ok := tc.builder.Types.UnaryType(typeID); ok && unary != nil {
			tc.markTypeParamUsage(unary.Inner, paramNames, usage)
		}
	case ast.TypeExprArray:
		if arr, ok := tc.builder.Types.Array(typeID); ok && arr != nil {
			tc.markTypeParamUsage(arr.Elem, paramNames, usage)
		}
	case ast.TypeExprOptional:
		if opt, ok := tc.builder.Types.Optional(typeID); ok && opt != nil {
			tc.markTypeParamUsage(opt.Inner, paramNames, usage)
		}
	case ast.TypeExprErrorable:
		if errable, ok := tc.builder.Types.Errorable(typeID); ok && errable != nil {
			tc.markTypeParamUsage(errable.Inner, paramNames, usage)
			if errable.Error.IsValid() {
				tc.markTypeParamUsage(errable.Error, paramNames, usage)
			}
		}
	case ast.TypeExprTuple:
		if tuple, ok := tc.builder.Types.Tuple(typeID); ok && tuple != nil {
			for _, elem := range tuple.Elems {
				tc.markTypeParamUsage(elem, paramNames, usage)
			}
		}
	case ast.TypeExprFn:
		if fn, ok := tc.builder.Types.Fn(typeID); ok && fn != nil {
			for _, param := range fn.Params {
				tc.markTypeParamUsage(param.Type, paramNames, usage)
			}
			tc.markTypeParamUsage(fn.Return, paramNames, usage)
		}
	}
}

func (tc *typeChecker) getContractFnParamIDs(fn *ast.ContractFnReq) []ast.FnParamID {
	if fn == nil || fn.ParamsCount == 0 || !fn.ParamsStart.IsValid() {
		return nil
	}
	count, err := safecast.Conv[int](fn.ParamsCount)
	if err != nil {
		return nil
	}
	params := make([]ast.FnParamID, 0, count)
	base := ast.FnParamID(fn.ParamsStart)
	for i := ast.FnParamID(0); i < ast.FnParamID(fn.ParamsCount); i++ {
		params = append(params, base+i)
	}
	return params
}

func (tc *typeChecker) matchesSelfType(expected, actual types.TypeID) bool {
	if expected == types.NoTypeID || actual == types.NoTypeID || tc.types == nil {
		return true
	}
	curr := actual
	for {
		if curr == expected {
			return true
		}
		tt, ok := tc.types.Lookup(curr)
		if !ok {
			return false
		}
		switch tt.Kind {
		case types.KindReference, types.KindOwn, types.KindPointer:
			curr = tt.Elem
			continue
		default:
			return false
		}
	}
}
