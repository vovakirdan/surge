package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) populateStructType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	structDecl := tc.builder.Items.TypeStruct(typeItem)
	if structDecl == nil {
		return
	}
	symID := tc.typeSymbolForItem(itemID)
	pushed := tc.pushTypeParams(symID, typeItem.Generics, nil)
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
	fields := make([]types.StructField, 0, structDecl.FieldsCount)
	scope := tc.fileScope()
	if paramIDs := tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount); len(paramIDs) > 0 {
		bounds := tc.resolveTypeParamBounds(paramIDs, scope, nil)
		tc.attachTypeParamSymbols(symID, bounds)
		tc.applyTypeParamBounds(symID)
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
			fieldType := tc.resolveTypeExprWithScope(field.Type, scope)
			attrs := tc.attrNames(field.AttrStart, field.AttrCount)
			fields = append(fields, types.StructField{
				Name:  field.Name,
				Type:  fieldType,
				Attrs: attrs,
			})
		}
	}
	tc.types.SetStructFields(typeID, fields)
}

func (tc *typeChecker) instantiateStruct(typeItem *ast.TypeItem, symID symbols.SymbolID, args []types.TypeID) types.TypeID {
	structDecl := tc.builder.Items.TypeStruct(typeItem)
	if structDecl == nil {
		return types.NoTypeID
	}
	pushed := tc.pushTypeParams(symID, typeItem.Generics, args)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	fields := make([]types.StructField, 0, structDecl.FieldsCount)
	scope := tc.fileScope()
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
			fieldType := tc.resolveTypeExprWithScope(field.Type, scope)
			attrs := tc.attrNames(field.AttrStart, field.AttrCount)
			fields = append(fields, types.StructField{
				Name:  field.Name,
				Type:  fieldType,
				Attrs: attrs,
			})
		}
	}
	typeID := tc.types.RegisterStructInstance(typeItem.Name, typeItem.Span, args)
	tc.types.SetStructFields(typeID, fields)
	return typeID
}
