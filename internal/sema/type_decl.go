package sema

import (
	"fmt"
	"strconv"
	"strings"

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

func (tc *typeChecker) populateAliasType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	aliasDecl := tc.builder.Items.TypeAlias(typeItem)
	if aliasDecl == nil {
		return
	}
	symID := tc.typeSymbolForItem(itemID)
	pushed := tc.pushTypeParams(symID, typeItem.Generics, nil)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
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
	key := typeCacheKey{Type: id, Scope: scope, Env: tc.currentTypeParamEnv()}
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
	case ast.TypeExprOptional:
		if opt, ok := tc.builder.Types.Optional(id); ok && opt != nil {
			inner := tc.resolveTypeExprWithScope(opt.Inner, scope)
			result = tc.resolveOptionType(inner, expr.Span, scope)
		}
	case ast.TypeExprErrorable:
		if errable, ok := tc.builder.Types.Errorable(id); ok && errable != nil {
			inner := tc.resolveTypeExprWithScope(errable.Inner, scope)
			var errType types.TypeID
			if errable.Error.IsValid() {
				errType = tc.resolveTypeExprWithScope(errable.Error, scope)
			} else {
				errType = tc.resolveErrorType(expr.Span, scope)
			}
			result = tc.resolveResultType(inner, errType, expr.Span, scope)
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
	if len(seg.Generics) == 0 {
		if param := tc.lookupTypeParam(seg.Name); param != types.NoTypeID {
			return param
		}
	}
	args := tc.resolveTypeArgs(seg.Generics, scope)
	return tc.resolveNamedType(seg.Name, args, span, scope)
}

func (tc *typeChecker) resolveNamedType(name source.StringID, args []types.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if name == source.NoStringID {
		return types.NoTypeID
	}
	literal := tc.lookupName(name)
	if literal != "" {
		if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
			return builtin
		}
	}
	if literal == "Option" || literal == "Result" {
		if ty := tc.resolveBuiltinGeneric(literal, args, span); ty != types.NoTypeID {
			return ty
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
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	expected := len(sym.TypeParams)
	if expected == 0 {
		if len(args) > 0 {
			tc.report(diag.SemaTypeMismatch, span, "%s does not take type arguments", tc.lookupName(sym.Name))
			return types.NoTypeID
		}
		return tc.symbolType(symID)
	}
	if len(args) == 0 {
		tc.report(diag.SemaTypeMismatch, span, "%s requires %d type argument(s)", tc.lookupName(sym.Name), expected)
		return types.NoTypeID
	}
	if len(args) != expected {
		tc.report(diag.SemaTypeMismatch, span, "%s expects %d type argument(s), got %d", tc.lookupName(sym.Name), expected, len(args))
		return types.NoTypeID
	}
	return tc.instantiateType(symID, args, span)
}

func (tc *typeChecker) resolveTypeArgs(typeIDs []ast.TypeID, scope symbols.ScopeID) []types.TypeID {
	if len(typeIDs) == 0 {
		return nil
	}
	args := make([]types.TypeID, 0, len(typeIDs))
	for _, tid := range typeIDs {
		arg := tc.resolveTypeExprWithScope(tid, scope)
		args = append(args, arg)
	}
	return args
}

func (tc *typeChecker) resolveBuiltinGeneric(name string, args []types.TypeID, span source.Span) types.TypeID {
	switch name {
	case "Option":
		if len(args) == 0 {
			tc.report(diag.SemaTypeMismatch, span, "Option requires 1 type argument")
			return types.NoTypeID
		}
		if len(args) != 1 {
			tc.report(diag.SemaTypeMismatch, span, "Option expects 1 type argument, got %d", len(args))
			return types.NoTypeID
		}
		return tc.makeOptionType(args[0])
	case "Result":
		if len(args) == 0 {
			tc.report(diag.SemaTypeMismatch, span, "Result requires 2 type arguments")
			return types.NoTypeID
		}
		if len(args) != 2 {
			tc.report(diag.SemaTypeMismatch, span, "Result expects 2 type arguments, got %d", len(args))
			return types.NoTypeID
		}
		return tc.makeResultType(args[0], args[1])
	default:
		return types.NoTypeID
	}
}

func (tc *typeChecker) makeOptionType(elem types.TypeID) types.TypeID {
	if tc.types == nil || elem == types.NoTypeID {
		return types.NoTypeID
	}
	key := tc.builtinInstantiationKey("Option", elem)
	if cached := tc.cachedInstantiation(key); cached != types.NoTypeID {
		return cached
	}
	some := tc.builder.StringsInterner.Intern("Some")
	members := []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: some, TagArgs: []types.TypeID{elem}},
		{Kind: types.UnionMemberNothing, Type: tc.types.Builtins().Nothing},
	}
	typeID := tc.types.RegisterUnionInstance(tc.builder.StringsInterner.Intern("Option"), source.Span{}, []types.TypeID{elem})
	tc.types.SetUnionMembers(typeID, members)
	tc.rememberInstantiation(key, typeID)
	return typeID
}

