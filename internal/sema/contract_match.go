package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type contractRequirements struct {
	fields  map[source.StringID]types.TypeID
	methods map[source.StringID][]methodRequirement
}

type methodRequirement struct {
	name   source.StringID
	params []types.TypeID
	result types.TypeID
	span   source.Span
}

type methodSignature struct {
	params []types.TypeID
	result types.TypeID
}

func (tc *typeChecker) checkContractSatisfaction(target types.TypeID, bound symbols.BoundInstance) bool {
	if target == types.NoTypeID || !bound.Contract.IsValid() || tc.builder == nil {
		return false
	}
	contractSym := tc.symbolFromID(bound.Contract)
	if contractSym == nil || contractSym.Kind != symbols.SymbolContract {
		return false
	}
	contractDecl, okContract := tc.builder.Items.Contract(contractSym.Decl.Item)
	if !okContract || contractDecl == nil {
		return false
	}

	args := bound.GenericArgs
	if len(contractSym.TypeParams) > 0 && len(args) != len(contractSym.TypeParams) {
		tc.report(diag.SemaTypeMismatch, bound.Span, "%s expects %d type argument(s), got %d", tc.lookupName(contractSym.Name), len(contractSym.TypeParams), len(args))
		return false
	}

	scope := tc.scopeForItem(contractSym.Decl.Item)
	pushed := false
	if len(contractSym.TypeParams) > 0 {
		pushed = tc.pushTypeParams(bound.Contract, contractSym.TypeParams, args)
	}
	if pushed {
		defer tc.popTypeParams()
	}

	reqs, okReqs := tc.contractRequirementSet(contractDecl, scope)
	ok := okReqs

	fields := tc.collectTypeFields(target)
	for name, expected := range reqs.fields {
		actual, exists := fields[name]
		if !exists {
			tc.report(diag.SemaContractMissingField, bound.Span, "missing required field '%s'", tc.lookupName(name))
			ok = false
			continue
		}
		if !tc.contractTypesEqual(expected, actual) {
			tc.report(diag.SemaContractFieldTypeError, bound.Span, "field '%s' has type %s, expected %s", tc.lookupName(name), tc.typeLabel(actual), tc.typeLabel(expected))
			ok = false
		}
	}

	for name, methods := range reqs.methods {
		for _, req := range methods {
			if !tc.ensureMethodSatisfies(target, name, req, bound.Span) {
				ok = false
			}
		}
	}

	return ok
}

func (tc *typeChecker) validateFunctionCall(sym *symbols.Symbol, call *ast.ExprCallData, argTypes []types.TypeID) {
	if sym == nil || call == nil || tc.builder == nil {
		return
	}
	fnItem, ok := tc.builder.Items.Fn(sym.Decl.Item)
	if !ok || fnItem == nil {
		return
	}
	bindings := tc.inferTypeParamBindings(sym, fnItem, argTypes)
	if len(sym.TypeParamSymbols) > 0 {
		tc.enforceContractBounds(sym.TypeParamSymbols, bindings)
	}
}

func (tc *typeChecker) inferTypeParamBindings(sym *symbols.Symbol, fn *ast.FnItem, argTypes []types.TypeID) map[source.StringID]types.TypeID {
	if sym == nil || fn == nil || len(sym.TypeParams) == 0 || tc.builder == nil {
		return nil
	}
	result := make(map[source.StringID]types.TypeID, len(sym.TypeParams))
	indexByName := make(map[source.StringID]struct{}, len(sym.TypeParams))
	for _, name := range sym.TypeParams {
		indexByName[name] = struct{}{}
	}
	paramIDs := tc.builder.Items.GetFnParamIDs(fn)
	for i, pid := range paramIDs {
		if i >= len(argTypes) {
			break
		}
		argType := argTypes[i]
		if argType == types.NoTypeID {
			continue
		}
		param := tc.builder.Items.FnParam(pid)
		if param == nil {
			continue
		}
		if name := tc.paramTypeParamName(param.Type, indexByName); name != source.NoStringID {
			result[name] = argType
		}
	}
	return result
}

func (tc *typeChecker) paramTypeParamName(typeID ast.TypeID, allowed map[source.StringID]struct{}) source.StringID {
	if typeID == ast.NoTypeID || tc.builder == nil {
		return source.NoStringID
	}
	expr := tc.builder.Types.Get(typeID)
	if expr == nil || expr.Kind != ast.TypeExprPath {
		return source.NoStringID
	}
	path, ok := tc.builder.Types.Path(typeID)
	if !ok || path == nil || len(path.Segments) != 1 {
		return source.NoStringID
	}
	seg := path.Segments[0]
	if len(seg.Generics) > 0 {
		return source.NoStringID
	}
	if _, ok := allowed[seg.Name]; ok {
		return seg.Name
	}
	return source.NoStringID
}

