package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) indexResultType(container, index types.TypeID, span source.Span) types.TypeID {
	if container == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	base := tc.valueType(container)
	if base == types.NoTypeID {
		return types.NoTypeID
	}
	intType := tc.types.Builtins().Int
	if elem, ok := tc.arrayElemType(base); ok {
		if index != types.NoTypeID && intType != types.NoTypeID {
			if tc.sameType(index, intType) {
				return elem
			}
			if payload, ok := tc.rangePayload(index); ok && tc.sameType(payload, intType) {
				return tc.instantiateArrayType(elem)
			}
			tc.report(diag.SemaTypeMismatch, span, "array index must be int or Range<int>, got %s", tc.typeLabel(index))
			return types.NoTypeID
		}
		return elem
	}
	tt, ok := tc.types.Lookup(base)
	if !ok {
		return types.NoTypeID
	}
	switch tt.Kind {
	case types.KindString:
		if index != types.NoTypeID && intType != types.NoTypeID && !tc.sameType(index, intType) {
			tc.report(diag.SemaTypeMismatch, span, "string index must be int, got %s", tc.typeLabel(index))
			return types.NoTypeID
		}
		return tc.types.Builtins().Uint
	default:
		tc.report(diag.SemaTypeMismatch, span, "%s is not indexable", tc.typeLabel(base))
		return types.NoTypeID
	}
}

func (tc *typeChecker) memberResultType(base types.TypeID, field source.StringID, span source.Span) types.TypeID {
	if base == types.NoTypeID || field == source.NoStringID {
		return types.NoTypeID
	}
	base = tc.valueType(base)
	if base == types.NoTypeID {
		return types.NoTypeID
	}
	if ty := tc.boundFieldType(base, field); ty != types.NoTypeID {
		return ty
	}
	info, structType := tc.structInfoForType(base)
	externFields := tc.externFieldsForType(base)
	if info != nil {
		for _, f := range info.Fields {
			if f.Name == field {
				return f.Type
			}
		}
		for _, f := range externFields {
			if f.Name == field {
				return f.Type
			}
		}
		tc.report(diag.SemaUnresolvedSymbol, span, "%s has no field %s", tc.typeLabel(structType), tc.lookupName(field))
		return types.NoTypeID
	}
	for _, f := range externFields {
		if f.Name == field {
			return f.Type
		}
	}
	if len(externFields) > 0 {
		tc.report(diag.SemaUnresolvedSymbol, span, "%s has no field %s", tc.typeLabel(base), tc.lookupName(field))
		return types.NoTypeID
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "%s has no field %s", tc.typeLabel(base), tc.lookupName(field))
	return types.NoTypeID
}

func (tc *typeChecker) tupleIndexResultType(tupleType types.TypeID, index uint32, span source.Span) types.TypeID {
	if tupleType == types.NoTypeID {
		return types.NoTypeID
	}
	base := tc.valueType(tupleType)
	if base == types.NoTypeID {
		return types.NoTypeID
	}
	info, ok := tc.types.TupleInfo(base)
	if !ok || info == nil {
		tc.report(diag.SemaTypeMismatch, span, "%s is not a tuple", tc.typeLabel(base))
		return types.NoTypeID
	}
	if int(index) >= len(info.Elems) {
		tc.report(diag.SemaIndexOutOfBounds, span, "tuple index %d out of bounds (length %d)", index, len(info.Elems))
		return types.NoTypeID
	}
	return info.Elems[index]
}

