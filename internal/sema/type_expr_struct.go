package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

func (tc *typeChecker) indexResultType(container types.TypeID, span source.Span) types.TypeID {
	if container == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	base := tc.valueType(container)
	if base == types.NoTypeID {
		return types.NoTypeID
	}
	tt, ok := tc.types.Lookup(base)
	if !ok {
		return types.NoTypeID
	}
	switch tt.Kind {
	case types.KindArray:
		return tt.Elem
	case types.KindString:
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
	tc.report(diag.SemaTypeMismatch, span, "%s has no fields", tc.typeLabel(base))
	return types.NoTypeID
}

func (tc *typeChecker) validateStructLiteralFields(structType types.TypeID, data *ast.ExprStructData, span source.Span) {
	info, normalized := tc.structInfoForType(structType)
	if info == nil {
		tc.report(diag.SemaTypeMismatch, span, "%s is not a struct", tc.typeLabel(structType))
		return
	}
	if data.Positional {
		tc.validatePositionalStructLiteral(normalized, info, data, span)
		return
	}
	fieldMap := make(map[source.StringID]types.StructField, len(info.Fields))
	for _, f := range info.Fields {
		fieldMap[f.Name] = f
	}
	seen := make(map[source.StringID]struct{}, len(info.Fields))
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
	if len(seen) != len(info.Fields) {
		tc.report(diag.SemaTypeMismatch, span, "%s literal is missing %d field(s)", tc.typeLabel(normalized), len(info.Fields)-len(seen))
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
		return
	}
	if tc.valueType(actual) == tc.valueType(expected) {
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
