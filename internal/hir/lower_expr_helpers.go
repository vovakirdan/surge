package hir

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// lookupString looks up a string by its ID.
func (l *lowerer) lookupString(id source.StringID) string {
	if l.strings == nil || id == source.NoStringID {
		return ""
	}
	s, _ := l.strings.Lookup(id)
	return s
}

// varRefForSymbol creates a VarRef expression for a symbol.
func (l *lowerer) varRefForSymbol(symID symbols.SymbolID, span source.Span) *Expr {
	if !symID.IsValid() {
		return nil
	}
	name := ""
	ty := types.NoTypeID
	if l.symRes != nil && l.symRes.Table != nil && l.symRes.Table.Symbols != nil {
		if sym := l.symRes.Table.Symbols.Get(symID); sym != nil {
			if sym.Name != source.NoStringID {
				name = l.lookupString(sym.Name)
			}
			if sym.Type != types.NoTypeID {
				ty = sym.Type
			}
		}
	}
	return &Expr{
		Kind: ExprVarRef,
		Type: ty,
		Span: span,
		Data: VarRefData{
			Name:     name,
			SymbolID: symID,
		},
	}
}

func (l *lowerer) defaultValueExpr(span source.Span, typeID types.TypeID) *Expr {
	if l == nil || typeID == types.NoTypeID {
		return nil
	}
	callee, symID := l.intrinsicCallee("default", span)
	return &Expr{
		Kind: ExprCall,
		Type: typeID,
		Span: span,
		Data: CallData{
			Callee:   callee,
			Args:     nil,
			SymbolID: symID,
		},
	}
}

func (l *lowerer) toCallExpr(span source.Span, value *Expr, target types.TypeID, symID symbols.SymbolID) *Expr {
	if value == nil {
		return nil
	}
	if symID.IsValid() {
		args := l.applyParamBorrow(symID, []*Expr{value})
		if len(args) == 1 {
			value = args[0]
		}
	}
	callee := l.varRefForSymbol(symID, span)
	if callee == nil {
		callee = &Expr{
			Kind: ExprVarRef,
			Type: types.NoTypeID,
			Span: span,
			Data: VarRefData{
				Name:     "__to",
				SymbolID: symID,
			},
		}
	}
	return &Expr{
		Kind: ExprCall,
		Type: target,
		Span: span,
		Data: CallData{
			Callee:   callee,
			Args:     []*Expr{value},
			SymbolID: symID,
		},
	}
}

// referenceType creates a reference type for the given element type.
func (l *lowerer) referenceType(elem types.TypeID, mutable bool) types.TypeID {
	if elem == types.NoTypeID || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return types.NoTypeID
	}
	return l.semaRes.TypeInterner.Intern(types.MakeReference(elem, mutable))
}

func (l *lowerer) isArrayType(ty types.TypeID) bool {
	if ty == types.NoTypeID || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return false
	}
	typesIn := l.semaRes.TypeInterner
	if _, ok := typesIn.ArrayInfo(ty); ok {
		return true
	}
	if _, _, ok := typesIn.ArrayFixedInfo(ty); ok {
		return true
	}
	if tt, ok := typesIn.Lookup(ty); ok && tt.Kind == types.KindArray {
		return true
	}
	return false
}

func (l *lowerer) arrayTypeFromElem(elem types.TypeID) types.TypeID {
	if elem == types.NoTypeID || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return types.NoTypeID
	}
	typesIn := l.semaRes.TypeInterner
	if base := typesIn.ArrayNominalType(); base != types.NoTypeID {
		if info, ok := typesIn.StructInfo(base); ok && info != nil && info.Name != source.NoStringID {
			if inst, ok := typesIn.FindStructInstance(info.Name, []types.TypeID{elem}); ok {
				return inst
			}
			return typesIn.RegisterStructInstance(info.Name, info.Decl, []types.TypeID{elem})
		}
	}
	return typesIn.Intern(types.MakeArray(elem, types.ArrayDynamicLength))
}

func (l *lowerer) variadicParamArrayType(symID symbols.SymbolID, variadicIndex int) types.TypeID {
	if variadicIndex < 0 || l == nil || l.builder == nil || l.symRes == nil || l.symRes.Table == nil {
		return types.NoTypeID
	}
	if l.semaRes == nil || l.semaRes.BindingTypes == nil || l.semaRes.ItemScopes == nil {
		return types.NoTypeID
	}
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil || !sym.Decl.Item.IsValid() {
		return types.NoTypeID
	}
	fnItem, ok := l.builder.Items.Fn(sym.Decl.Item)
	if !ok || fnItem == nil {
		return types.NoTypeID
	}
	paramIDs := l.builder.Items.GetFnParamIDs(fnItem)
	if variadicIndex >= len(paramIDs) {
		return types.NoTypeID
	}
	param := l.builder.Items.FnParam(paramIDs[variadicIndex])
	if param == nil || param.Name == source.NoStringID {
		return types.NoTypeID
	}
	fnScope := l.semaRes.ItemScopes[sym.Decl.Item]
	symID = l.symbolInScope(fnScope, param.Name, symbols.SymbolParam)
	if symID.IsValid() {
		if ty := l.semaRes.BindingTypes[symID]; ty != types.NoTypeID {
			return ty
		}
	}
	return types.NoTypeID
}

// lookupTypeFromAST looks up a type from AST type ID.
func (l *lowerer) lookupTypeFromAST(_ ast.TypeID) types.TypeID {
	// AST TypeID is different from types.TypeID
	// For now, we return NoTypeID - the actual type would need
	// to be resolved through the type checker's type expressions
	// This is a simplification - in a full implementation,
	// we'd need to map AST type expressions to resolved types
	return types.NoTypeID
}

// inferOwnership infers ownership from a type.
func (l *lowerer) inferOwnership(ty types.TypeID) Ownership {
	if ty == types.NoTypeID || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return OwnershipNone
	}

	t, ok := l.semaRes.TypeInterner.Lookup(ty)
	if !ok {
		return OwnershipNone
	}

	switch t.Kind {
	case types.KindReference:
		if t.Mutable {
			return OwnershipRefMut
		}
		return OwnershipRef
	case types.KindPointer:
		return OwnershipPtr
	case types.KindOwn:
		return OwnershipOwn
	case types.KindInt, types.KindUint, types.KindFloat, types.KindBool:
		return OwnershipCopy
	default:
		return OwnershipNone
	}
}

//nolint:gocritic // ifElseChain is clearer than switch for character ranges
func parseIntLiteral(s string) int64 {
	var result int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int64(c-'0')
		} else if c == '_' {
			continue
		} else {
			break
		}
	}
	return result
}

//nolint:gocritic // ifElseChain is clearer than switch for character ranges
func parseFloatLiteral(s string) float64 {
	var result float64
	var frac float64 = 1
	var inFrac bool
	for _, c := range s {
		if c >= '0' && c <= '9' {
			if inFrac {
				frac /= 10
				result += float64(c-'0') * frac
			} else {
				result = result*10 + float64(c-'0')
			}
		} else if c == '.' {
			inFrac = true
		} else if c == '_' {
			continue
		} else {
			break
		}
	}
	return result
}