// inferStructLiteralType attempts to infer generic type arguments for a struct literal
// from its field expressions. It returns the inferred struct type and a flag indicating
// whether inference was attempted (and diagnostics, if any, were already produced).
func (tc *typeChecker) inferStructLiteralType(data *ast.ExprStructData, scope symbols.ScopeID, span source.Span) (types.TypeID, bool) {
	if data == nil || tc.builder == nil || tc.types == nil || !data.Type.IsValid() {
		return types.NoTypeID, false
	}
	scope = tc.scopeOrFile(scope)

	path, ok := tc.builder.Types.Path(data.Type)
	if !ok || path == nil || len(path.Segments) == 0 {
		return types.NoTypeID, false
	}
	// Only support unqualified paths for inference; qualified paths fall back to regular resolution.
	if len(path.Segments) != 1 {
		return types.NoTypeID, false
	}
	seg := path.Segments[0]
	// If explicit type arguments are provided, let regular resolution handle it.
	if len(seg.Generics) > 0 {
		return types.NoTypeID, false
	}

	symID := tc.lookupTypeSymbol(seg.Name, scope)
	if !symID.IsValid() {
		return types.NoTypeID, false
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || len(sym.TypeParams) == 0 {
		return types.NoTypeID, false
	}

	info, structType := tc.structInfoForType(tc.symbolType(symID))
	if info == nil {
		return types.NoTypeID, false
	}

	paramNames := make([]string, len(sym.TypeParams))
	paramSet := make(map[string]struct{}, len(sym.TypeParams))
	for i, nameID := range sym.TypeParams {
		name := tc.lookupName(nameID)
		if name == "" {
			name = "_"
		}
		paramNames[i] = name
		paramSet[name] = struct{}{}
	}

	bindings := make(map[string]types.TypeID, len(paramNames))

	externFields := tc.externFieldsForType(structType)
	expectedByName := make(map[source.StringID]types.TypeID, len(info.Fields)+len(externFields))
	for _, f := range info.Fields {
		expectedByName[f.Name] = f.Type
	}
	for _, f := range externFields {
		if _, exists := expectedByName[f.Name]; !exists {
			expectedByName[f.Name] = f.Type
		}
	}

	if data.Positional {
		limit := len(data.Fields)
		if len(info.Fields) < limit {
			limit = len(info.Fields)
		}
		for idx := range limit {
			tc.bindStructFieldParam(info.Fields[idx].Type, data.Fields[idx].Value, bindings, paramSet)
		}
	} else {
		for _, field := range data.Fields {
			expected := expectedByName[field.Name]
			if expected == types.NoTypeID {
				continue
			}
			tc.bindStructFieldParam(expected, field.Value, bindings, paramSet)
		}
	}

	missing := make([]string, 0, len(paramNames))
	args := make([]types.TypeID, len(paramNames))
	for i, name := range paramNames {
		arg := bindings[name]
		args[i] = arg
		if arg == types.NoTypeID {
			missing = append(missing, name)
		}
	}

	var resultType types.TypeID
	if len(missing) == 0 {
		resultType = tc.instantiateType(symID, args, span, "ctor")
	} else {
		tc.reportStructInferenceFailure(sym.Name, missing, span)
		resultType = structType
	}
	if resultType != types.NoTypeID {
		tc.validateStructLiteralFields(resultType, data, span)
	}
	if len(missing) == 0 {
		return resultType, true
	}
	return types.NoTypeID, true
}

func (tc *typeChecker) bindStructFieldParam(expected types.TypeID, expr ast.ExprID, bindings map[string]types.TypeID, paramSet map[string]struct{}) {
	if expected == types.NoTypeID || expr == ast.NoExprID || tc.types == nil {
		return
	}
	actual := tc.typeExpr(expr)
	if actual == types.NoTypeID {
		return
	}
	key := tc.typeKeyForType(expected)
	if key == "" {
		return
	}
	tc.instantiateTypeKeyWithInference(key, actual, bindings, paramSet)
}

func (tc *typeChecker) reportStructInferenceFailure(typeName source.StringID, missing []string, span source.Span) {
	if tc.reporter == nil || len(missing) == 0 {
		return
	}
	displayName := tc.lookupName(typeName)
	if displayName == "" {
		displayName = "_"
	}
	missingLabel := strings.Join(missing, ", ")
	msg := fmt.Sprintf("cannot infer type parameter %s for %s; specify %s::<%s> or provide an explicit type annotation", missingLabel, displayName, displayName, missingLabel)
	if b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, span, msg); b != nil {
		b.Emit()
	}
}

