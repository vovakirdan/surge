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
	if _, ok, recvMut := l.referenceInfo(recv.Type); ok {
		if mut {
			if !recvMut {
				return recv
			}
			if isBorrowExpr(recv) {
				return recv
			}
			if recv.Kind == ExprVarRef {
				return l.applyBorrow(recv, true)
			}
			return recv
		}
		if recvMut {
			if isBorrowExpr(recv) {
				return recv
			}
			if recv.Kind == ExprVarRef {
				return l.applyBorrow(recv, false)
			}
			return recv
		}
		return recv
	}
	return l.applyBorrow(recv, mut)
}

func (l *lowerer) applyBorrow(value *Expr, mut bool) *Expr {
	if value == nil {
		return nil
	}
	if elem, ok, recvMut := l.referenceInfo(value.Type); ok {
		if mut && !recvMut {
			return value
		}
		deref := &Expr{
			Kind: ExprUnaryOp,
			Type: elem,
			Span: value.Span,
			Data: UnaryOpData{
				Op:      ast.ExprUnaryDeref,
				Operand: value,
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
			Span: value.Span,
			Data: UnaryOpData{
				Op:      op,
				Operand: deref,
			},
		}
	}
	refType := l.referenceType(value.Type, mut)
	op := ast.ExprUnaryRef
	if mut {
		op = ast.ExprUnaryRefMut
	}
	return &Expr{
		Kind: ExprUnaryOp,
		Type: refType,
		Span: value.Span,
		Data: UnaryOpData{
			Op:      op,
			Operand: value,
		},
	}
}

func (l *lowerer) applyDeref(value *Expr) *Expr {
	if value == nil {
		return nil
	}
	elem, ok, _ := l.referenceInfo(value.Type)
	if !ok {
		return value
	}
	return &Expr{
		Kind: ExprUnaryOp,
		Type: elem,
		Span: value.Span,
		Data: UnaryOpData{
			Op:      ast.ExprUnaryDeref,
			Operand: value,
		},
	}
}

func (l *lowerer) applyParamBorrow(symID symbols.SymbolID, args []*Expr) []*Expr {
	if !symID.IsValid() || l.symRes == nil || l.symRes.Table == nil || l.symRes.Table.Symbols == nil {
		return args
	}
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil || sym.Signature == nil || len(sym.Signature.Params) == 0 {
		return args
	}
	for i, arg := range args {
		if arg == nil || i >= len(sym.Signature.Params) {
			continue
		}
		// A variadic parameter's spelling names its element (`...args: &int`),
		// but its argument here is the packed array, which the parameter owns
		// (`args: (&int)[]`); borrowing the array would pass `&(&int)[]`.
		if i < len(sym.Signature.Variadic) && sym.Signature.Variadic[i] {
			continue
		}
		_, argIsRef, argIsMut := l.referenceInfo(arg.Type)
		param := strings.TrimSpace(string(sym.Signature.Params[i]))
		switch {
		case strings.HasPrefix(param, "&mut "):
			if argIsRef {
				if isBorrowExpr(arg) {
					continue
				}
				if arg.Kind == ExprVarRef {
					args[i] = l.applyBorrow(arg, true)
				}
				continue
			}
			args[i] = l.applyBorrow(arg, true)
		case strings.HasPrefix(param, "&"):
			if argIsRef {
				if argIsMut {
					args[i] = l.applyBorrow(arg, false)
				}
				continue
			}
			args[i] = l.applyBorrow(arg, false)
		default:
			if argIsRef && !l.passesReferenceAsValue(arg) {
				args[i] = l.applyDeref(arg)
			}
		}
	}
	return args
}

// noteReferenceValueArg remembers a lowered argument that sema matched, as a
// reference, against a by-value parameter instantiated with a reference type.
func (l *lowerer) noteReferenceValueArg(exprID ast.ExprID, lowered *Expr) {
	if l == nil || lowered == nil || l.semaRes == nil || !exprID.IsValid() {
		return
	}
	if _, ok := l.semaRes.ReferenceValueArgs[exprID]; !ok {
		return
	}
	if l.referenceValueArgs == nil {
		l.referenceValueArgs = make(map[*Expr]struct{})
	}
	l.referenceValueArgs[lowered] = struct{}{}
}

// passesReferenceAsValue reports whether a reference argument IS the value its
// by-value parameter takes, as in `Some::<&mut int>(&mut r[0])`. Reading
// through it there would hand the callee the referent's bits where it expects
// an address.
func (l *lowerer) passesReferenceAsValue(arg *Expr) bool {
	if l == nil || arg == nil {
		return false
	}
	_, ok := l.referenceValueArgs[arg]
	return ok
}

func isBorrowExpr(e *Expr) bool {
	if e == nil || e.Kind != ExprUnaryOp {
		return false
	}
	data, ok := e.Data.(UnaryOpData)
	if !ok {
		return false
	}
	return data.Op == ast.ExprUnaryRef || data.Op == ast.ExprUnaryRefMut
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
func (l *lowerer) wrapInSome(inner *Expr, targetType types.TypeID, callee symbols.SymbolID, span source.Span) *Expr {
	return l.wrapInTagConstructor(inner, targetType, "Some", callee, span)
}

// wrapInSuccess wraps an expression in a Success() tag constructor call.
// This is used for implicit tag injection: let x: int! = 1 becomes Success(1).
func (l *lowerer) wrapInSuccess(inner *Expr, targetType types.TypeID, callee symbols.SymbolID, span source.Span) *Expr {
	return l.wrapInTagConstructor(inner, targetType, "Success", callee, span)
}

// wrapInTagConstructor creates a call to a tag constructor wrapping the inner expression.
func (l *lowerer) wrapInTagConstructor(inner *Expr, targetType types.TypeID, tagName string, tagSymID symbols.SymbolID, span source.Span) *Expr {
	if inner == nil {
		return nil
	}

	// Look up the tag symbol for the constructor
	if !tagSymID.IsValid() && l.symRes != nil && l.symRes.Table != nil && l.strings != nil {
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

	// Sema registered this synthetic call at the conversion site. Lowering
	// can erase groups or Copy clones, so the payload may have a different
	// span. Preserve both identities for authoritative monomorphization.
	return &Expr{
		Kind: ExprCall,
		Type: targetType,
		Span: span,
		Data: CallData{
			Callee: &Expr{
				Kind: ExprVarRef,
				Type: types.NoTypeID, // Callee type doesn't matter for dispatch
				Span: span,
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

func (l *lowerer) tagUnionUpcast(inner *Expr, targetType types.TypeID) *Expr {
	if inner == nil {
		return nil
	}
	return &Expr{
		Kind: ExprCast,
		Type: targetType,
		Span: inner.Span,
		Data: CastData{
			Value:    inner,
			TargetTy: targetType,
		},
	}
}
