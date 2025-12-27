package sema

import (
	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) stripOwnType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}
	for range 32 {
		tt, ok := tc.types.Lookup(id)
		if !ok || tt.Kind != types.KindOwn {
			return id
		}
		id = tt.Elem
	}
	return id
}

func (tc *typeChecker) resolveTypeOperand(exprID ast.ExprID, opLabel string) (types.TypeID, bool) {
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		tc.reportExpectTypeOperand(opLabel, exprID)
		return types.NoTypeID, false
	}
	switch expr.Kind {
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(exprID); ok && group != nil {
			return tc.resolveTypeOperand(group.Inner, opLabel)
		}
	case ast.ExprUnary:
		if unary, ok := tc.builder.Exprs.Unary(exprID); ok && unary != nil {
			switch unary.Op {
			case ast.ExprUnaryOwn:
				if inner, ok := tc.resolveTypeOperand(unary.Operand, opLabel); ok {
					return tc.types.Intern(types.MakeOwn(inner)), true
				}
			case ast.ExprUnaryRef, ast.ExprUnaryRefMut:
				if inner, ok := tc.resolveTypeOperand(unary.Operand, opLabel); ok {
					mutable := unary.Op == ast.ExprUnaryRefMut
					return tc.types.Intern(types.MakeReference(inner, mutable)), true
				}
			case ast.ExprUnaryDeref:
				if inner, ok := tc.resolveTypeOperand(unary.Operand, opLabel); ok {
					return tc.types.Intern(types.MakePointer(inner)), true
				}
			}
		}
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(exprID); ok && ident != nil {
			if symID := tc.symbolForExpr(exprID); symID.IsValid() {
				if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolType {
					return sym.Type, true
				}
			}
			if literal := tc.lookupName(ident.Name); literal != "" {
				if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
					return builtin, true
				}
			}
			scope := tc.scopeOrFile(tc.currentScope())
			if symID := tc.lookupTypeSymbol(ident.Name, scope); symID.IsValid() {
				return tc.symbolType(symID), true
			}
		}
	case ast.ExprLit:
		// Handle 'nothing' literal as type operand
		if lit, ok := tc.builder.Exprs.Literal(exprID); ok && lit != nil {
			if lit.Kind == ast.ExprLitNothing {
				return tc.types.Builtins().Nothing, true
			}
		}
	default:
		// fallthrough to error reporting
	}
	tc.reportExpectTypeOperand(opLabel, exprID)
	return types.NoTypeID, false
}

// tryResolveTypeOperand attempts to resolve an expression used as a type operand without emitting diagnostics.
func (tc *typeChecker) tryResolveTypeOperand(exprID ast.ExprID) types.TypeID {
	if !exprID.IsValid() || tc.builder == nil {
		return types.NoTypeID
	}
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return types.NoTypeID
	}
	switch expr.Kind {
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(exprID); ok && group != nil {
			return tc.tryResolveTypeOperand(group.Inner)
		}
	case ast.ExprUnary:
		if unary, ok := tc.builder.Exprs.Unary(exprID); ok && unary != nil {
			switch unary.Op {
			case ast.ExprUnaryOwn:
				if inner := tc.tryResolveTypeOperand(unary.Operand); inner != types.NoTypeID {
					return tc.types.Intern(types.MakeOwn(inner))
				}
			case ast.ExprUnaryRef, ast.ExprUnaryRefMut:
				if inner := tc.tryResolveTypeOperand(unary.Operand); inner != types.NoTypeID {
					mutable := unary.Op == ast.ExprUnaryRefMut
					return tc.types.Intern(types.MakeReference(inner, mutable))
				}
			case ast.ExprUnaryDeref:
				if inner := tc.tryResolveTypeOperand(unary.Operand); inner != types.NoTypeID {
					return tc.types.Intern(types.MakePointer(inner))
				}
			}
		}
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(exprID); ok && ident != nil {
			if symID := tc.symbolForExpr(exprID); symID.IsValid() {
				if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolType && sym.Type != types.NoTypeID {
					return sym.Type
				}
			}
			if param := tc.lookupTypeParam(ident.Name); param != types.NoTypeID {
				return param
			}
			if literal := tc.lookupName(ident.Name); literal != "" {
				if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
					return builtin
				}
			}
			scope := tc.scopeOrFile(tc.currentScope())
			if symID := tc.lookupTypeSymbol(ident.Name, scope); symID.IsValid() {
				return tc.symbolType(symID)
			}
		}
	case ast.ExprLit:
		// Handle 'nothing' literal as type operand
		if lit, ok := tc.builder.Exprs.Literal(exprID); ok && lit != nil {
			if lit.Kind == ast.ExprLitNothing {
				return tc.types.Builtins().Nothing
			}
		}
	}
	return types.NoTypeID
}
