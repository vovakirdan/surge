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

	symID := tc.typeSymbolForItem(id)
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetTypeParamIDs(decl.TypeParamsStart, decl.TypeParamsCount), scope)
	if len(paramSpecs) == 0 && len(decl.Generics) > 0 {
		paramSpecs = specsFromNames(decl.Generics)
	}
	typeParamsPushed := tc.pushTypeParams(symID, paramSpecs, nil)
	typeParamSet := make(map[source.StringID]struct{}, len(paramSpecs))
	typeParamUsage := make(map[source.StringID]bool, len(paramSpecs))
	var contractSpec *symbols.ContractSpec
	if sym := tc.symbolFromID(symID); sym != nil {
		contractSpec = symbols.NewContractSpec()
	}
	if typeParamsPushed {
		for _, spec := range paramSpecs {
			typeParamSet[spec.name] = struct{}{}
			typeParamUsage[spec.name] = false
		}
	}

	markUsage := func(typeID ast.TypeID) {
		tc.markTypeParamUsage(typeID, typeParamSet, typeParamUsage)
	}

	paramIDs := tc.builder.Items.GetTypeParamIDs(decl.TypeParamsStart, decl.TypeParamsCount)
	if len(paramIDs) > 0 {
		bounds := tc.resolveTypeParamBounds(paramIDs, scope, markUsage)
		tc.attachTypeParamSymbols(symID, bounds)
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
			tc.validateAttrs(field.AttrStart, field.AttrCount, ast.AttrTargetField, diag.SemaContractUnknownAttr)
			if field.Type.IsValid() {
				fieldType := tc.resolveTypeExprWithScope(field.Type, scope)
				markUsage(field.Type)
				if contractSpec != nil && fieldType != types.NoTypeID {
					contractSpec.AddField(field.Name, fieldType, tc.attrNames(field.AttrStart, field.AttrCount))
				}
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
			tc.validateAttrs(fn.AttrStart, fn.AttrCount, ast.AttrTargetFn, diag.SemaContractUnknownAttr)
			method, okMethod := tc.checkContractMethod(fn, scope, markUsage)
			if contractSpec != nil && okMethod && method != nil {
				contractSpec.AddMethod(method)
			}
			if !okMethod {
				continue
			}
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
	if contractSpec != nil {
		if sym := tc.symbolFromID(symID); sym != nil {
			sym.Contract = contractSpec
		}
	}
}

func (tc *typeChecker) checkContractMethod(fn *ast.ContractFnReq, scope symbols.ScopeID, markUsage func(ast.TypeID)) (*symbols.ContractMethod, bool) {
	if fn == nil {
		return nil, false
	}
	ok := true
	if fn.Body.IsValid() {
		tc.report(diag.SemaContractMethodBody, fn.Span, "contract methods must not have bodies")
	}

	method := &symbols.ContractMethod{
		Name:   fn.Name,
		Span:   fn.Span,
		Attrs:  tc.attrNames(fn.AttrStart, fn.AttrCount),
		Public: fn.Flags&ast.FnModifierPublic != 0,
		Async:  fn.Flags&ast.FnModifierAsync != 0,
	}

	if fn.ReturnType.IsValid() {
		method.Result = tc.resolveTypeExprWithScope(fn.ReturnType, scope)
		markUsage(fn.ReturnType)
		if method.Result == types.NoTypeID {
			ok = false
		}
	}

	paramIDs := tc.getContractFnParamIDs(fn)
	for _, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil || !param.Type.IsValid() {
			method.Params = append(method.Params, types.NoTypeID)
			ok = false
			continue
		}
		paramType := tc.resolveTypeExprWithScope(param.Type, scope)
		markUsage(param.Type)
		method.Params = append(method.Params, paramType)
		if paramType == types.NoTypeID {
			ok = false
		}
	}
	return method, ok
}

func (tc *typeChecker) validateAttrs(start ast.AttrID, count uint32, target ast.AttrTargetMask, code diag.Code) {
	if count == 0 || !start.IsValid() {
		return
	}
	attrs := tc.builder.Items.CollectAttrs(start, count)
	for _, attr := range attrs {
		if spec, ok := ast.LookupAttrID(tc.builder.StringsInterner, attr.Name); ok {
			if !spec.Allows(target) {
				tc.report(code, attr.Span, "attribute '@%s' is not allowed here", tc.lookupName(attr.Name))
			}
			continue
		}
		tc.report(code, attr.Span, "unknown attribute '@%s'", tc.lookupName(attr.Name))
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
