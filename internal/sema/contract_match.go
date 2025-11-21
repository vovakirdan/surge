package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type contractRequirements struct {
	fields     map[source.StringID]types.TypeID
	fieldAttrs map[source.StringID][]source.StringID
	methods    map[source.StringID][]methodRequirement
}

type methodRequirement struct {
	name   source.StringID
	params []types.TypeID
	result types.TypeID
	span   source.Span
	attrs  []source.StringID
	pub    bool
	async  bool
}

type methodSignature struct {
	params []types.TypeID
	result types.TypeID
	attrs  []source.StringID
	pub    bool
	async  bool
}

type bindingInfo struct {
	typ  types.TypeID
	span source.Span
	sym  symbols.SymbolID
}

func (tc *typeChecker) checkContractSatisfaction(target types.TypeID, bound symbols.BoundInstance, hintSpan source.Span, typeName string) bool {
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

	reportSpan := hintSpan
	if reportSpan == (source.Span{}) {
		reportSpan = bound.Span
	}
	if reportSpan == (source.Span{}) {
		reportSpan = contractSym.Span
	}

	typeLabel := typeName
	if typeLabel == "" {
		typeLabel = tc.contractTypeLabel(target)
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
	fieldAttrs := tc.collectFieldAttrs(target)
	var missingFields []string
	for name, expected := range reqs.fields {
		actual, exists := fields[name]
		if !exists {
			missingFields = append(missingFields, tc.lookupName(name))
			continue
		}
		if !tc.contractTypesEqual(expected, actual) {
			tc.report(diag.SemaContractFieldTypeError, reportSpan, "type %s field '%s' has type %s, expected %s (contract %s)", typeLabel, tc.lookupName(name), tc.typeLabel(actual), tc.typeLabel(expected), tc.lookupName(contractSym.Name))
			ok = false
			continue
		}
		if !tc.attrSetsEqual(reqs.fieldAttrs[name], fieldAttrs[name]) {
			tc.report(diag.SemaContractFieldAttrMismatch, reportSpan, "type %s field '%s' attributes differ from contract %s: expected [%s], got [%s]", typeLabel, tc.lookupName(name), tc.lookupName(contractSym.Name), joinAttrNames(tc, reqs.fieldAttrs[name]), joinAttrNames(tc, fieldAttrs[name]))
			ok = false
		}
	}
	if len(missingFields) > 0 {
		fieldLabel := "field"
		if len(missingFields) > 1 {
			fieldLabel = "fields"
		}
		tc.report(diag.SemaContractMissingField, reportSpan, "type `%s` missing required %s by contract `%s`: %s", typeLabel, fieldLabel, tc.lookupName(contractSym.Name), joinNames(missingFields))
		ok = false
	}

	var missingMethods []string
	var mismatchedMethods []string
	var attrMismatchedMethods []string
	for name, methods := range reqs.methods {
		for idx := range methods {
			req := &methods[idx]
			switch tc.ensureMethodSatisfies(target, name, req, reportSpan, tc.lookupName(contractSym.Name)) {
			case -1:
				missingMethods = append(missingMethods, tc.lookupName(name))
				ok = false
			case 0:
				mismatchedMethods = append(mismatchedMethods, tc.lookupName(name))
				ok = false
			case -2:
				attrMismatchedMethods = append(attrMismatchedMethods, tc.lookupName(name))
				ok = false
			}
		}
	}

	if len(missingMethods) > 0 {
		methodLabel := "method"
		if len(missingMethods) > 1 {
			methodLabel = "methods"
		}
		tc.report(diag.SemaContractMissingMethod, reportSpan, "type `%s` missing required %s by contract `%s`: %s", typeLabel, methodLabel, tc.lookupName(contractSym.Name), joinNames(missingMethods))
	}
	if len(mismatchedMethods) > 0 {
		methodLabel := "method"
		if len(mismatchedMethods) > 1 {
			methodLabel = "methods"
		}
		tc.report(diag.SemaContractMethodMismatch, reportSpan, "type `%s` has incompatible %s for contract `%s`: %s", typeLabel, methodLabel, tc.lookupName(contractSym.Name), joinNames(mismatchedMethods))
	}
	if len(attrMismatchedMethods) > 0 {
		methodLabel := "method"
		if len(attrMismatchedMethods) > 1 {
			methodLabel = "methods"
		}
		tc.report(diag.SemaContractMethodAttrMismatch, reportSpan, "type `%s` has attribute/modifier mismatch for %s in contract `%s`: %s", typeLabel, methodLabel, tc.lookupName(contractSym.Name), joinNames(attrMismatchedMethods))
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
	bindings := tc.inferTypeParamBindings(sym, fnItem, argTypes, call)
	if len(sym.TypeParamSymbols) > 0 {
		tc.enforceContractBounds(sym.TypeParamSymbols, bindings, tc.exprSpan(call.Target))
	}
}

func (tc *typeChecker) inferTypeParamBindings(sym *symbols.Symbol, fn *ast.FnItem, argTypes []types.TypeID, call *ast.ExprCallData) map[source.StringID]bindingInfo {
	if sym == nil || fn == nil || len(sym.TypeParams) == 0 || tc.builder == nil || call == nil {
		return nil
	}
	result := make(map[source.StringID]bindingInfo, len(sym.TypeParams))
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
		argSpan := tc.exprSpan(call.Args[i])
		argSym := tc.symbolForExpr(call.Args[i])
		argValType := tc.valueType(argType)
		if argSym.IsValid() {
			if boundType := tc.bindingType(argSym); boundType != types.NoTypeID {
				argValType = boundType
			}
		}
		param := tc.builder.Items.FnParam(pid)
		if param == nil {
			continue
		}
		if name := tc.paramTypeParamName(param.Type, indexByName); name != source.NoStringID {
			result[name] = bindingInfo{typ: argValType, span: argSpan, sym: argSym}
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

func (tc *typeChecker) enforceContractBounds(params []symbols.TypeParamSymbol, bindings map[source.StringID]bindingInfo, span source.Span) {
	if len(params) == 0 || tc.reporter == nil {
		return
	}
	for _, param := range params {
		binding := bindings[param.Name]
		concrete := binding.typ
		if concrete == types.NoTypeID {
			continue
		}
		reportSpan := binding.span
		if reportSpan == (source.Span{}) {
			reportSpan = span
		}
		typeLabel := tc.bindingTypeLabel(binding)
		for _, bound := range param.Bounds {
			inst := bound
			inst.GenericArgs = tc.substituteBoundArgs(bound.GenericArgs, bindings)
			tc.checkContractSatisfaction(concrete, inst, reportSpan, typeLabel)
		}
	}
}

func (tc *typeChecker) substituteBoundArgs(args []types.TypeID, bindings map[source.StringID]bindingInfo) []types.TypeID {
	if len(args) == 0 {
		return nil
	}
	out := make([]types.TypeID, len(args))
	for i, arg := range args {
		out[i] = tc.substituteTypeParamByName(arg, bindings)
	}
	return out
}

func (tc *typeChecker) substituteTypeParamByName(id types.TypeID, bindings map[source.StringID]bindingInfo) types.TypeID {
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
			if concrete := bindings[name].typ; concrete != types.NoTypeID {
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
		fields:     make(map[source.StringID]types.TypeID),
		fieldAttrs: make(map[source.StringID][]source.StringID),
		methods:    make(map[source.StringID][]methodRequirement),
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
			reqs.fieldAttrs[field.Name] = tc.attrNames(field.AttrStart, field.AttrCount)
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
	req.attrs = tc.attrNames(fn.AttrStart, fn.AttrCount)
	req.pub = fn.Flags&ast.FnModifierPublic != 0
	req.async = fn.Flags&ast.FnModifierAsync != 0

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

func (tc *typeChecker) collectFieldAttrs(target types.TypeID) map[source.StringID][]source.StringID {
	attrMap := make(map[source.StringID][]source.StringID)
	info, _ := tc.structInfoForType(target)
	if info == nil {
		return attrMap
	}
	for _, field := range info.Fields {
		attrMap[field.Name] = field.Attrs
	}
	return attrMap
}

// returns 1 if satisfied, 0 if signature mismatch, -1 if missing entirely, -2 if only attrs/modifiers mismatch
func (tc *typeChecker) ensureMethodSatisfies(target types.TypeID, name source.StringID, req *methodRequirement, reportSpan source.Span, contractName string) int {
	if req == nil {
		return 0
	}
	if len(req.params) > 0 && !tc.contractTypesEqual(req.params[0], target) {
		tc.report(diag.SemaContractSelfType, reportSpan, "type %s method '%s' must have self %s per contract %s, got %s", tc.contractTypeLabel(target), tc.lookupName(name), tc.typeLabel(target), contractName, tc.typeLabel(req.params[0]))
		return 0
	}

	actual := tc.methodsForType(target, name)
	if len(actual) == 0 {
		return -1
	}

	attrMismatch := false
	for _, cand := range actual {
		if len(cand.params) != len(req.params) {
			continue
		}
		if len(cand.params) > 0 && !tc.contractTypesEqual(cand.params[0], target) {
			continue
		}
		if match, attrBad := tc.contractSignatureMatches(req, cand); match {
			return 1
		} else if attrBad {
			attrMismatch = true
		}
	}
	if attrMismatch {
		return -2
	}
	return 0
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
				ms.pub = false
				ms.async = false
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
						ms.pub = sym.Flags&symbols.SymbolFlagPublic != 0
						if fn, okFn := tc.builder.Items.Fn(sym.Decl.Item); okFn && fn != nil {
							ms.attrs = tc.attrNames(fn.AttrStart, fn.AttrCount)
							ms.async = fn.Flags&ast.FnModifierAsync != 0
						}
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

func (tc *typeChecker) contractSignatureMatches(expected *methodRequirement, actual methodSignature) (match, attrMismatch bool) {
	if expected == nil {
		return false, false
	}
	if len(expected.params) != len(actual.params) {
		return false, false
	}
	for i := range expected.params {
		if !tc.contractTypesEqual(expected.params[i], actual.params[i]) {
			return false, false
		}
	}
	if !tc.contractTypesEqual(expected.result, actual.result) {
		return false, false
	}
	if expected.pub != actual.pub || expected.async != actual.async {
		return false, true
	}
	if !tc.attrSetsEqual(expected.attrs, actual.attrs) {
		return false, true
	}
	return true, false
}

func (tc *typeChecker) contractTypesEqual(expected, actual types.TypeID) bool {
	if expected == types.NoTypeID || actual == types.NoTypeID {
		return false
	}
	return tc.resolveAlias(expected) == tc.resolveAlias(actual)
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	result := "`" + names[0] + "`"
	for _, n := range names[1:] {
		result += ", `" + n + "`"
	}
	return result
}

func joinAttrNames(tc *typeChecker, attrs []source.StringID) string {
	if tc == nil || len(attrs) == 0 {
		return ""
	}
	names := make([]string, 0, len(attrs))
	for _, id := range attrs {
		if name := tc.lookupName(id); name != "" {
			names = append(names, "@"+name)
		}
	}
	return strings.Join(names, ", ")
}

func (tc *typeChecker) attrSetsEqual(expected, actual []source.StringID) bool {
	if len(expected) == 0 && len(actual) == 0 {
		return true
	}
	set := make(map[source.StringID]int, len(expected))
	for _, a := range expected {
		set[a]++
	}
	for _, a := range actual {
		if set[a] == 0 {
			return false
		}
		set[a]--
	}
	for _, v := range set {
		if v != 0 {
			return false
		}
	}
	return true
}

func (tc *typeChecker) bindingTypeLabel(b bindingInfo) string {
	if b.sym.IsValid() {
		if t := tc.bindingType(b.sym); t != types.NoTypeID {
			if l := tc.contractTypeLabel(t); l != "" && l != "_" {
				return l
			}
		}
		if sym := tc.symbolFromID(b.sym); sym != nil {
			if sym.Kind == symbols.SymbolLet && sym.Decl.Stmt.IsValid() {
				if letStmt := tc.builder.Stmts.Let(sym.Decl.Stmt); letStmt != nil {
					scope := tc.scopeForStmt(sym.Decl.Stmt)
					if declType := tc.resolveTypeExprWithScope(letStmt.Type, scope); declType != types.NoTypeID {
						if l := tc.contractTypeLabel(declType); l != "" && l != "_" {
							return l
						}
					}
				}
			}
		}
		if sym := tc.symbolFromID(b.sym); sym != nil && sym.Name != source.NoStringID {
			if name := tc.lookupName(sym.Name); name != "" {
				return name
			}
		}
	}
	label := tc.contractTypeLabel(b.typ)
	if label != "" && label != "_" {
		return label
	}
	return tc.typeLabel(b.typ)
}

func (tc *typeChecker) contractTypeLabel(id types.TypeID) string {
	if id == types.NoTypeID || tc.types == nil {
		return tc.typeLabel(id)
	}
	resolved := tc.resolveAlias(id)
	if info, ok := tc.types.StructInfo(resolved); ok && info != nil && info.Name != source.NoStringID {
		if name := tc.lookupName(info.Name); name != "" {
			return name
		}
	}
	if info, ok := tc.types.AliasInfo(resolved); ok && info != nil && info.Name != source.NoStringID {
		if name := tc.lookupName(info.Name); name != "" {
			return name
		}
	}
	if info, ok := tc.types.UnionInfo(resolved); ok && info != nil && info.Name != source.NoStringID {
		if name := tc.lookupName(info.Name); name != "" {
			return name
		}
	}
	if info, ok := tc.types.TypeParamInfo(resolved); ok && info != nil {
		if name := tc.lookupName(info.Name); name != "" {
			return name
		}
	}
	return tc.typeLabel(id)
}