func (tc *typeChecker) enforceContractBounds(params []symbols.TypeParamSymbol, bindings map[source.StringID]types.TypeID) {
	if len(params) == 0 || tc.reporter == nil {
		return
	}
	for _, param := range params {
		concrete := bindings[param.Name]
		if concrete == types.NoTypeID {
			continue
		}
		for _, bound := range param.Bounds {
			inst := bound
			inst.GenericArgs = tc.substituteBoundArgs(bound.GenericArgs, bindings)
			tc.checkContractSatisfaction(concrete, inst)
		}
	}
}

func (tc *typeChecker) substituteBoundArgs(args []types.TypeID, bindings map[source.StringID]types.TypeID) []types.TypeID {
	if len(args) == 0 {
		return nil
	}
	out := make([]types.TypeID, len(args))
	for i, arg := range args {
		out[i] = tc.substituteTypeParamByName(arg, bindings)
	}
	return out
}

func (tc *typeChecker) substituteTypeParamByName(id types.TypeID, bindings map[source.StringID]types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return resolved
	}
	if tt.Kind == types.KindGenericParam {
		if name := tc.typeParamNames[resolved]; name != source.NoStringID {
			if concrete := bindings[name]; concrete != types.NoTypeID {
				return concrete
			}
		}
		return resolved
	}
	switch tt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn:
		elem := tc.substituteTypeParamByName(tt.Elem, bindings)
		if elem == tt.Elem {
			return resolved
		}
		clone := tt
		clone.Elem = elem
		return tc.types.Intern(clone)
	case types.KindArray:
		elem := tc.substituteTypeParamByName(tt.Elem, bindings)
		if elem == tt.Elem {
			return resolved
		}
		clone := tt
		clone.Elem = elem
		return tc.types.Intern(clone)
	default:
		return resolved
	}
}

func (tc *typeChecker) contractRequirementSet(contractDecl *ast.ContractDecl, scope symbols.ScopeID) (contractRequirements, bool) {
	reqs := contractRequirements{
		fields:  make(map[source.StringID]types.TypeID),
		methods: make(map[source.StringID][]methodRequirement),
	}
	if contractDecl == nil {
		return reqs, false
	}
	ok := true
	members := tc.builder.Items.GetContractItemIDs(contractDecl)
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
			fieldType := tc.resolveTypeExprWithScope(field.Type, scope)
			if fieldType == types.NoTypeID {
				ok = false
				continue
			}
			reqs.fields[field.Name] = fieldType
		case ast.ContractItemFn:
			fn := tc.builder.Items.ContractFn(ast.ContractFnID(member.Payload))
			if fn == nil {
				continue
			}
			if req, okMethod := tc.contractMethodRequirement(fn, scope); okMethod {
				reqs.methods[fn.Name] = append(reqs.methods[fn.Name], req)
			} else {
				ok = false
			}
		}
	}
	return reqs, ok
}

func (tc *typeChecker) contractMethodRequirement(fn *ast.ContractFnReq, scope symbols.ScopeID) (methodRequirement, bool) {
	req := methodRequirement{}
	if fn == nil {
		return req, false
	}
	req.name = fn.Name
	req.span = fn.Span

	paramIDs := tc.getContractFnParamIDs(fn)
	req.params = make([]types.TypeID, 0, len(paramIDs))
	ok := true
	for _, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil {
			req.params = append(req.params, types.NoTypeID)
			ok = false
			continue
		}
		paramType := tc.resolveTypeExprWithScope(param.Type, scope)
		req.params = append(req.params, paramType)
		if paramType == types.NoTypeID {
			ok = false
		}
	}
	req.result = tc.types.Builtins().Nothing
	if fn.ReturnType.IsValid() {
		req.result = tc.resolveTypeExprWithScope(fn.ReturnType, scope)
		if req.result == types.NoTypeID {
			ok = false
		}
	}
	return req, ok
}

func (tc *typeChecker) collectTypeFields(target types.TypeID) map[source.StringID]types.TypeID {
	fields := make(map[source.StringID]types.TypeID)
	info, _ := tc.structInfoForType(target)
	if info == nil {
		return fields
	}
	for _, field := range info.Fields {
		fields[field.Name] = field.Type
	}
	return fields
}

