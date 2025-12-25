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

// referenceType creates a reference type for the given element type.
func (l *lowerer) referenceType(elem types.TypeID, mutable bool) types.TypeID {
	if elem == types.NoTypeID || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return types.NoTypeID
	}
	return l.semaRes.TypeInterner.Intern(types.MakeReference(elem, mutable))
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
