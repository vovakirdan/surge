package hir

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (l *lowerer) packVariadicArgs(symID symbols.SymbolID, args []*Expr, span source.Span) []*Expr {
	if l == nil || !symID.IsValid() || l.symRes == nil || l.symRes.Table == nil {
		return args
	}
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil || sym.Signature == nil {
		return args
	}
	variadicIndex := -1
	for i, v := range sym.Signature.Variadic {
		if v {
			variadicIndex = i
			break
		}
	}
	if variadicIndex < 0 || variadicIndex > len(args) {
		return args
	}

	arrayType := l.variadicParamArrayType(symID, variadicIndex)
	if arrayType == types.NoTypeID && variadicIndex < len(args) && args[variadicIndex] != nil {
		arrayType = l.arrayTypeFromElem(args[variadicIndex].Type)
	}
	arraySpan := span
	if variadicIndex < len(args) && args[variadicIndex] != nil {
		arraySpan = args[variadicIndex].Span
		if last := args[len(args)-1]; last != nil {
			arraySpan = arraySpan.Cover(last.Span)
		}
	}
	arrayExpr := &Expr{
		Kind: ExprArrayLit,
		Type: arrayType,
		Span: arraySpan,
		Data: ArrayLitData{Elements: args[variadicIndex:]},
	}
	packed := append([]*Expr(nil), args[:variadicIndex]...)
	packed = append(packed, arrayExpr)
	return packed
}

// lowerCallExpr lowers a function call expression.
func (l *lowerer) lowerCallExpr(exprID ast.ExprID, expr *ast.Expr, ty types.TypeID) *Expr {
	callData := l.builder.Exprs.Calls.Get(uint32(expr.Payload))
	if callData == nil {
		return nil
	}

	if l.semaRes != nil && l.semaRes.CloneSymbols != nil && len(callData.Args) == 1 {
		if ident, ok := l.builder.Exprs.Ident(callData.Target); ok && ident != nil {
			name := l.lookupString(ident.Name)
			if name == "clone" {
				arg := l.lowerExpr(callData.Args[0].Value)
				if arg == nil {
					return nil
				}
				if l.semaRes.TypeInterner != nil && l.semaRes.TypeInterner.IsCopy(ty) {
					if _, ok, _ := l.referenceInfo(arg.Type); ok {
						return l.applyDeref(arg)
					}
					return arg
				}
				if symID := l.semaRes.CloneSymbols[exprID]; symID.IsValid() {
					recv := l.applySelfBorrow(symID, arg)
					callee := l.varRefForSymbol(symID, expr.Span)
					return &Expr{
						Kind: ExprCall,
						Type: ty,
						Span: expr.Span,
						Data: CallData{
							Callee:   callee,
							Args:     []*Expr{recv},
							SymbolID: symID,
						},
					}
				}
			}
		}
	}

	member, isMember := l.builder.Exprs.Member(callData.Target)

	args := make([]*Expr, len(callData.Args))
	for i, arg := range callData.Args {
		args[i] = l.lowerExpr(arg.Value)
	}

	var symID symbols.SymbolID
	if l.symRes != nil {
		symID = l.symRes.ExprSymbols[exprID]
		if !symID.IsValid() && !isMember {
			if targetSym, ok := l.symRes.ExprSymbols[callData.Target]; ok && targetSym.IsValid() {
				symID = targetSym
			}
		}
	}

	if isMember && member != nil {
		if symID.IsValid() {
			recv := l.lowerExpr(member.Target)
			if recv != nil && recv.Type != types.NoTypeID {
				recv = l.applySelfBorrow(symID, recv)
				args = append([]*Expr{recv}, args...)
			}
			args = l.packVariadicArgs(symID, args, expr.Span)
			args = l.applyParamBorrow(symID, args)
			callee := l.varRefForSymbol(symID, expr.Span)
			return &Expr{
				Kind: ExprCall,
				Type: ty,
				Span: expr.Span,
				Data: CallData{
					Callee:   callee,
					Args:     args,
					SymbolID: symID,
				},
			}
		}
		if l.symRes != nil {
			if memberSym := l.symRes.ExprSymbols[callData.Target]; memberSym.IsValid() {
				args = l.packVariadicArgs(memberSym, args, expr.Span)
				args = l.applyParamBorrow(memberSym, args)
				callee := l.varRefForSymbol(memberSym, expr.Span)
				return &Expr{
					Kind: ExprCall,
					Type: ty,
					Span: expr.Span,
					Data: CallData{
						Callee:   callee,
						Args:     args,
						SymbolID: memberSym,
					},
				}
			}
		}
	}

	callee := l.lowerExpr(callData.Target)
	args = l.packVariadicArgs(symID, args, expr.Span)
	args = l.applyParamBorrow(symID, args)
	return &Expr{
		Kind: ExprCall,
		Type: ty,
		Span: expr.Span,
		Data: CallData{
			Callee:   callee,
			Args:     args,
			SymbolID: symID,
		},
	}
}
