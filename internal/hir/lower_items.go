package hir

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// lowerFnItem lowers a function declaration to HIR.
func (l *lowerer) lowerFnItem(itemID ast.ItemID) *Func {
	fnItem, ok := l.builder.Items.Fn(itemID)
	if !ok || fnItem == nil {
		return nil
	}

	name := l.lookupString(fnItem.Name)
	fnID := l.nextFnID
	l.nextFnID++

	// Get symbol ID for this function
	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	fn := &Func{
		ID:       fnID,
		Name:     name,
		SymbolID: symID,
		Span:     fnItem.Span,
		Result:   types.NoTypeID,
	}

	// Process modifiers/flags
	if fnItem.Flags&ast.FnModifierAsync != 0 {
		fn.Flags |= FuncAsync
	}
	if fnItem.Flags&ast.FnModifierPublic != 0 {
		fn.Flags |= FuncPublic
	}

	// Check for attributes (@intrinsic, @entrypoint, @overload, @override)
	fn.Flags |= l.extractFnFlags(fnItem)

	// Process generic parameters
	fn.GenericParams = l.lowerGenericParams(fnItem)

	// Process function parameters
	fn.Params = l.lowerFnParams(itemID, fnItem)

	// Process return type from function symbol's type
	fn.Result = l.getFunctionReturnType(symID)

	// Process body
	if fnItem.Body.IsValid() {
		fn.Body = l.lowerBlockStmt(fnItem.Body)

		// Insert explicit return for last expression if needed
		l.ensureExplicitReturn(fn)
	}

	return fn
}

// extractFnFlags extracts function flags from attributes.
func (l *lowerer) extractFnFlags(fnItem *ast.FnItem) FuncFlags {
	var flags FuncFlags
	if fnItem.AttrCount == 0 || !fnItem.AttrStart.IsValid() {
		return flags
	}

	for i := range fnItem.AttrCount {
		attrID := ast.AttrID(uint32(fnItem.AttrStart) + i)
		attr := l.builder.Items.Attrs.Get(uint32(attrID))
		if attr == nil {
			continue
		}
		name := l.lookupString(attr.Name)
		switch name {
		case "intrinsic":
			flags |= FuncIntrinsic
		case "entrypoint":
			flags |= FuncEntrypoint
		case "overload":
			flags |= FuncOverload
		case "override":
			flags |= FuncOverride
		}
	}
	return flags
}

// lowerGenericParams lowers generic type parameters.
func (l *lowerer) lowerGenericParams(fnItem *ast.FnItem) []GenericParam {
	typeParamIDs := l.builder.Items.GetFnTypeParamIDs(fnItem)
	if len(typeParamIDs) == 0 {
		return nil
	}

	params := make([]GenericParam, 0, len(typeParamIDs))
	for _, tpID := range typeParamIDs {
		tp := l.builder.Items.TypeParams.Get(uint32(tpID))
		if tp == nil {
			continue
		}
		params = append(params, GenericParam{
			Name: l.lookupString(tp.Name),
			Span: tp.Span,
			// Bounds would be processed here if needed
		})
	}
	return params
}

// lowerFnParams lowers function parameters.
func (l *lowerer) lowerFnParams(fnItemID ast.ItemID, fnItem *ast.FnItem) []Param {
	paramIDs := l.builder.Items.GetFnParamIDs(fnItem)
	if len(paramIDs) == 0 {
		return nil
	}

	// Get function scope to look up parameter symbols
	var fnScope symbols.ScopeID
	if l.semaRes != nil && l.semaRes.ItemScopes != nil {
		fnScope = l.semaRes.ItemScopes[fnItemID]
	}

	params := make([]Param, 0, len(paramIDs))
	for _, paramID := range paramIDs {
		param := l.builder.Items.FnParam(paramID)
		if param == nil {
			continue
		}

		p := Param{
			Name:       l.lookupString(param.Name),
			Span:       param.Span,
			HasDefault: param.Default.IsValid(),
		}

		// Try to get parameter type from sema bindings
		if fnScope.IsValid() && param.Name != 0 {
			symID := l.symbolInScope(fnScope, param.Name, symbols.SymbolParam)
			if symID.IsValid() && l.semaRes != nil && l.semaRes.BindingTypes != nil {
				p.Type = l.semaRes.BindingTypes[symID]
			}
		}

		// Fallback to AST type if sema type not found
		if p.Type == types.NoTypeID {
			p.Type = l.lookupTypeFromAST(param.Type)
		}

		p.Ownership = l.inferOwnership(p.Type)

		if param.Default.IsValid() {
			p.Default = l.lowerExpr(param.Default)
		}

		params = append(params, p)
	}
	return params
}