func (tc *typeChecker) makeResultType(okType, errType types.TypeID) types.TypeID {
	if tc.types == nil || okType == types.NoTypeID || errType == types.NoTypeID {
		return types.NoTypeID
	}
	key := tc.builtinInstantiationKey("Result", okType, errType)
	if cached := tc.cachedInstantiation(key); cached != types.NoTypeID {
		return cached
	}
	okName := tc.builder.StringsInterner.Intern("Ok")
	errName := tc.builder.StringsInterner.Intern("Error")
	members := []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: okName, TagArgs: []types.TypeID{okType}},
		{Kind: types.UnionMemberTag, TagName: errName, TagArgs: []types.TypeID{errType}},
	}
	typeID := tc.types.RegisterUnionInstance(tc.builder.StringsInterner.Intern("Result"), source.Span{}, []types.TypeID{okType, errType})
	tc.types.SetUnionMembers(typeID, members)
	tc.rememberInstantiation(key, typeID)
	return typeID
}

func (tc *typeChecker) instantiationKey(symID symbols.SymbolID, args []types.TypeID) string {
	if !symID.IsValid() {
		return ""
	}
	var b strings.Builder
	b.WriteString(strconv.FormatUint(uint64(symID), 10))
	for _, arg := range args {
		b.WriteByte('#')
		b.WriteString(strconv.FormatUint(uint64(arg), 10))
	}
	return b.String()
}

func (tc *typeChecker) builtinInstantiationKey(name string, args ...types.TypeID) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("builtin:")
	b.WriteString(name)
	for _, arg := range args {
		b.WriteByte('#')
		b.WriteString(strconv.FormatUint(uint64(arg), 10))
	}
	return b.String()
}

func (tc *typeChecker) cachedInstantiation(key string) types.TypeID {
	if key == "" || tc.typeInstantiations == nil {
		return types.NoTypeID
	}
	if cached, ok := tc.typeInstantiations[key]; ok {
		return cached
	}
	return types.NoTypeID
}

func (tc *typeChecker) rememberInstantiation(key string, typeID types.TypeID) {
	if key == "" || typeID == types.NoTypeID || tc.typeInstantiations == nil {
		return
	}
	tc.typeInstantiations[key] = typeID
}

func (tc *typeChecker) instantiateType(symID symbols.SymbolID, args []types.TypeID, span source.Span) types.TypeID {
	key := tc.instantiationKey(symID, args)
	if cached := tc.cachedInstantiation(key); cached != types.NoTypeID {
		return cached
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	item := tc.builder.Items.Get(sym.Decl.Item)
	if item == nil || item.Kind != ast.ItemType {
		return types.NoTypeID
	}
	typeItem, ok := tc.builder.Items.Type(sym.Decl.Item)
	if !ok || typeItem == nil {
		return types.NoTypeID
	}

	var instantiated types.TypeID
	switch typeItem.Kind {
	case ast.TypeDeclStruct:
		instantiated = tc.instantiateStruct(typeItem, symID, args)
	case ast.TypeDeclAlias:
		instantiated = tc.instantiateAlias(typeItem, symID, args)
	case ast.TypeDeclUnion:
		instantiated = tc.instantiateUnion(typeItem, symID, args)
	default:
		instantiated = types.NoTypeID
	}
	tc.rememberInstantiation(key, instantiated)
	return instantiated
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
			fields = append(fields, types.StructField{
				Name: field.Name,
				Type: fieldType,
			})
		}
	}
	typeID := tc.types.RegisterStructInstance(typeItem.Name, typeItem.Span, args)
	tc.types.SetStructFields(typeID, fields)
	return typeID
}

func (tc *typeChecker) instantiateAlias(typeItem *ast.TypeItem, symID symbols.SymbolID, args []types.TypeID) types.TypeID {
	aliasDecl := tc.builder.Items.TypeAlias(typeItem)
	if aliasDecl == nil {
		return types.NoTypeID
	}
	pushed := tc.pushTypeParams(symID, typeItem.Generics, args)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	target := tc.resolveTypeExprWithScope(aliasDecl.Target, tc.fileScope())
	if target == types.NoTypeID {
		span := typeItem.Span
		name := tc.lookupName(typeItem.Name)
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaUnresolvedSymbol, span, "unable to resolve alias target for %s", name)
		return types.NoTypeID
	}
	typeID := tc.types.RegisterAliasInstance(typeItem.Name, typeItem.Span, args)
	tc.types.SetAliasTarget(typeID, target)
	return typeID
}

