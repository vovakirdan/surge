package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
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
	// Трассировка проверки контракта
	var span *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDebug {
		span = trace.Begin(tc.tracer, trace.ScopeNode, "check_contract_satisfaction", 0)
		span.WithExtra("type", tc.typeLabel(target))
	}
	defer func() {
		if span != nil {
			span.End("")
		}
	}()

	if target == types.NoTypeID || !bound.Contract.IsValid() || tc.builder == nil {
		return false
	}
	contractSym := tc.symbolFromID(bound.Contract)
	if contractSym == nil || contractSym.Kind != symbols.SymbolContract {
		return false
	}
	var contractDecl *ast.ContractDecl
	okContract := false
	if tc.builder != nil {
		contractDecl, okContract = tc.builder.Items.Contract(contractSym.Decl.Item)
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
		paramSpecs := specsFromSymbolParams(contractSym.TypeParamSymbols)
		pushed = tc.pushTypeParams(bound.Contract, paramSpecs, args)
	}
	if pushed {
		defer tc.popTypeParams()
	}

	var (
		reqs   contractRequirements
		okReqs bool
	)
	switch {
	case contractSym.Contract != nil:
		reqs = tc.instantiateContractRequirements(contractSym, contractSym.Contract, args)
		okReqs = true
	case okContract && contractDecl != nil:
		reqs, okReqs = tc.contractRequirementSet(contractDecl, scope)
	default:
		return false
	}
	ok := okReqs

	fields := tc.collectTypeFields(target)
	fieldAttrs := tc.collectFieldAttrs(target)
	var missingFields []string
	fieldCount := 0
	for name, expected := range reqs.fields {
		fieldCount++
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
	methodCount := 0
	for name, methods := range reqs.methods {
		for idx := range methods {
			methodCount++
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
	if span != nil {
		span.WithExtra("fields_checked", fmt.Sprintf("%d", fieldCount))
		span.WithExtra("methods_checked", fmt.Sprintf("%d", methodCount))
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

func requirementsFromSpec(spec *symbols.ContractSpec) contractRequirements {
	reqs := contractRequirements{
		fields:     make(map[source.StringID]types.TypeID),
		fieldAttrs: make(map[source.StringID][]source.StringID),
		methods:    make(map[source.StringID][]methodRequirement),
	}
	if spec == nil {
		return reqs
	}
	for name, ty := range spec.Fields {
		reqs.fields[name] = ty
	}
	for name, attrs := range spec.FieldAttrs {
		reqs.fieldAttrs[name] = append([]source.StringID(nil), attrs...)
	}
	for name, methods := range spec.Methods {
		for _, m := range methods {
			reqs.methods[name] = append(reqs.methods[name], methodRequirement{
				name:   m.Name,
				params: append([]types.TypeID(nil), m.Params...),
				result: m.Result,
				span:   m.Span,
				attrs:  append([]source.StringID(nil), m.Attrs...),
				pub:    m.Public,
				async:  m.Async,
			})
		}
	}
	return reqs
}

func (tc *typeChecker) instantiateContractRequirements(sym *symbols.Symbol, spec *symbols.ContractSpec, args []types.TypeID) contractRequirements {
	reqs := requirementsFromSpec(spec)
	if tc == nil || sym == nil || spec == nil {
		return reqs
	}
	if len(args) == 0 || len(sym.TypeParams) == 0 {
		return reqs
	}
	bindings := make(map[source.StringID]bindingInfo, len(sym.TypeParams))
	for idx, name := range sym.TypeParams {
		if idx >= len(args) {
			break
		}
		if name == source.NoStringID || args[idx] == types.NoTypeID {
			continue
		}
		bindings[name] = bindingInfo{typ: args[idx]}
	}
	if len(bindings) == 0 {
		return reqs
	}
	for name, ty := range reqs.fields {
		reqs.fields[name] = tc.substituteTypeParamByName(ty, bindings)
	}
	for mname, methods := range reqs.methods {
		for idx := range methods {
			for i := range methods[idx].params {
				methods[idx].params[i] = tc.substituteTypeParamByName(methods[idx].params[i], bindings)
			}
			methods[idx].result = tc.substituteTypeParamByName(methods[idx].result, bindings)
		}
		reqs.methods[mname] = methods
	}
	return reqs
}
