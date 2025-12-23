package hir

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// intrinsicSymbolID returns the symbol ID for an intrinsic function.
func (l *lowerer) intrinsicSymbolID(name string) symbols.SymbolID {
	if l == nil || l.symRes == nil || l.symRes.Table == nil || !l.symRes.FileScope.IsValid() {
		return symbols.NoSymbolID
	}
	nameID := l.strings.Intern(name)
	return l.symbolInScope(l.symRes.FileScope, nameID, symbols.SymbolFunction)
}

// intrinsicCallee creates a callee expression for an intrinsic function.
func (l *lowerer) intrinsicCallee(name string, span source.Span) (*Expr, symbols.SymbolID) {
	symID := l.intrinsicSymbolID(name)
	return &Expr{
		Kind: ExprVarRef,
		Type: types.NoTypeID,
		Span: span,
		Data: VarRefData{Name: name, SymbolID: symID},
	}, symID
}

// boolLiteralExpr creates a boolean literal expression.
func (l *lowerer) boolLiteralExpr(span source.Span, value bool) *Expr {
	boolType := types.NoTypeID
	if l != nil && l.module != nil && l.module.TypeInterner != nil {
		boolType = l.module.TypeInterner.Builtins().Bool
	}
	return &Expr{
		Kind: ExprLiteral,
		Type: boolType,
		Span: span,
		Data: LiteralData{Kind: LiteralBool, BoolValue: value},
	}
}

// applySelfBorrow applies a borrow operation for method receivers.
func (l *lowerer) applySelfBorrow(symID symbols.SymbolID, recv *Expr) *Expr {
	if recv == nil || !symID.IsValid() {
		return recv
	}
	if l.symRes == nil || l.symRes.Table == nil || l.symRes.Table.Symbols == nil {
		return recv
	}
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil || sym.Signature == nil || !sym.Signature.HasSelf || len(sym.Signature.Params) == 0 {
		return recv
	}
	selfKey := string(sym.Signature.Params[0])
	mut := false
	switch {
	case strings.HasPrefix(selfKey, "&mut "):
		mut = true
	case strings.HasPrefix(selfKey, "&"):
	default:
		return recv
	}
	if elem, ok, recvMut := l.referenceInfo(recv.Type); ok {
		if mut && !recvMut {
			return recv
		}
		deref := &Expr{
			Kind: ExprUnaryOp,
			Type: elem,
			Span: recv.Span,
			Data: UnaryOpData{
				Op:      ast.ExprUnaryDeref,
				Operand: recv,
			},
		}
		refType := l.referenceType(elem, mut)
		op := ast.ExprUnaryRef
		if mut {
			op = ast.ExprUnaryRefMut
		}
		return &Expr{
			Kind: ExprUnaryOp,
			Type: refType,
			Span: recv.Span,
			Data: UnaryOpData{
				Op:      op,
				Operand: deref,
			},
		}
	}
	refType := l.referenceType(recv.Type, mut)
	op := ast.ExprUnaryRef
	if mut {
		op = ast.ExprUnaryRefMut
	}
	return &Expr{
		Kind: ExprUnaryOp,
		Type: refType,
		Span: recv.Span,
		Data: UnaryOpData{
			Op:      op,
			Operand: recv,
		},
	}
}

func (l *lowerer) referenceInfo(id types.TypeID) (elem types.TypeID, ok, mut bool) {
	if id == types.NoTypeID || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return types.NoTypeID, false, false
	}
	tt, found := l.semaRes.TypeInterner.Lookup(id)
	if !found || tt.Kind != types.KindReference {
		return types.NoTypeID, false, false
	}
	return tt.Elem, true, tt.Mutable
}

// wrapInSome wraps an expression in a Some() tag constructor call.
// This is used for implicit tag injection: let x: int? = 1 becomes Some(1).
func (l *lowerer) wrapInSome(inner *Expr, targetType types.TypeID) *Expr {
	return l.wrapInTagConstructor(inner, targetType, "Some")
}

// wrapInSuccess wraps an expression in a Success() tag constructor call.
// This is used for implicit tag injection: let x: int! = 1 becomes Success(1).
func (l *lowerer) wrapInSuccess(inner *Expr, targetType types.TypeID) *Expr {
	return l.wrapInTagConstructor(inner, targetType, "Success")
}

// wrapInTagConstructor creates a call to a tag constructor wrapping the inner expression.
func (l *lowerer) wrapInTagConstructor(inner *Expr, targetType types.TypeID, tagName string) *Expr {
	if inner == nil {
		return nil
	}

	// Look up the tag symbol for the constructor
	var tagSymID symbols.SymbolID
	if l.symRes != nil && l.symRes.Table != nil && l.strings != nil {
		nameID := l.strings.Intern(tagName)
		// Look up tag in file scope
		if l.symRes.Table.Scopes != nil && l.symRes.FileScope.IsValid() {
			scopeData := l.symRes.Table.Scopes.Get(l.symRes.FileScope)
			if scopeData != nil {
				if ids := scopeData.NameIndex[nameID]; len(ids) > 0 {
					for _, id := range ids {
						sym := l.symRes.Table.Symbols.Get(id)
						if sym != nil && sym.Kind == symbols.SymbolTag {
							tagSymID = id
							break
						}
					}
				}
			}
		}
	}

	// Create a call expression that wraps the inner expression
	return &Expr{
		Kind: ExprCall,
		Type: targetType,
		Span: inner.Span,
		Data: CallData{
			Callee: &Expr{
				Kind: ExprVarRef,
				Type: types.NoTypeID, // Callee type doesn't matter for dispatch
				Span: inner.Span,
				Data: VarRefData{
					Name:     tagName,
					SymbolID: tagSymID,
				},
			},
			Args:     []*Expr{inner},
			SymbolID: tagSymID,
		},
	}
}
