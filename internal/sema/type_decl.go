package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
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
		default:
			continue
		}
		tc.typeItems[itemID] = typeID
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
			tc.populateAliasType(typeItem, typeID)
		}
	}
}

func (tc *typeChecker) populateStructType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	structDecl := tc.builder.Items.TypeStruct(typeItem)
	if structDecl == nil {
		return
	}
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
			fields = append(fields, types.StructField{
				Name: field.Name,
				Type: fieldType,
			})
		}
	}
	tc.types.SetStructFields(typeID, fields)
}

func (tc *typeChecker) populateAliasType(typeItem *ast.TypeItem, typeID types.TypeID) {
	aliasDecl := tc.builder.Items.TypeAlias(typeItem)
	if aliasDecl == nil {
		return
	}
	target := tc.resolveTypeExprWithScope(aliasDecl.Target, tc.fileScope())
	if target == types.NoTypeID {
		span := typeItem.Span
		name := tc.lookupName(typeItem.Name)
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaUnresolvedSymbol, span, "unable to resolve alias target for %s", name)
		return
	}
	tc.types.SetAliasTarget(typeID, target)
}

func (tc *typeChecker) typeSymbolForItem(itemID ast.ItemID) symbols.SymbolID {
	if tc.symbols == nil || tc.symbols.ItemSymbols == nil {
		return symbols.NoSymbolID
	}
	syms := tc.symbols.ItemSymbols[itemID]
	if len(syms) == 0 {
		return symbols.NoSymbolID
	}
	return syms[0]
}

func (tc *typeChecker) assignSymbolType(symID symbols.SymbolID, typeID types.TypeID) {
	if !symID.IsValid() || typeID == types.NoTypeID {
		return
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return
	}
	sym.Type = typeID
}

func (tc *typeChecker) resolveTypeExprWithScope(id ast.TypeID, scope symbols.ScopeID) types.TypeID {
	if !id.IsValid() || tc.builder == nil {
		return types.NoTypeID
	}
	scope = tc.scopeOrFile(scope)
	key := typeCacheKey{Type: id, Scope: scope}
	if tc.typeCache != nil {
		if cached, ok := tc.typeCache[key]; ok {
			return cached
		}
	}
	expr := tc.builder.Types.Get(id)
	if expr == nil {
		return types.NoTypeID
	}
	var result types.TypeID
	switch expr.Kind {
	case ast.TypeExprPath:
		path, _ := tc.builder.Types.Path(id)
		result = tc.resolveTypePath(path, expr.Span, scope)
	case ast.TypeExprUnary:
		if unary, ok := tc.builder.Types.UnaryType(id); ok && unary != nil {
			inner := tc.resolveTypeExprWithScope(unary.Inner, scope)
			if inner != types.NoTypeID {
				switch unary.Op {
				case ast.TypeUnaryOwn:
					result = tc.types.Intern(types.MakeOwn(inner))
				case ast.TypeUnaryRef:
					result = tc.types.Intern(types.MakeReference(inner, false))
				case ast.TypeUnaryRefMut:
					result = tc.types.Intern(types.MakeReference(inner, true))
				case ast.TypeUnaryPointer:
					result = tc.types.Intern(types.MakePointer(inner))
				}
			}
		}
	case ast.TypeExprArray:
		if arr, ok := tc.builder.Types.Array(id); ok && arr != nil {
			elem := tc.resolveTypeExprWithScope(arr.Elem, scope)
			if elem != types.NoTypeID {
				count := types.ArrayDynamicLength
				if arr.Kind == ast.ArraySized {
					if !arr.HasConstLen {
						tc.report(diag.SemaTypeMismatch, expr.Span, "array length must be a constant")
						break
					}
					if arr.ConstLength > uint64(^uint32(0)) {
						tc.report(diag.SemaTypeMismatch, expr.Span, "array length %d exceeds limit", arr.ConstLength)
						break
					}
					count = uint32(arr.ConstLength)
				}
				result = tc.types.Intern(types.MakeArray(elem, count))
			}
		}
	default:
		// other type forms (tuple/fn) are not supported yet
	}
	if tc.typeCache != nil {
		tc.typeCache[key] = result
	}
	return result
}

func (tc *typeChecker) resolveTypePath(path *ast.TypePath, span source.Span, scope symbols.ScopeID) types.TypeID {
	if path == nil || len(path.Segments) == 0 {
		return types.NoTypeID
	}
	if len(path.Segments) > 1 {
		tc.report(diag.SemaUnresolvedSymbol, span, "qualified type paths are not supported yet")
		return types.NoTypeID
	}
	seg := path.Segments[0]
	if len(seg.Generics) > 0 {
		tc.report(diag.SemaUnresolvedSymbol, span, "generic type arguments are not supported yet")
		return types.NoTypeID
	}
	return tc.resolveNamedType(seg.Name, span, scope)
}

func (tc *typeChecker) resolveNamedType(name source.StringID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if name == source.NoStringID {
		return types.NoTypeID
	}
	literal := tc.lookupName(name)
	if literal != "" {
		if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
			return builtin
		}
	}
	symID := tc.lookupTypeSymbol(name, scope)
	if !symID.IsValid() {
		if literal == "" {
			literal = "_"
		}
		tc.report(diag.SemaUnresolvedSymbol, span, "unknown type %s", literal)
		return types.NoTypeID
	}
	return tc.symbolType(symID)
}

func (tc *typeChecker) lookupTypeSymbol(name source.StringID, scope symbols.ScopeID) symbols.SymbolID {
	if name == source.NoStringID || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil || tc.symbols.Table.Symbols == nil {
		return symbols.NoSymbolID
	}
	for scope = tc.scopeOrFile(scope); scope.IsValid(); {
		scopeData := tc.symbols.Table.Scopes.Get(scope)
		if scopeData == nil {
			break
		}
		if ids := scopeData.NameIndex[name]; len(ids) > 0 {
			for i := len(ids) - 1; i >= 0; i-- {
				id := ids[i]
				sym := tc.symbols.Table.Symbols.Get(id)
				if sym == nil {
					continue
				}
				if sym.Kind == symbols.SymbolType {
					return id
				}
			}
		}
		scope = scopeData.Parent
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) builtinTypeByName(name string) types.TypeID {
	switch name {
	case "int":
		return tc.types.Builtins().Int
	case "uint":
		return tc.types.Builtins().Uint
	case "float":
		return tc.types.Builtins().Float
	case "bool":
		return tc.types.Builtins().Bool
	case "string":
		return tc.types.Builtins().String
	case "nothing":
		return tc.types.Builtins().Nothing
	case "unit":
		return tc.types.Builtins().Unit
	default:
		return types.NoTypeID
	}
}

func (tc *typeChecker) scopeOrFile(scope symbols.ScopeID) symbols.ScopeID {
	if scope.IsValid() {
		return scope
	}
	return tc.fileScope()
}

func (tc *typeChecker) symbolType(symID symbols.SymbolID) types.TypeID {
	if !symID.IsValid() {
		return types.NoTypeID
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	return sym.Type
}