// symbolInScope finds a symbol by name and kind in the given scope.
func (l *lowerer) symbolInScope(scope symbols.ScopeID, name source.StringID, kind symbols.SymbolKind) symbols.SymbolID {
	if !scope.IsValid() || name == source.NoStringID || l.symRes == nil || l.symRes.Table == nil {
		return symbols.NoSymbolID
	}
	scopeData := l.symRes.Table.Scopes.Get(scope)
	if scopeData == nil {
		return symbols.NoSymbolID
	}
	ids := scopeData.NameIndex[name]
	for i := len(ids) - 1; i >= 0; i-- {
		symID := ids[i]
		sym := l.symRes.Table.Symbols.Get(symID)
		if sym == nil {
			continue
		}
		if sym.Kind == kind {
			return symID
		}
	}
	return symbols.NoSymbolID
}

// getFunctionReturnType extracts return type from a function symbol's type.
func (l *lowerer) getFunctionReturnType(symID symbols.SymbolID) types.TypeID {
	if !symID.IsValid() || l.symRes == nil || l.symRes.Table == nil || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return types.NoTypeID
	}

	// Get function symbol's type from symbol table
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil || sym.Type == types.NoTypeID {
		return types.NoTypeID
	}

	// Extract return type from function type
	fnInfo, ok := l.semaRes.TypeInterner.FnInfo(sym.Type)
	if !ok || fnInfo == nil {
		return types.NoTypeID
	}
	return fnInfo.Result
}

// lowerLetItem lowers a top-level let declaration.
func (l *lowerer) lowerLetItem(itemID ast.ItemID) *VarDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemLet {
		return nil
	}

	letItem := l.builder.Items.Lets.Get(uint32(item.Payload))
	if letItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	decl := &VarDecl{
		Name:     l.lookupString(letItem.Name),
		SymbolID: symID,
		IsMut:    letItem.IsMut,
		Span:     item.Span,
	}

	if letItem.Type.IsValid() {
		decl.Type = l.lookupTypeFromAST(letItem.Type)
	}

	if letItem.Value.IsValid() {
		decl.Value = l.lowerExpr(letItem.Value)
		if decl.Type == types.NoTypeID && decl.Value != nil {
			decl.Type = decl.Value.Type
		}
	}

	return decl
}

// lowerConstItem lowers a top-level const declaration.
func (l *lowerer) lowerConstItem(itemID ast.ItemID) *ConstDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemConst {
		return nil
	}

	constItem := l.builder.Items.Consts.Get(uint32(item.Payload))
	if constItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	decl := &ConstDecl{
		Name:     l.lookupString(constItem.Name),
		SymbolID: symID,
		Span:     item.Span,
	}

	if constItem.Type.IsValid() {
		decl.Type = l.lookupTypeFromAST(constItem.Type)
	}

	if constItem.Value.IsValid() {
		decl.Value = l.lowerExpr(constItem.Value)
		if decl.Type == types.NoTypeID && decl.Value != nil {
			decl.Type = decl.Value.Type
		}
	}

	return decl
}

// lowerTypeItem lowers a type declaration.
func (l *lowerer) lowerTypeItem(itemID ast.ItemID) *TypeDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemType {
		return nil
	}

	typeItem := l.builder.Items.Types.Get(uint32(item.Payload))
	if typeItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	decl := &TypeDecl{
		Name:     l.lookupString(typeItem.Name),
		SymbolID: symID,
		Span:     item.Span,
	}

	// Determine kind from AST type declaration kind
	switch typeItem.Kind {
	case ast.TypeDeclStruct:
		decl.Kind = TypeDeclStruct
	case ast.TypeDeclUnion:
		decl.Kind = TypeDeclUnion
	case ast.TypeDeclEnum:
		decl.Kind = TypeDeclEnum
	case ast.TypeDeclAlias:
		decl.Kind = TypeDeclAlias
	}

	return decl
}

// lowerTagItem lowers a tag declaration.
func (l *lowerer) lowerTagItem(itemID ast.ItemID) *TypeDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemTag {
		return nil
	}

	tagItem := l.builder.Items.Tags.Get(uint32(item.Payload))
	if tagItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	return &TypeDecl{
		Name:     l.lookupString(tagItem.Name),
		SymbolID: symID,
		Kind:     TypeDeclTag,
		Span:     item.Span,
	}
}

// lowerContractItem lowers a contract declaration.
func (l *lowerer) lowerContractItem(itemID ast.ItemID) *TypeDecl {
	item := l.builder.Items.Arena.Get(uint32(itemID))
	if item == nil || item.Kind != ast.ItemContract {
		return nil
	}

	contractItem := l.builder.Items.Contracts.Get(uint32(item.Payload))
	if contractItem == nil {
		return nil
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		if syms, ok := l.symRes.ItemSymbols[itemID]; ok && len(syms) > 0 {
			symID = syms[0]
		}
	}

	return &TypeDecl{
		Name:     l.lookupString(contractItem.Name),
		SymbolID: symID,
		Kind:     TypeDeclContract,
		Span:     item.Span,
	}
}
