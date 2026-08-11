package hir

import (
	"surge/internal/ast"
	"surge/internal/numlit"
	"surge/internal/sema"
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
	args := []*Expr{value}
	if symID.IsValid() && !l.isBuiltinSymbol(symID) {
		args = append(args, l.defaultValueExpr(span, target))
	}
	if symID.IsValid() {
		args = l.applyParamBorrow(symID, args)
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
			Args:     args,
			SymbolID: symID,
		},
	}
}

// foldNegatedIntLiteral turns `-<integer literal>` into the one literal it
// names, and returns nil for anything that is not exactly that shape.
//
// The difference is not cosmetic. Sema validates `-128` against int8's range
// and then types the WHOLE chain `int8` (materializeNumericLiteral →
// setExprTypes over info.exprIDs), which leaves the inner `128` stamped with a
// type that cannot hold it. The VM never notices, because its `__neg` intrinsic
// negates a value kept in an untruncated int64; a backend that materialises the
// constant at the type's own width reads that operand as -128 and then reports
// the negation of it as an overflow, which is a panic for a program that is
// perfectly in range.
//
// Folding here means no backend is ever handed the out-of-range magnitude. It
// is also what lets `let a: int64 = -9223372036854775808` answer with itself
// instead of with 0: that magnitude has no int64 to be parsed into either, so
// the sign has to join the literal BEFORE it is parsed, not after.
//
// Only the builtin negation folds. A user-defined `__neg` over a numeric type
// is a call the program asked for, and constant-folding it would silently skip
// a body that might do something else entirely.
func (l *lowerer) foldNegatedIntLiteral(exprID ast.ExprID, operand *Expr, ty types.TypeID, span source.Span) *Expr {
	if operand == nil || operand.Kind != ExprLiteral {
		return nil
	}
	lit, ok := operand.Data.(LiteralData)
	if !ok || lit.Kind != LiteralInt || lit.Text == "" {
		return nil
	}
	// FIXED-WIDTH signed integers only, and the boundary is load-bearing in both
	// directions. A width is what makes a magnitude unrepresentable, so it is
	// the only place the defect exists; and arbitrary-precision `int` carries
	// its literals as text into the bignum runtime, whose parser takes a
	// magnitude and rejects a leading sign outright — folding there turns a
	// working `let a: int = -5` into `invalid int literal "-5"`.
	if l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return nil
	}
	tt, found := l.semaRes.TypeInterner.Lookup(resolveAliasHIR(l.semaRes.TypeInterner, ty))
	if !found || tt.Kind != types.KindInt || tt.Width == types.WidthAny {
		return nil
	}
	if l.semaRes != nil && l.semaRes.MagicUnarySymbols != nil {
		if symID, found := l.semaRes.MagicUnarySymbols[exprID]; found && symID.IsValid() && !l.isBuiltinSymbol(symID) {
			return nil
		}
	}
	// The sign is flipped in the TEXT rather than by negating the parsed value,
	// which is what makes the int64 minimum reachable: `9223372036854775808`
	// does not parse, so there is nothing to negate, while `-9223372036854775808`
	// parses exactly. Flipping rather than prefixing also keeps `- -5` correct.
	text := lit.Text
	if text[0] == '-' {
		text = text[1:]
	} else {
		text = "-" + text
	}
	value, ok := numlit.ParseInt64(text)
	if !ok {
		return nil
	}
	return &Expr{
		Kind: ExprLiteral,
		Type: ty,
		Span: span,
		Data: LiteralData{Kind: LiteralInt, Text: text, IntValue: value},
	}
}

func (l *lowerer) isBuiltinSymbol(symID symbols.SymbolID) bool {
	if l == nil || l.symRes == nil || l.symRes.Table == nil || l.symRes.Table.Symbols == nil || !symID.IsValid() {
		return false
	}
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil || sym.Flags&symbols.SymbolFlagBuiltin == 0 {
		return false
	}
	if sym.Signature != nil && sym.Signature.HasBody {
		return false
	}
	return true
}

func (l *lowerer) magicCallExpr(span source.Span, ty types.TypeID, symID symbols.SymbolID, args []*Expr) *Expr {
	if symID.IsValid() {
		args = l.packVariadicArgs(symID, args, span)
		args = l.applyParamBorrow(symID, args)
	}
	callee := l.varRefForSymbol(symID, span)
	return &Expr{
		Kind: ExprCall,
		Type: ty,
		Span: span,
		Data: CallData{
			Callee:   callee,
			Args:     args,
			SymbolID: symID,
		},
	}
}

