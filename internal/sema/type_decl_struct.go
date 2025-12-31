package sema

import (
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) populateStructType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	structDecl := tc.builder.Items.TypeStruct(typeItem)
	if structDecl == nil {
		return
	}
	symID := tc.typeSymbolForItem(itemID)
	scope := tc.fileScope()
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount), scope)
	if len(paramSpecs) == 0 && len(typeItem.Generics) > 0 {
		paramSpecs = specsFromNames(typeItem.Generics)
	}
	pushed := tc.pushTypeParams(symID, paramSpecs, nil)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	if len(typeItem.Generics) > 0 {
		paramIDs := make([]types.TypeID, 0, len(typeItem.Generics))
		for _, name := range typeItem.Generics {
			paramIDs = append(paramIDs, tc.lookupTypeParam(name))
		}
		tc.types.SetStructTypeParams(typeID, paramIDs)
	}
	allowRawPointer := tc.hasIntrinsicAttr(typeItem.AttrStart, typeItem.AttrCount)
	fields := make([]types.StructField, 0, structDecl.FieldsCount)
	if paramIDs := tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount); len(paramIDs) > 0 {
		bounds := tc.resolveTypeParamBounds(paramIDs, scope, nil)
		tc.attachTypeParamSymbols(symID, bounds)
		tc.applyTypeParamBounds(symID)
	} else if len(paramSpecs) > 0 && len(typeItem.Generics) > 0 {
		// Attach type param symbols for generics syntax (<T>)
		typeParamSyms := make([]symbols.TypeParamSymbol, 0, len(paramSpecs))
		for _, spec := range paramSpecs {
			typeParamSyms = append(typeParamSyms, symbols.TypeParamSymbol{
				Name:      spec.name,
				IsConst:   spec.kind == paramKindConst,
				ConstType: spec.constType,
			})
		}
		tc.attachTypeParamSymbols(symID, typeParamSyms)
	}
	// Check if base type is @sealed before extending
	if base := tc.resolveStructBase(structDecl.Base, scope); base != types.NoTypeID {
		// Validate base is not sealed
		if tc.typeHasAttr(base, "sealed") {
			baseName := tc.typeLabel(base)
			tc.report(diag.SemaAttrSealedExtend, tc.typeSpan(structDecl.Base),
				"cannot extend @sealed type '%s'", baseName)
		}
		tc.structBases[typeID] = base
		tc.types.SetStructBase(typeID, base)
		fields = append(fields, tc.inheritedFields(base)...)
	}
	nameSet := make(map[source.StringID]struct{}, len(fields)+int(structDecl.FieldsCount))
	for _, f := range fields {
		nameSet[f.Name] = struct{}{}
	}
	for _, f := range tc.resolveOwnStructFields(structDecl, scope, allowRawPointer) {
		if _, exists := nameSet[f.Name]; exists {
			tc.report(diag.SemaTypeMismatch, structDecl.BodySpan, "field %s conflicts with inherited field", tc.lookupName(f.Name))
			continue
		}
		fields = append(fields, f)
		nameSet[f.Name] = struct{}{}
	}
	tc.types.SetStructFields(typeID, fields)

	// Validate field-level attributes
	if structDecl.FieldsCount > 0 && structDecl.FieldsStart.IsValid() {
		start := uint32(structDecl.FieldsStart)
		count := int(structDecl.FieldsCount)
		for offset := range count {
			uoff, err := safecast.Conv[uint32](offset)
			if err != nil {
				panic(fmt.Errorf("struct field offset overflow: %w", err))
			}
			fieldID := ast.TypeFieldID(start + uoff)
			field := tc.builder.Items.StructField(fieldID)
			if field == nil {
				continue
			}
			// Validate attributes for this field
			tc.validateFieldAttrs(field, typeID, offset)
		}
	}

	// Validate type-level attributes
	tc.validateTypeAttrs(typeItem, typeID)
}

func (tc *typeChecker) instantiateStruct(typeItem *ast.TypeItem, symID symbols.SymbolID, args []types.TypeID) types.TypeID {
	structDecl := tc.builder.Items.TypeStruct(typeItem)
	if structDecl == nil {
		return types.NoTypeID
	}
	scope := tc.fileScope()
	allowRawPointer := tc.hasIntrinsicAttr(typeItem.AttrStart, typeItem.AttrCount)
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount), scope)
	if len(paramSpecs) == 0 && len(typeItem.Generics) > 0 {
		paramSpecs = specsFromNames(typeItem.Generics)
	}
	pushed := tc.pushTypeParams(symID, paramSpecs, args)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	fields := make([]types.StructField, 0, structDecl.FieldsCount)
	base := tc.resolveStructBase(structDecl.Base, scope)
	if base != types.NoTypeID {
		for _, f := range tc.inheritedFields(base) {
			fields = append(fields, tc.instantiateField(f, symID, args))
		}
	}
	if structDecl.FieldsCount > 0 {
		start := uint32(structDecl.FieldsStart)
		count := int(structDecl.FieldsCount)
		for offset := range count {
			uoff, err := safecast.Conv[uint32](offset)
			if err != nil {
				panic(fmt.Errorf("struct field offset overflow: %w", err))
			}
			fieldID := ast.TypeFieldID(start + uoff)
			field := tc.builder.Items.StructField(fieldID)
			if field == nil {
				continue
			}
			fieldType := tc.resolveTypeExprWithScopeAllowPointer(field.Type, scope, allowRawPointer)
			infos := tc.collectAttrs(field.AttrStart, field.AttrCount)
			attrs := tc.attrNames(field.AttrStart, field.AttrCount)
			fields = append(fields, types.StructField{
				Name:   field.Name,
				Type:   fieldType,
				Attrs:  attrs,
				Layout: tc.fieldLayoutAttrsFromInfos(infos),
			})
		}
	}
	typeID := tc.types.RegisterStructInstance(typeItem.Name, typeItem.Span, args)
	if base != types.NoTypeID {
		tc.structBases[typeID] = base
		tc.types.SetStructBase(typeID, base)
	}
	if externFields := tc.externFieldsForType(typeID); len(externFields) > 0 {
		nameSet := make(map[source.StringID]struct{}, len(fields)+len(externFields))
		for _, f := range fields {
			nameSet[f.Name] = struct{}{}
		}
		for _, f := range externFields {
			if _, exists := nameSet[f.Name]; exists {
				continue
			}
			fields = append(fields, f)
			nameSet[f.Name] = struct{}{}
		}
	}
	tc.types.SetStructFields(typeID, fields)
	if sym := tc.symbolFromID(symID); sym != nil && sym.Type != types.NoTypeID {
		if attrs, ok := tc.typeAttrs[sym.Type]; ok {
			tc.recordTypeAttrs(typeID, attrs)
		}
		if attrs, ok := tc.types.TypeLayoutAttrs(sym.Type); ok {
			tc.types.SetTypeLayoutAttrs(typeID, attrs)
		}
		if tc.types.IsCopy(sym.Type) {
			tc.types.MarkCopyType(typeID)
		}
	}
	return typeID
}

