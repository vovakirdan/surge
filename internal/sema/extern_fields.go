package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type externFieldInfo struct {
	name  source.StringID
	typ   types.TypeID
	attrs []source.StringID
	span  source.Span
}

type externFieldSet struct {
	paramNames []source.StringID
	fields     map[source.StringID]externFieldInfo
	order      []externFieldInfo
}

func (tc *typeChecker) collectExternFields(file *ast.File) {
	if tc.builder == nil || tc.types == nil || file == nil {
		return
	}
	for _, itemID := range file.Items {
		block, ok := tc.builder.Items.Extern(itemID)
		if !ok || block == nil {
			continue
		}
		tc.processExternBlock(itemID, block)
	}
}

func (tc *typeChecker) processExternBlock(itemID ast.ItemID, block *ast.ExternBlock) {
	if block == nil || block.MembersCount == 0 || !block.MembersStart.IsValid() {
		return
	}

	paramNames := tc.externTypeParams(block.Target)
	pushed := tc.pushTypeParams(symbols.NoSymbolID, specsFromNames(paramNames), nil)
	if pushed {
		defer tc.popTypeParams()
	}

	scope := tc.scopeForItem(itemID)
	targetType := tc.resolveTypeExprWithScope(block.Target, scope)
	normalized := tc.valueType(targetType)
	key := tc.typeKeyForType(normalized)
	if key == "" {
		return
	}

	structFieldSpans := make(map[source.StringID]source.Span)
	if info, ok := tc.types.StructInfo(normalized); ok && info != nil {
		for _, f := range info.Fields {
			structFieldSpans[f.Name] = info.Decl
		}
	}

	set := tc.externFields[key]
	if set == nil {
		set = &externFieldSet{
			paramNames: slices.Clone(paramNames),
			fields:     make(map[source.StringID]externFieldInfo),
		}
		tc.externFields[key] = set
	} else if len(set.paramNames) == 0 && len(paramNames) > 0 {
		set.paramNames = slices.Clone(paramNames)
	}

	start := uint32(block.MembersStart)
	seen := make(map[source.StringID]source.Span)
	for idx := range block.MembersCount {
		member := tc.builder.Items.ExternMember(ast.ExternMemberID(start + uint32(idx)))
		if member == nil || member.Kind != ast.ExternMemberField {
			continue
		}
		field := tc.builder.Items.ExternField(member.Field)
		if field == nil {
			continue
		}

		name := field.Name
		if prev, exists := structFieldSpans[name]; exists {
			tc.reportExternDuplicate(field.NameSpan, prev, tc.lookupName(name))
			continue
		}
		if prev, exists := seen[name]; exists {
			tc.reportExternDuplicate(field.NameSpan, prev, tc.lookupName(name))
			continue
		}
		if prev, exists := set.fields[name]; exists {
			tc.reportExternDuplicate(field.NameSpan, prev.span, tc.lookupName(name))
			continue
		}
		seen[name] = field.NameSpan

		tc.validateAttrs(field.AttrStart, field.AttrCount, ast.AttrTargetField, diag.SemaExternUnknownAttr)
		fieldType := tc.resolveTypeExprWithScope(field.Type, scope)
		info := externFieldInfo{
			name:  name,
			typ:   fieldType,
			attrs: tc.attrNames(field.AttrStart, field.AttrCount),
			span:  field.Span,
		}

		if _, exists := set.fields[name]; !exists {
			set.fields[name] = info
			set.order = append(set.order, info)
		}
	}
}

func (tc *typeChecker) reportExternDuplicate(primary, prev source.Span, name string) {
	if tc.reporter == nil {
		return
	}
	msg := fmt.Sprintf("duplicate extern field '%s'", name)
	b := diag.ReportError(tc.reporter, diag.SemaExternDuplicateField, primary, msg)
	if b == nil {
		return
	}
	if prev != (source.Span{}) {
		b.WithNote(prev, fmt.Sprintf("previous declaration of '%s' is here", name))
	}
	b.Emit()
}

