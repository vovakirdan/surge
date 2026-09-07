package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) markArrayViewExpr(expr ast.ExprID) {
	if !expr.IsValid() || tc.arrayViewExprs == nil {
		return
	}
	tc.arrayViewExprs[expr] = struct{}{}
}

// markArrayViewBinding records what the LAST binding of this name established,
// and an assignment that binds something else withdraws it.
//
// This is the narrow fact, and it is narrow because of who reads it. Three
// rules do, and not one of them has anything standing behind it: the resize
// rule below refuses `push`/`pop`/`reserve` on a view, `mintsOwnedValue` decides
// whether an index expression's temporary is reclaimed here or left to the
// container that owns it, and `rangeCursorBaseOfExpr` goes SILENT on a view,
// which is how SEM3199 stops refusing a cursor that outlives its array. Two of
// those were measured going wrong when this fact was widened to "may be a view
// on some path": `let mut v: int[] = base[1..3]; v = [9, 9]; v.push(5);` stopped
// compiling with SEM3015 though `v` owns its buffer by then, and `let mut out:
// Item[] = src[[0..2]]; out = [Item{n=33}]; return out.__range();` lost SEM3199
// and segfaulted on the native lane, eight runs out of eight. So the fact these
// three read stays the one a reader can follow by eye: `v = [9, 9]` makes `v` an
// array of its own, and every rule here agrees.
//
// The crossing gate needs the OTHER fact -- may this name hold a window on ANY
// path -- and it has the runtime's view registry behind it, so it keeps its own
// never-withdrawn map next door (type_array_view_may.go). Two questions, two
// maps, told apart by whether a wrong answer is caught later.
func (tc *typeChecker) markArrayViewBinding(symID symbols.SymbolID, isView bool) {
	if !symID.IsValid() || tc.arrayViewBindings == nil {
		return
	}
	if isView {
		tc.arrayViewBindings[symID] = struct{}{}
		return
	}
	delete(tc.arrayViewBindings, symID)
}

// isArrayViewExpr asks about the VALUE an expression produces, not about the
// shape of its head: a slice expression is one, and so is a name whose last
// binding was one.
//
// It stays deliberately blind to two shapes the crossing gate does see -- an
// element read back out of a holder, and a choice whose arms are slices --
// because its readers act on a yes with no way back. `xs[1]` is a view only if
// position 1 holds one, which this checker cannot say about an array; answering
// yes anyway refused `xs[1].push(9)` on an `int[][]` holding a view at position
// 0, where element 1 owned its own buffer, and it puts the same guess in front
// of `mintsOwnedValue`, which asks what an index expression ALLOCATED and cannot
// be answered by a fact about the container. `mayBeArrayView` is where the
// doubtful yes belongs, because only the crossing gate can afford one.
func (tc *typeChecker) isArrayViewExpr(expr ast.ExprID) bool {
	if !expr.IsValid() {
		return false
	}
	expr = tc.unwrapArrayViewExpr(expr)
	if expr.IsValid() && tc.arrayViewExprs != nil {
		if _, ok := tc.arrayViewExprs[expr]; ok {
			return true
		}
	}
	symID := tc.symbolForExpr(expr)
	if symID.IsValid() {
		if _, ok := tc.arrayViewBindings[symID]; ok {
			return true
		}
	}
	return false
}

func (tc *typeChecker) unwrapArrayViewExpr(expr ast.ExprID) ast.ExprID {
	if !expr.IsValid() || tc.builder == nil || tc.builder.Exprs == nil {
		return expr
	}
	for {
		if group, ok := tc.builder.Exprs.Group(expr); ok && group != nil {
			expr = group.Inner
			continue
		}
		if unary, ok := tc.builder.Exprs.Unary(expr); ok && unary != nil {
			// `own v` names the same storage `v` does; so does `&v`. None of
			// them turns a window into an array of its own.
			if unary.Op == ast.ExprUnaryRef || unary.Op == ast.ExprUnaryRefMut || unary.Op == ast.ExprUnaryOwn {
				expr = unary.Operand
				continue
			}
		}
		break
	}
	return expr
}