func (tc *typeChecker) validateStructLiteralFields(structType types.TypeID, data *ast.ExprStructData, span source.Span) {
	info, normalized := tc.structInfoForType(structType)
	if info == nil {
		tc.report(diag.SemaTypeMismatch, span, "%s is not a struct", tc.typeLabel(structType))
		return
	}
	externFields := tc.externFieldsForType(normalized)
	if data.Positional {
		if len(externFields) > 0 {
			tc.report(diag.SemaTypeMismatch, span, "%s has extern fields; positional literals are not allowed", tc.typeLabel(normalized))
			return
		}
		tc.validatePositionalStructLiteral(normalized, info, data, span)
		return
	}
	fieldMap := make(map[source.StringID]types.StructField, len(info.Fields))
	for _, f := range info.Fields {
		fieldMap[f.Name] = f
	}
	for _, f := range externFields {
		if _, exists := fieldMap[f.Name]; !exists {
			fieldMap[f.Name] = f
		}
	}
	seen := make(map[source.StringID]struct{}, len(fieldMap))
	for _, field := range data.Fields {
		spec, ok := fieldMap[field.Name]
		if !ok {
			tc.report(diag.SemaUnresolvedSymbol, span, "%s has no field %s", tc.typeLabel(normalized), tc.lookupName(field.Name))
			continue
		}
		tc.ensureStructFieldType(field.Name, field.Value, spec.Type)
		if _, dup := seen[field.Name]; dup {
			tc.report(diag.SemaTypeMismatch, span, "field %s specified multiple times", tc.lookupName(field.Name))
		} else {
			seen[field.Name] = struct{}{}
		}
	}
	for name := range fieldMap {
		if _, ok := seen[name]; ok {
			continue
		}
		tc.report(diag.SemaTypeMismatch, span, "%s is missing required field %s", tc.typeLabel(normalized), tc.lookupName(name))
	}
}

func (tc *typeChecker) validatePositionalStructLiteral(structType types.TypeID, info *types.StructInfo, data *ast.ExprStructData, span source.Span) {
	if info == nil {
		return
	}
	if len(data.Fields) != len(info.Fields) {
		tc.report(diag.SemaTypeMismatch, span, "%s literal expects %d fields, got %d", tc.typeLabel(structType), len(info.Fields), len(data.Fields))
	}
	limit := len(data.Fields)
	if len(info.Fields) < limit {
		limit = len(info.Fields)
	}
	for i := range limit {
		data.Fields[i].Name = info.Fields[i].Name
		tc.ensureStructFieldType(info.Fields[i].Name, data.Fields[i].Value, info.Fields[i].Type)
	}
}

func (tc *typeChecker) ensureStructFieldType(name source.StringID, value ast.ExprID, expected types.TypeID) {
	if expected == types.NoTypeID || !value.IsValid() {
		return
	}
	actual := tc.typeExpr(value)
	if actual == types.NoTypeID {
		if tc.applyExpectedType(value, expected) {
			actual = tc.result.ExprTypes[value]
		} else {
			return
		}
	}
	if tc.valueType(actual) == tc.valueType(expected) {
		return
	}
	// Try implicit conversion before reporting error
	if convType, found, ambiguous := tc.tryImplicitConversion(actual, expected); found {
		tc.recordImplicitConversion(value, actual, convType)
		return
	} else if ambiguous {
		fieldName := tc.lookupName(name)
		tc.report(diag.SemaAmbiguousConversion, tc.exprSpan(value),
			"field %s: ambiguous conversion from %s to %s: multiple __to methods found",
			fieldName, tc.typeLabel(actual), tc.typeLabel(expected))
		return
	}
	fieldName := tc.lookupName(name)
	tc.report(diag.SemaTypeMismatch, tc.exprSpan(value), "field %s expects %s, got %s", fieldName, tc.typeLabel(expected), tc.typeLabel(actual))
}

func (tc *typeChecker) structInfoForType(id types.TypeID) (*types.StructInfo, types.TypeID) {
	if id == types.NoTypeID || tc.types == nil {
		return nil, types.NoTypeID
	}
	val := tc.valueType(id)
	if val == types.NoTypeID {
		return nil, types.NoTypeID
	}
	info, ok := tc.types.StructInfo(val)
	if !ok {
		return nil, val
	}
	return info, val
}

func (tc *typeChecker) valueType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	for {
		id = tc.resolveAlias(id)
		tt, ok := tc.types.Lookup(id)
		if !ok {
			return types.NoTypeID
		}
		switch tt.Kind {
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
}

func (tc *typeChecker) resolveAlias(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}
	const maxDepth = 32
	for range maxDepth {
		tt, ok := tc.types.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := tc.types.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
	}
	return id
}
