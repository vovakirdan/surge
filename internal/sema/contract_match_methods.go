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

func (tc *typeChecker) collectTypeFields(target types.TypeID) map[source.StringID]types.TypeID {
	fields := make(map[source.StringID]types.TypeID)
	target = tc.valueType(target)
	if target == types.NoTypeID {
		return fields
	}
	if info, _ := tc.structInfoForType(target); info != nil {
		for _, field := range info.Fields {
			fields[field.Name] = field.Type
		}
	}
	for _, field := range tc.externFieldsForType(target) {
		if _, exists := fields[field.Name]; !exists {
			fields[field.Name] = field.Type
		}
	}
	return fields
}

func (tc *typeChecker) collectFieldAttrs(target types.TypeID) map[source.StringID][]source.StringID {
	attrMap := make(map[source.StringID][]source.StringID)
	target = tc.valueType(target)
	if target == types.NoTypeID {
		return attrMap
	}
	if info, _ := tc.structInfoForType(target); info != nil {
		for _, field := range info.Fields {
			attrMap[field.Name] = field.Attrs
		}
	}
	for _, field := range tc.externFieldsForType(target) {
		if _, exists := attrMap[field.Name]; !exists {
			attrMap[field.Name] = field.Attrs
		}
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
		var aligned methodRequirement
		switch {
		case len(cand.params) == len(req.params):
			aligned = *req
		case len(cand.params) == len(req.params)+1:
			aligned = *req
			aligned.params = append([]types.TypeID{target}, req.params...)
		default:
			continue
		}
		if len(aligned.params) > 0 && !tc.contractTypesEqual(aligned.params[0], target) {
			continue
		}
		if match, attrBad := tc.contractSignatureMatches(&aligned, cand); match {
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
	// Трассировка поиска методов для типа
	var span *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDebug {
		span = trace.Begin(tc.tracer, trace.ScopeNode, "methods_for_type", 0)
		span.WithExtra("type", tc.typeLabel(target))
	}
	defer func() {
		if span != nil {
			span.End("")
		}
	}()

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

	if span != nil {
		span.WithExtra("found", fmt.Sprintf("%d", len(methods)))
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
	expectedRes := tc.resolveAlias(expected)
	actualRes := tc.resolveAlias(actual)
	if expectedRes == actualRes {
		return true
	}
	// Structural comparison for wrapper types
	if tc.types == nil {
		return false
	}
	expInfo, okExp := tc.types.Lookup(expectedRes)
	actInfo, okAct := tc.types.Lookup(actualRes)
	if !okExp || !okAct {
		return false
	}
	if expInfo.Kind != actInfo.Kind {
		return false
	}
	switch expInfo.Kind {
	case types.KindReference, types.KindPointer, types.KindOwn:
		// Recursively compare inner element types
		return tc.contractTypesEqual(expInfo.Elem, actInfo.Elem)
	case types.KindGenericParam:
		// Compare generic params by name - they may be structurally equivalent
		// even if registered with different IDs
		expName := tc.typeParamName(expectedRes)
		actName := tc.typeParamName(actualRes)
		return expName != source.NoStringID && expName == actName
	case types.KindArray:
		// Arrays need same element type and count
		if expInfo.Count != actInfo.Count {
			return false
		}
		return tc.contractTypesEqual(expInfo.Elem, actInfo.Elem)
	}
	return false
}

// typeParamName returns the name of a type parameter, using cache or TypeParamInfo.
func (tc *typeChecker) typeParamName(id types.TypeID) source.StringID {
	if name := tc.typeParamNames[id]; name != source.NoStringID {
		return name
	}
	if info, ok := tc.types.TypeParamInfo(id); ok && info != nil {
		if info.Name != source.NoStringID {
			tc.typeParamNames[id] = info.Name
		}
		return info.Name
	}
	return source.NoStringID
}
