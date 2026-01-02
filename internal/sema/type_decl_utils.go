package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) typeSymbolForItem(itemID ast.ItemID) symbols.SymbolID {
	if tc.symbols == nil || tc.symbols.ItemSymbols == nil {
		// fall through to lookup by name
	} else {
		syms := tc.symbols.ItemSymbols[itemID]
		if len(syms) > 0 {
			return syms[0]
		}
	}
	if tc.builder != nil && tc.symbols != nil && tc.symbols.Table != nil {
		if typeItem, ok := tc.builder.Items.Type(itemID); ok && typeItem != nil {
			if scope := tc.fileScope(); scope.IsValid() {
				if symID := tc.symbolInScope(scope, typeItem.Name, symbols.SymbolType); symID.IsValid() {
					return symID
				}
			}
		}
	}
	return symbols.NoSymbolID
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

func (tc *typeChecker) attachTypeParamSymbols(symID symbols.SymbolID, params []symbols.TypeParamSymbol) {
	if !symID.IsValid() {
		return
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return
	}
	sym.TypeParamSymbols = append([]symbols.TypeParamSymbol(nil), params...)
}

func (tc *typeChecker) defaultable(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	id = tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return false
	}
	if elem, ok := tc.arrayElemType(id); ok {
		return tc.defaultable(elem)
	}
	switch tt.Kind {
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool, types.KindString, types.KindNothing, types.KindUnit:
		return true
	case types.KindConst:
		return true
	case types.KindGenericParam:
		return true
	case types.KindPointer:
		return true
	case types.KindReference, types.KindOwn:
		return false
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(id); ok && info != nil {
			for _, field := range info.Fields {
				if !tc.defaultable(field.Type) {
					return false
				}
			}
			return true
		}
		return false
	case types.KindAlias:
		if target, ok := tc.types.AliasTarget(id); ok && target != types.NoTypeID {
			return tc.defaultable(target)
		}
		return false
	case types.KindUnion:
		info, ok := tc.types.UnionInfo(id)
		if !ok || info == nil {
			return false
		}
		for _, m := range info.Members {
			if m.Kind == types.UnionMemberNothing {
				return true
			}
		}
		return false
	default:
		return false
	}
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

func (tc *typeChecker) lookupConstSymbol(name source.StringID, scope symbols.ScopeID) symbols.SymbolID {
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
				if sym.Kind == symbols.SymbolConst {
					return id
				}
			}
		}
		scope = scopeData.Parent
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) lookupTagSymbol(name source.StringID, scope symbols.ScopeID) symbols.SymbolID {
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
				if sym.Kind == symbols.SymbolTag {
					return id
				}
			}
		}
		scope = scopeData.Parent
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) lookupValueSymbol(name source.StringID, scope symbols.ScopeID) symbols.SymbolID {
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
				symID := ids[i]
				sym := tc.symbols.Table.Symbols.Get(symID)
				if sym == nil {
					continue
				}
				if sym.Kind == symbols.SymbolParam || sym.Kind == symbols.SymbolLet || sym.Kind == symbols.SymbolConst {
					return symID
				}
			}
		}
		scope = scopeData.Parent
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) lookupContractSymbol(name source.StringID, scope symbols.ScopeID) symbols.SymbolID {
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
				if sym.Kind == symbols.SymbolContract {
					return id
				}
			}
		}
		scope = scopeData.Parent
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) lookupSymbolAny(name source.StringID, scope symbols.ScopeID) symbols.SymbolID {
	if name == source.NoStringID || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil || tc.symbols.Table.Symbols == nil {
		return symbols.NoSymbolID
	}
	for scope = tc.scopeOrFile(scope); scope.IsValid(); {
		scopeData := tc.symbols.Table.Scopes.Get(scope)
		if scopeData == nil {
			break
		}
		if ids := scopeData.NameIndex[name]; len(ids) > 0 {
			return ids[len(ids)-1]
		}
		scope = scopeData.Parent
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) builtinTypeByName(name string) types.TypeID {
	switch name {
	case "int":
		return tc.types.Builtins().Int
	case "int8":
		return tc.types.Builtins().Int8
	case "int16":
		return tc.types.Builtins().Int16
	case "int32":
		return tc.types.Builtins().Int32
	case "int64":
		return tc.types.Builtins().Int64
	case "uint":
		return tc.types.Builtins().Uint
	case "uint8":
		return tc.types.Builtins().Uint8
	case "uint16":
		return tc.types.Builtins().Uint16
	case "uint32":
		return tc.types.Builtins().Uint32
	case "uint64":
		return tc.types.Builtins().Uint64
	case "float":
		return tc.types.Builtins().Float
	case "float16":
		return tc.types.Builtins().Float16
	case "float32":
		return tc.types.Builtins().Float32
	case "float64":
		return tc.types.Builtins().Float64
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

// taskType wraps the provided payload type into Task<payload> by resolving the nominal Task symbol.
func (tc *typeChecker) taskType(payload types.TypeID, span source.Span) types.TypeID {
	if payload == types.NoTypeID || tc.builder == nil || tc.builder.StringsInterner == nil {
		return types.NoTypeID
	}
	nameID := tc.builder.StringsInterner.Intern("Task")
	scope := tc.scopeOrFile(tc.currentScope())
	return tc.resolveNamedType(nameID, []types.TypeID{payload}, nil, span, scope)
}

func (tc *typeChecker) taskResultType(payload types.TypeID, span source.Span) types.TypeID {
	if payload == types.NoTypeID || tc.builder == nil || tc.builder.StringsInterner == nil {
		return types.NoTypeID
	}
	nameID := tc.builder.StringsInterner.Intern("TaskResult")
	scope := tc.scopeOrFile(tc.currentScope())
	return tc.resolveNamedType(nameID, []types.TypeID{payload}, nil, span, scope)
}

func (tc *typeChecker) taskPayloadType(taskType types.TypeID) types.TypeID {
	if taskType == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	resolved := tc.valueType(taskType)
	if resolved == types.NoTypeID {
		return types.NoTypeID
	}
	if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
		if tc.lookupTypeName(resolved, info.Name) == "Task" {
			args := tc.types.StructArgs(resolved)
			if len(args) == 1 {
				return args[0]
			}
		}
	}
	if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
		if tc.lookupTypeName(resolved, info.Name) == "Task" && len(info.TypeArgs) == 1 {
			return info.TypeArgs[0]
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) isTaskType(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	resolved := tc.resolveAlias(id)
	for {
		tt, ok := tc.types.Lookup(resolved)
		if !ok {
			break
		}
		switch tt.Kind {
		case types.KindReference, types.KindPointer, types.KindOwn:
			resolved = tc.resolveAlias(tt.Elem)
			continue
		}
		break
	}
	if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
		return tc.lookupTypeName(resolved, info.Name) == "Task"
	}
	if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
		return tc.lookupTypeName(resolved, info.Name) == "Task"
	}
	return false
}

// isChannelType checks if the given type is Channel<T>.
func (tc *typeChecker) isChannelType(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	resolved := tc.resolveAlias(id)
	for {
		tt, ok := tc.types.Lookup(resolved)
		if !ok {
			break
		}
		switch tt.Kind {
		case types.KindReference, types.KindPointer, types.KindOwn:
			resolved = tc.resolveAlias(tt.Elem)
			continue
		}
		break
	}
	if info, ok := tc.types.StructInfo(resolved); ok && info != nil {
		return tc.lookupTypeName(resolved, info.Name) == "Channel"
	}
	if info, ok := tc.types.AliasInfo(resolved); ok && info != nil {
		return tc.lookupTypeName(resolved, info.Name) == "Channel"
	}
	return false
}

// isCheckpointCall checks if the expression is a call to checkpoint().
func (tc *typeChecker) isCheckpointCall(exprID ast.ExprID) bool {
	if tc.builder == nil || tc.builder.Exprs == nil {
		return false
	}
	expr := tc.builder.Exprs.Get(exprID)
	if expr.Kind != ast.ExprCall {
		return false
	}
	call, ok := tc.builder.Exprs.Call(exprID)
	if !ok || call == nil {
		return false
	}
	// Check if target is an identifier named "checkpoint"
	targetExpr := tc.builder.Exprs.Get(call.Target)
	if targetExpr.Kind != ast.ExprIdent {
		return false
	}
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil {
		return false
	}
	name, _ := tc.builder.StringsInterner.Lookup(ident.Name)
	return name == "checkpoint"
}