func (tc *typeChecker) resolveStructBase(base ast.TypeID, scope symbols.ScopeID) types.TypeID {
	if !base.IsValid() {
		return types.NoTypeID
	}
	baseType := tc.resolveTypeExprWithScope(base, scope)
	baseVal := tc.valueType(baseType)
	if baseVal == types.NoTypeID {
		return types.NoTypeID
	}
	if info, ok := tc.types.StructInfo(baseVal); !ok || info == nil {
		tc.report(diag.SemaTypeMismatch, tc.typeSpan(base), "base type %s is not a struct", tc.typeLabel(baseVal))
		return types.NoTypeID
	}
	if tc.typeHasNoInherit(baseVal) {
		tc.report(diag.SemaTypeMismatch, tc.typeSpan(base), "type %s is marked @noinherit and cannot be extended", tc.typeLabel(baseVal))
		return types.NoTypeID
	}
	return baseVal
}

func (tc *typeChecker) resolveOwnStructFields(structDecl *ast.TypeStructDecl, scope symbols.ScopeID, allowRawPointer bool) []types.StructField {
	if structDecl == nil {
		return nil
	}
	fields := make([]types.StructField, 0, structDecl.FieldsCount)
	if structDecl.FieldsCount == 0 || !structDecl.FieldsStart.IsValid() {
		return fields
	}
	start := uint32(structDecl.FieldsStart)
	count := int(structDecl.FieldsCount)
	for offset := range count {
		uoff, err := safecast.Conv[uint32](offset)
		if err != nil {
			panic(fmt.Errorf("struct field offset overflow: %w", err))
		}
		fieldID := ast.TypeFieldID(start + uoff)
		field := tc.builder.Items.StructField(fieldID)
		if field == nil {
			continue
		}
		fieldType := tc.resolveTypeExprWithScopeAllowPointer(field.Type, scope, allowRawPointer)
		infos := tc.collectAttrs(field.AttrStart, field.AttrCount)
		fields = append(fields, types.StructField{
			Name:   field.Name,
			Type:   fieldType,
			Attrs:  tc.attrNames(field.AttrStart, field.AttrCount),
			Layout: tc.fieldLayoutAttrsFromInfos(infos),
		})
	}
	return fields
}

func (tc *typeChecker) inheritedFields(base types.TypeID) []types.StructField {
	if base == types.NoTypeID {
		return nil
	}
	fields := make([]types.StructField, 0)
	if info, ok := tc.types.StructInfo(base); ok && info != nil {
		for _, f := range info.Fields {
			if attrHasNoInherit(tc, f.Attrs) {
				continue
			}
			fields = append(fields, f)
		}
	}
	return fields
}

func (tc *typeChecker) instantiateField(f types.StructField, owner symbols.SymbolID, args []types.TypeID) types.StructField {
	if len(args) == 0 {
		return f
	}
	bindings := make(map[source.StringID]bindingInfo, len(args))
	if owner.IsValid() {
		if sym := tc.symbolFromID(owner); sym != nil && len(sym.TypeParams) == len(args) {
			for i, name := range sym.TypeParams {
				bindings[name] = bindingInfo{typ: args[i]}
			}
		}
	}
	typ := tc.substituteTypeParamByName(f.Type, bindings)
	return types.StructField{
		Name:   f.Name,
		Type:   typ,
		Attrs:  f.Attrs,
		Layout: f.Layout,
	}
}

func attrHasNoInherit(tc *typeChecker, attrs []source.StringID) bool {
	if tc == nil {
		return false
	}
	for _, id := range attrs {
		if strings.EqualFold(tc.lookupName(id), "noinherit") {
			return true
		}
	}
	return false
}

func (tc *typeChecker) typeHasNoInherit(typeID types.TypeID) bool {
	if typeID == types.NoTypeID || tc.typeIDItems == nil {
		return false
	}
	itemID := tc.typeIDItems[typeID]
	if !itemID.IsValid() {
		return false
	}
	typeItem, ok := tc.builder.Items.Type(itemID)
	if !ok || typeItem == nil {
		return false
	}
	return attrHasNoInherit(tc, tc.attrNames(typeItem.AttrStart, typeItem.AttrCount))
}