func (tc *typeChecker) externFieldsForType(id types.TypeID) []types.StructField {
	if id == types.NoTypeID || tc.externFields == nil {
		return nil
	}
	candidates := tc.typeKeyCandidates(id)
	for _, cand := range candidates {
		set := tc.externFields[cand.key]
		if set == nil {
			continue
		}
		return tc.instantiateExternFields(set, cand.base)
	}
	return nil
}

func (tc *typeChecker) instantiateExternFields(set *externFieldSet, target types.TypeID) []types.StructField {
	if set == nil {
		return nil
	}
	args := tc.typeArgsForType(target)
	bindings := make(map[source.StringID]bindingInfo, len(set.paramNames))
	if len(set.paramNames) > 0 && len(args) > 0 {
		for idx, name := range set.paramNames {
			if idx >= len(args) {
				break
			}
			bindings[name] = bindingInfo{typ: args[idx]}
		}
	}
	result := make([]types.StructField, 0, len(set.order))
	for _, info := range set.order {
		fieldType := info.typ
		if len(bindings) > 0 {
			fieldType = tc.substituteTypeParamByName(info.typ, bindings)
		}
		result = append(result, types.StructField{
			Name:  info.name,
			Type:  fieldType,
			Attrs: slices.Clone(info.attrs),
		})
	}
	return result
}

func (tc *typeChecker) typeArgsForType(id types.TypeID) []types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return nil
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return nil
	}
	switch tt.Kind {
	case types.KindStruct:
		return tc.types.StructArgs(id)
	case types.KindAlias:
		return tc.types.AliasArgs(id)
	case types.KindUnion:
		return tc.types.UnionArgs(id)
	default:
		return nil
	}
}

func (tc *typeChecker) externTypeParams(target ast.TypeID) []source.StringID {
	if tc.builder == nil || !target.IsValid() {
		return nil
	}
	params := make([]source.StringID, 0, 2)
	seen := make(map[source.StringID]struct{})
	var visit func(ast.TypeID)
	visit = func(id ast.TypeID) {
		if !id.IsValid() {
			return
		}
		expr := tc.builder.Types.Get(id)
		if expr == nil {
			return
		}
		switch expr.Kind {
		case ast.TypeExprPath:
			path, _ := tc.builder.Types.Path(id)
			if path == nil {
				return
			}
			for _, seg := range path.Segments {
				for _, gid := range seg.Generics {
					if p, ok := tc.builder.Types.Path(gid); ok && p != nil && len(p.Segments) == 1 && len(p.Segments[0].Generics) == 0 {
						name := p.Segments[0].Name
						if name == source.NoStringID || tc.isKnownTypeName(name) {
							continue
						}
						if _, exists := seen[name]; !exists {
							seen[name] = struct{}{}
							params = append(params, name)
						}
						continue
					}
					visit(gid)
				}
			}
		case ast.TypeExprUnary:
			if unary, ok := tc.builder.Types.UnaryType(id); ok && unary != nil {
				visit(unary.Inner)
			}
		case ast.TypeExprArray:
			if arr, ok := tc.builder.Types.Array(id); ok && arr != nil {
				visit(arr.Elem)
			}
		case ast.TypeExprOptional:
			if opt, ok := tc.builder.Types.Optional(id); ok && opt != nil {
				visit(opt.Inner)
			}
		case ast.TypeExprErrorable:
			if errable, ok := tc.builder.Types.Errorable(id); ok && errable != nil {
				visit(errable.Inner)
				if errable.Error.IsValid() {
					visit(errable.Error)
				}
			}
		}
	}
	visit(target)
	return params
}

func (tc *typeChecker) isKnownTypeName(id source.StringID) bool {
	name := tc.lookupName(id)
	if name == "" {
		return false
	}
	switch name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float", "float16", "float32", "float64",
		"bool", "string", "nothing", "unit":
		return true
	}
	if tc.typeKeys != nil {
		if ty := tc.typeKeys[name]; ty != types.NoTypeID {
			return true
		}
	}
	return false
}