// calleeResultType reads what a callee actually hands back, for the synthetic
// calls whose surrounding expression is not the thing being called. An
// index-set assignment is the case that needs it: the expression is typed by
// what was assigned, while the `__index_set` it lowers to returns nothing, and
// giving the call the expression's type asks a later stage for a destination
// no callee ever writes.
func (l *lowerer) calleeResultType(symID symbols.SymbolID) (types.TypeID, bool) {
	if l == nil || !symID.IsValid() || l.symRes == nil || l.symRes.Table == nil {
		return types.NoTypeID, false
	}
	if l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return types.NoTypeID, false
	}
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil || sym.Type == types.NoTypeID {
		return types.NoTypeID, false
	}
	fnInfo, ok := l.semaRes.TypeInterner.FnInfo(sym.Type)
	if !ok || fnInfo.Result == types.NoTypeID {
		return types.NoTypeID, false
	}
	return fnInfo.Result, true
}

func (l *lowerer) boolType() types.TypeID {
	if l == nil || l.semaRes == nil || l.semaRes.TypeInterner == nil {
		return types.NoTypeID
	}
	return l.semaRes.TypeInterner.Builtins().Bool
}

func (l *lowerer) applyBoolMagic(exprID ast.ExprID, result *Expr) *Expr {
	if result == nil || l.semaRes == nil {
		return result
	}
	if l.semaRes.BoolSymbols != nil {
		if symID, ok := l.semaRes.BoolSymbols[exprID]; ok && symID.IsValid() {
			return l.magicCallExpr(result.Span, l.boolType(), symID, []*Expr{result})
		}
	}
	if l.semaRes.BoolBoundMethods != nil {
		if _, ok := l.semaRes.BoolBoundMethods[exprID]; ok {
			useID := l.semaRes.DeferredCallableUses[sema.DeferredUseRef{Expr: exprID, Kind: sema.DeferredBoolCall}]
			return l.boundBoolCallExpr(result, useID)
		}
	}
	return result
}

func (l *lowerer) boundBoolCallExpr(result *Expr, useID sema.DeferredUseID) *Expr {
	if result == nil {
		return nil
	}
	callee := &Expr{
		Kind: ExprFieldAccess,
		Type: types.NoTypeID,
		Span: result.Span,
		Data: FieldAccessData{
			Object:    result,
			FieldName: "__bool",
			FieldIdx:  -1,
		},
	}
	return &Expr{
		Kind: ExprCall,
		Type: l.boolType(),
		Span: result.Span,
		Data: CallData{
			Callee:        callee,
			DeferredUseID: useID,
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
	sym := l.symRes.Table.Symbols.Get(symID)
	if sym == nil {
		return types.NoTypeID
	}
	if sym.Decl.Item.IsValid() && l.semaRes != nil && l.semaRes.BindingTypes != nil && l.semaRes.ItemScopes != nil {
		fnItem, ok := l.builder.Items.Fn(sym.Decl.Item)
		if ok && fnItem != nil {
			paramIDs := l.builder.Items.GetFnParamIDs(fnItem)
			if variadicIndex < len(paramIDs) {
				param := l.builder.Items.FnParam(paramIDs[variadicIndex])
				if param != nil && param.Name != source.NoStringID {
					fnScope := l.semaRes.ItemScopes[sym.Decl.Item]
					symID = l.symbolInScope(fnScope, param.Name, symbols.SymbolParam)
					if symID.IsValid() {
						if ty := l.semaRes.BindingTypes[symID]; ty != types.NoTypeID {
							return ty
						}
					}
				}
			}
		}
	}
	if l.semaRes != nil && l.semaRes.TypeInterner != nil && sym.Type != types.NoTypeID {
		if fnInfo, ok := l.semaRes.TypeInterner.FnInfo(sym.Type); ok {
			if variadicIndex < len(fnInfo.Params) {
				return fnInfo.Params[variadicIndex]
			}
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
	//
	// One caller depends on the answer being ABSENT rather than merely
	// approximate: a cast's `TargetTy`. MIR decides from a cast's types whether
	// it converts anything at all, and sema decides the same thing from the
	// types it recorded; they must agree, or a value gets released with no
	// owner. MIR therefore asks with the cast's own sema type today. Anyone
	// implementing this must re-check that pair together — a second source of
	// truth for a cast's target is exactly how the two could start disagreeing.
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

func parseIntLiteral(s string) int64 {
	value, ok := numlit.ParseInt64(s)
	if !ok {
		return 0
	}
	return value
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