func (tc *typeChecker) ensureMethodSatisfies(target types.TypeID, name source.StringID, req methodRequirement, boundSpan source.Span) bool {
	if len(req.params) > 0 && !tc.contractTypesEqual(req.params[0], target) {
		tc.report(diag.SemaContractSelfType, boundSpan, "contract method '%s' must have self of type %s, got %s", tc.lookupName(name), tc.typeLabel(target), tc.typeLabel(req.params[0]))
		return false
	}

	actual := tc.methodsForType(target, name)
	if len(actual) == 0 {
		tc.report(diag.SemaContractMissingMethod, boundSpan, "missing required method '%s'", tc.lookupName(name))
		return false
	}

	signatureMismatch := false
	for _, cand := range actual {
		if len(cand.params) != len(req.params) {
			signatureMismatch = true
			continue
		}
		if len(cand.params) > 0 && !tc.contractTypesEqual(cand.params[0], target) {
			signatureMismatch = true
			continue
		}
		if tc.contractSignatureMatches(req, cand) {
			return true
		}
		signatureMismatch = true
	}
	if signatureMismatch {
		tc.report(diag.SemaContractMethodMismatch, boundSpan, "method '%s' has incompatible signature, expected %s", tc.lookupName(name), tc.formatSignature(req))
	} else {
		tc.report(diag.SemaContractMissingMethod, boundSpan, "missing required method '%s'", tc.lookupName(name))
	}
	return false
}

func (tc *typeChecker) methodsForType(target types.TypeID, name source.StringID) []methodSignature {
	if target == types.NoTypeID || name == source.NoStringID {
		return nil
	}
	nameLiteral := tc.lookupName(name)
	if nameLiteral == "" {
		return nil
	}

	candidates := tc.typeKeyCandidates(target)
	methods := make([]methodSignature, 0, 4)

	for _, cand := range candidates {
		if cand.key == "" {
			continue
		}
		for _, sig := range tc.lookupMagicMethods(cand.key, nameLiteral) {
			if sig == nil || len(sig.Params) == 0 || !typeKeyEqual(sig.Params[0], cand.key) {
				continue
			}
			if ms, ok := tc.signatureToTypes(sig); ok {
				methods = append(methods, ms)
			}
		}
	}

	if tc.symbols != nil && tc.symbols.Table != nil && tc.symbols.Table.Symbols != nil {
		if data := tc.symbols.Table.Symbols.Data(); data != nil {
			for i := range data {
				sym := &data[i]
				if sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
					continue
				}
				if tc.symbolName(sym.Name) != nameLiteral {
					continue
				}
				if len(sym.Signature.Params) == 0 {
					continue
				}
				for _, cand := range candidates {
					if cand.key == "" || !typeKeyEqual(sym.Signature.Params[0], cand.key) {
						continue
					}
					if ms, ok := tc.signatureToTypes(sym.Signature); ok {
						methods = append(methods, ms)
					}
					break
				}
			}
		}
	}

	return methods
}

func (tc *typeChecker) signatureToTypes(sig *symbols.FunctionSignature) (methodSignature, bool) {
	ms := methodSignature{}
	if sig == nil {
		return ms, false
	}
	params := make([]types.TypeID, 0, len(sig.Params))
	ok := true
	for _, p := range sig.Params {
		paramType := tc.typeFromKey(p)
		params = append(params, paramType)
		if paramType == types.NoTypeID {
			ok = false
		}
	}
	ms.params = params
	ms.result = tc.typeFromKey(sig.Result)
	if ms.result == types.NoTypeID {
		ok = false
	}
	return ms, ok
}

func (tc *typeChecker) contractSignatureMatches(expected methodRequirement, actual methodSignature) bool {
	if len(expected.params) != len(actual.params) {
		return false
	}
	for i := range expected.params {
		if !tc.contractTypesEqual(expected.params[i], actual.params[i]) {
			return false
		}
	}
	return tc.contractTypesEqual(expected.result, actual.result)
}

func (tc *typeChecker) contractTypesEqual(expected, actual types.TypeID) bool {
	if expected == types.NoTypeID || actual == types.NoTypeID {
		return false
	}
	return tc.resolveAlias(expected) == tc.resolveAlias(actual)
}

func (tc *typeChecker) formatSignature(req methodRequirement) string {
	name := tc.lookupName(req.name)
	parts := make([]string, 0, len(req.params))
	for _, p := range req.params {
		parts = append(parts, tc.typeLabel(p))
	}
	result := tc.typeLabel(req.result)
	return fmt.Sprintf("fn %s(%s) -> %s", name, joinSignature(parts), result)
}

func joinSignature(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("self: %s", parts[0])
	default:
		result := fmt.Sprintf("self: %s", parts[0])
		for _, p := range parts[1:] {
			result += ", " + p
		}
		return result
	}
}
