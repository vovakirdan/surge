package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) applyParamOwnership(param symbols.TypeKey, expr ast.ExprID, exprType types.TypeID, span source.Span) {
	if !expr.IsValid() || param == "" {
		return
	}
	paramStr := strings.TrimSpace(string(param))
	if tc.isTaskContainerType(exprType) && !strings.HasPrefix(paramStr, "&") {
		tc.reportTaskContainerEscape(expr, span)
		return
	}
	switch {
	case strings.HasPrefix(paramStr, "&mut "):
		if tc.isReferenceType(exprType) {
			tc.refuseMutRefHandOffOverView(expr, exprType, span)
			return
		}
		tc.handleBorrow(expr, span, ast.ExprUnaryRefMut, expr)
	case strings.HasPrefix(paramStr, "&"):
		if tc.isReferenceType(exprType) {
			tc.noteSharedRefArg(expr, exprType, span)
			return
		}
		if tc.canMaterializeForRefString(expr, tc.typeFromKey(param)) {
			return
		}
		tc.handleBorrow(expr, span, ast.ExprUnaryRef, expr)
	default:
		tc.observeMove(expr, span)
	}
}

func (tc *typeChecker) isReferenceType(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(id))
	return ok && tt.Kind == types.KindReference
}

func (tc *typeChecker) applyMethodReceiverOwnership(symID symbols.SymbolID, recvExpr ast.ExprID, recvType types.TypeID) {
	if !symID.IsValid() || !recvExpr.IsValid() {
		return
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Signature == nil || !sym.Signature.HasSelf || len(sym.Signature.Params) == 0 {
		return
	}
	tc.applyParamOwnership(sym.Signature.Params[0], recvExpr, recvType, tc.exprSpan(recvExpr))
}

func (tc *typeChecker) applyCallArgsOwnership(symID symbols.SymbolID, args []ast.CallArg, argTypes []types.TypeID) bool {
	if !symID.IsValid() {
		return false
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Signature == nil || len(sym.Signature.Params) == 0 {
		return false
	}
	defer tc.endMutArgs(tc.beginMutArgs())
	ordered := make([]argumentInOrder, 0, len(args))
	for i, arg := range args {
		if i >= len(sym.Signature.Params) || i >= len(argTypes) {
			break
		}
		tc.applyParamOwnership(sym.Signature.Params[i], arg.Value, argTypes[i], tc.exprSpan(arg.Value))
		ordered = append(ordered, argumentInOrder{expr: arg.Value, ty: argTypes[i], param: sym.Signature.Params[i]})
	}
	tc.refuseLaterArgumentOverEarlierElement(ordered)
	return true
}

// applyMethodArgsOwnership applies a method's explicit arguments. The receiver
// is evaluated first and handed to `self`, so it heads the evaluation order the
// arguments are checked in: `r[0].foo(appr(r))` hands the callee an element
// reference the later argument may free.
func (tc *typeChecker) applyMethodArgsOwnership(sym *symbols.Symbol, recvExpr ast.ExprID, recvType types.TypeID, args []ast.CallArg, argTypes []types.TypeID) bool {
	if sym == nil || sym.Signature == nil {
		return false
	}
	sig := sym.Signature
	offset := 0
	ordered := make([]argumentInOrder, 0, len(args)+1)
	if sig.HasSelf {
		offset = 1
		if recvExpr.IsValid() && len(sig.Params) > 0 {
			ordered = append(ordered, argumentInOrder{expr: recvExpr, ty: recvType, param: sig.Params[0]})
		}
	}
	for i, arg := range args {
		paramIndex := i + offset
		if i >= len(argTypes) || paramIndex >= len(sig.Params) {
			break
		}
		tc.applyParamOwnership(sig.Params[paramIndex], arg.Value, argTypes[i], tc.exprSpan(arg.Value))
		ordered = append(ordered, argumentInOrder{expr: arg.Value, ty: argTypes[i], param: sig.Params[paramIndex]})
	}
	tc.refuseLaterArgumentOverEarlierElement(ordered)
	return true
}