func (tc *typeChecker) isArrayOrFixedType(id types.TypeID) bool {
	if id == types.NoTypeID {
		return false
	}
	base := tc.valueType(id)
	if base == types.NoTypeID {
		return false
	}
	if tc.isArrayType(base) {
		return true
	}
	_, _, ok := tc.arrayFixedInfo(base)
	return ok
}

func (tc *typeChecker) isArrayRangeIndex(container, index types.TypeID) bool {
	if container == types.NoTypeID || index == types.NoTypeID || tc.types == nil {
		return false
	}
	base := tc.valueType(container)
	if base == types.NoTypeID {
		return false
	}
	if _, ok := tc.arrayElemType(base); !ok {
		if _, _, ok := tc.arrayFixedInfo(base); !ok {
			return false
		}
	}
	payload, ok := tc.rangePayload(index)
	if !ok {
		return false
	}
	intType := tc.types.Builtins().Int
	return intType != types.NoTypeID && tc.sameType(payload, intType)
}

func (tc *typeChecker) reportArrayViewResize(span source.Span, op string) {
	if op == "" {
		tc.report(diag.SemaTypeMismatch, span, "array view is not resizable")
		return
	}
	tc.report(diag.SemaTypeMismatch, span, "array view is not resizable; %s requires an owned array", op)
}

func (tc *typeChecker) updateArrayViewBindingFromAssign(left, right ast.ExprID) {
	if !left.IsValid() || !right.IsValid() || tc.builder == nil || tc.builder.Exprs == nil {
		return
	}
	if _, ok := tc.builder.Exprs.Ident(left); !ok {
		return
	}
	symID := tc.symbolForExpr(left)
	if !symID.IsValid() {
		return
	}
	tc.markArrayViewBinding(symID, tc.isArrayViewExpr(right))
	tc.markArrayViewMayBinding(symID, tc.mayBeArrayView(right))
	tc.noteArrayViewHolding(symID, right)
}

func (tc *typeChecker) checkArrayViewResizeMethod(receiverExpr ast.ExprID, name string, receiverType types.TypeID, span source.Span) {
	if name != "push" && name != "pop" && name != "reserve" &&
		name != "append_string" && name != "append_bytes_view" &&
		name != "append_bytes_range" && name != "clear_keep_capacity" {
		return
	}
	if !tc.isArrayType(tc.valueType(receiverType)) {
		return
	}
	if !tc.isArrayViewExpr(receiverExpr) {
		return
	}
	opSpan := span
	if recvSpan := tc.exprSpan(receiverExpr); recvSpan != (source.Span{}) {
		opSpan = recvSpan
	}
	tc.reportArrayViewResize(opSpan, name)
}

func (tc *typeChecker) markArrayViewMethodCall(callID ast.ExprID, name string, receiverType types.TypeID, args []types.TypeID) {
	if callID == ast.NoExprID || !tc.isArrayOrFixedType(receiverType) {
		return
	}
	if name != "slice" && name != "__index" {
		return
	}
	if len(args) == 0 {
		return
	}
	payload, ok := tc.rangePayload(args[0])
	if !ok || tc.types == nil {
		return
	}
	intType := tc.types.Builtins().Int
	if intType == types.NoTypeID || !tc.sameType(payload, intType) {
		return
	}
	tc.markArrayViewExpr(callID)
}

func (tc *typeChecker) checkArrayViewResizeCall(name string, args []callArg, span source.Span) {
	switch name {
	case "rt_array_reserve", "rt_array_push", "rt_array_pop",
		"rt_array_append_raw_bytes", "rt_byte_array_append_range", "rt_byte_array_drop_prefix",
		"array_reserve", "array_push", "array_pop":
	default:
		return
	}
	if len(args) == 0 {
		return
	}
	if !tc.isArrayViewExpr(args[0].expr) {
		return
	}
	opSpan := span
	if argSpan := tc.exprSpan(args[0].expr); argSpan != (source.Span{}) {
		opSpan = argSpan
	}
	tc.reportArrayViewResize(opSpan, name)
}