func (tc *typeChecker) instantiateUnion(typeItem *ast.TypeItem, symID symbols.SymbolID, args []types.TypeID) types.TypeID {
	unionDecl := tc.builder.Items.TypeUnion(typeItem)
	if unionDecl == nil {
		return types.NoTypeID
	}
	pushed := tc.pushTypeParams(symID, typeItem.Generics, args)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	scope := tc.fileScope()
	members := make([]types.UnionMember, 0, unionDecl.MembersCount)
	if unionDecl.MembersCount > 0 {
		start := uint32(unionDecl.MembersStart)
		count := int(unionDecl.MembersCount)
		for offset := range count {
			uoff, err := safecast.Conv[uint32](offset)
			if err != nil {
				panic(fmt.Errorf("union member offset overflow: %w", err))
			}
			memberID := ast.TypeUnionMemberID(start + uoff)
			member := tc.builder.Items.UnionMember(memberID)
			if member == nil {
				continue
			}
			switch member.Kind {
			case ast.TypeUnionMemberType:
				typ := tc.resolveTypeExprWithScope(member.Type, scope)
				members = append(members, types.UnionMember{
					Kind: types.UnionMemberType,
					Type: typ,
				})
			case ast.TypeUnionMemberNothing:
				members = append(members, types.UnionMember{
					Kind: types.UnionMemberNothing,
					Type: tc.types.Builtins().Nothing,
				})
			case ast.TypeUnionMemberTag:
				tagArgs := make([]types.TypeID, 0, len(member.TagArgs))
				for _, arg := range member.TagArgs {
					tagArgs = append(tagArgs, tc.resolveTypeExprWithScope(arg, scope))
				}
				members = append(members, types.UnionMember{
					Kind:    types.UnionMemberTag,
					TagName: member.TagName,
					TagArgs: tagArgs,
				})
			}
		}
	}
	typeID := tc.types.RegisterUnionInstance(typeItem.Name, typeItem.Span, args)
	tc.types.SetUnionMembers(typeID, members)
	return typeID
}

func (tc *typeChecker) populateUnionType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	unionDecl := tc.builder.Items.TypeUnion(typeItem)
	if unionDecl == nil {
		return
	}
	symID := tc.typeSymbolForItem(itemID)
	pushed := tc.pushTypeParams(symID, typeItem.Generics, nil)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	scope := tc.fileScope()
	members := make([]types.UnionMember, 0, unionDecl.MembersCount)
	if unionDecl.MembersCount > 0 {
		start := uint32(unionDecl.MembersStart)
		count := int(unionDecl.MembersCount)
		for offset := range count {
			uoff, err := safecast.Conv[uint32](offset)
			if err != nil {
				panic(fmt.Errorf("union member offset overflow: %w", err))
			}
			memberID := ast.TypeUnionMemberID(start + uoff)
			member := tc.builder.Items.UnionMember(memberID)
			if member == nil {
				continue
			}
			switch member.Kind {
			case ast.TypeUnionMemberType:
				typ := tc.resolveTypeExprWithScope(member.Type, scope)
				members = append(members, types.UnionMember{
					Kind: types.UnionMemberType,
					Type: typ,
				})
			case ast.TypeUnionMemberNothing:
				members = append(members, types.UnionMember{
					Kind: types.UnionMemberNothing,
					Type: tc.types.Builtins().Nothing,
				})
			case ast.TypeUnionMemberTag:
				tagArgs := make([]types.TypeID, 0, len(member.TagArgs))
				for _, arg := range member.TagArgs {
					tagArgs = append(tagArgs, tc.resolveTypeExprWithScope(arg, scope))
				}
				members = append(members, types.UnionMember{
					Kind:    types.UnionMemberTag,
					TagName: member.TagName,
					TagArgs: tagArgs,
				})
			}
		}
	}
	tc.types.SetUnionMembers(typeID, members)
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

func (tc *typeChecker) resolveOptionType(inner types.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if inner == types.NoTypeID || tc.builder == nil {
		return types.NoTypeID
	}
	name := tc.builder.StringsInterner.Intern("Option")
	args := []types.TypeID{inner}
	return tc.resolveNamedType(name, args, span, scope)
}

func (tc *typeChecker) resolveErrorType(span source.Span, scope symbols.ScopeID) types.TypeID {
	if tc.builder == nil {
		return types.NoTypeID
	}
	errName := tc.builder.StringsInterner.Intern("Error")
	return tc.resolveNamedType(errName, nil, span, scope)
}

func (tc *typeChecker) resolveResultType(okType, errType types.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	if okType == types.NoTypeID || errType == types.NoTypeID || tc.builder == nil {
		return types.NoTypeID
	}
	name := tc.builder.StringsInterner.Intern("Result")
	args := []types.TypeID{okType, errType}
	return tc.resolveNamedType(name, args, span, scope)
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
