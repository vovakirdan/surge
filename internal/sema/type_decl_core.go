package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

func (tc *typeChecker) registerTypeDecls(file *ast.File) {
	if tc.builder == nil || tc.types == nil || file == nil {
		return
	}
	if tc.typeItems == nil {
		tc.typeItems = make(map[ast.ItemID]types.TypeID)
	}
	for _, itemID := range file.Items {
		item := tc.builder.Items.Get(itemID)
		if item == nil || item.Kind != ast.ItemType {
			continue
		}
		typeItem, ok := tc.builder.Items.Type(itemID)
		if !ok || typeItem == nil {
			continue
		}
		if _, exists := tc.typeItems[itemID]; exists {
			continue
		}
		var typeID types.TypeID
		switch typeItem.Kind {
		case ast.TypeDeclStruct:
			typeID = tc.types.RegisterStruct(typeItem.Name, typeItem.Span)
		case ast.TypeDeclAlias:
			typeID = tc.types.RegisterAlias(typeItem.Name, typeItem.Span)
		case ast.TypeDeclUnion:
			typeID = tc.types.RegisterUnion(typeItem.Name, typeItem.Span)
		default:
			continue
		}
		tc.typeItems[itemID] = typeID
		if tc.typeKeys != nil {
			if name := tc.lookupName(typeItem.Name); name != "" {
				tc.typeKeys[name] = typeID
			}
		}
		if symID := tc.typeSymbolForItem(itemID); symID.IsValid() {
			tc.assignSymbolType(symID, typeID)
		}
	}
}

func (tc *typeChecker) populateTypeDecls(file *ast.File) {
	if tc.builder == nil || tc.types == nil || file == nil {
		return
	}
	for _, itemID := range file.Items {
		typeID := tc.typeItems[itemID]
		if typeID == types.NoTypeID {
			continue
		}
		item := tc.builder.Items.Get(itemID)
		if item == nil || item.Kind != ast.ItemType {
			continue
		}
		typeItem, ok := tc.builder.Items.Type(itemID)
		if !ok || typeItem == nil {
			continue
		}
		switch typeItem.Kind {
		case ast.TypeDeclStruct:
			tc.populateStructType(itemID, typeItem, typeID)
		case ast.TypeDeclAlias:
			tc.populateAliasType(itemID, typeItem, typeID)
		case ast.TypeDeclUnion:
			tc.populateUnionType(itemID, typeItem, typeID)
		}
	}
}
