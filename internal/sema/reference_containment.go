package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// References carry no lifetime of their own: they are valid only while the
// borrowed value's scope is alive, and the borrow checker tracks them only in
// local bindings. A reference stored inside an aggregate (struct field, tag
// payload, tuple/array/map element) escapes that tracking — the aggregate can
// outlive the borrowed value and dangle. Until aggregates can carry loans,
// aggregates hold owned values only.
//
// rejectRefInAggregate reports the violation and returns true when t is a
// reference type; container names the aggregate position for the message.
func (tc *typeChecker) rejectRefInAggregate(t types.TypeID, span source.Span, container string) bool {
	if t == types.NoTypeID || tc.types == nil {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(t))
	if !ok || tt.Kind != types.KindReference {
		return false
	}
	tc.reportRefInAggregate(tc.typeLabel(t), tc.typeLabel(tt.Elem), span, container)
	return true
}

// checkBorrowEscapeOnReturn rejects returning a reference whose loan provably
// roots in storage that dies with this call frame: a local binding, or an
// owned (by-value) parameter. References with unknown provenance — above all
// a `&T` parameter returned as `&T` (`fn ident(x: &T) -> &T`) — stay allowed;
// the existing call-result loan propagation covers those.
func (tc *typeChecker) checkBorrowEscapeOnReturn(expr ast.ExprID, ty types.TypeID, span source.Span) {
	if tc.borrow == nil || !tc.isReferenceType(ty) {
		return
	}
	inner := tc.unwrapGroupExpr(expr)
	bid := tc.borrow.ExprBorrow(inner)
	if bid == NoBorrowID {
		if symID := tc.symbolForExpr(inner); symID.IsValid() && tc.bindingBorrow != nil {
			bid = tc.bindingBorrow[symID]
		}
	}
	if bid == NoBorrowID {
		bid = tc.inheritedBorrowForExpr(inner)
	}
	if bid == NoBorrowID {
		return
	}
	info := tc.borrow.Info(bid)
	if info == nil || !info.Place.Base.IsValid() {
		return
	}
	sym := tc.symbolFromID(info.Place.Base)
	if sym == nil {
		return
	}
	var storage string
	switch sym.Kind {
	case symbols.SymbolLet:
		storage = "local"
	case symbols.SymbolParam:
		// A reference parameter's target lives in the caller; only an
		// owned (by-value) parameter dies with this frame.
		if tc.isReferenceType(tc.bindingType(info.Place.Base)) {
			return
		}
		storage = "by-value parameter"
	default:
		return
	}
	name := tc.lookupName(sym.Name)
	if tc.reporter == nil {
		tc.report(diag.SemaBorrowEscapesReturn, span,
			"cannot return a borrow of %s '%s': it is freed when the function returns", storage, name)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaBorrowEscapesReturn, span,
		fmt.Sprintf("cannot return a borrow of %s '%s': it is freed when the function returns", storage, name))
	if b == nil {
		return
	}
	if sym.Span != (source.Span{}) {
		b.WithNote(sym.Span, fmt.Sprintf("'%s' lives only inside this function", name))
	}
	b.WithNote(span, fmt.Sprintf(
		"hint: return the value itself to move it out, or a copy: %s.__clone()", name))
	b.Emit()
}

// checkTaskBorrowEscapeOnReturn rejects returning a task handle whose spawn
// borrowed frame-local storage: the caller can await the task after this
// frame's locals are freed, so the task would read dangling memory (the VM
// panics VM3301; native reads freed memory silently). Awaiting the task in
// this function, or spawning with an owned value/clone, stays legal.
func (tc *typeChecker) checkTaskBorrowEscapeOnReturn(expr ast.ExprID, ty types.TypeID, span source.Span) {
	if tc.taskTracker == nil || tc.borrow == nil {
		return
	}
	if !tc.isTaskType(ty) && !tc.isFarTaskType(ty) {
		return
	}
	inner := tc.unwrapGroupExpr(expr)
	node := tc.builder.Exprs.Get(inner)
	if node == nil {
		return
	}
	spawnExpr := ast.NoExprID
	if id, ok := tc.taskTracker.exprTasks[inner]; ok && int(id) < len(tc.taskTracker.tasks) {
		spawnExpr = tc.taskTracker.tasks[id].SpawnExpr
	}
	if !spawnExpr.IsValid() && node.Kind == ast.ExprIdent {
		if symID := tc.symbolForExpr(inner); symID.IsValid() {
			if id, ok := tc.taskTracker.bindingTasks[symID]; ok && int(id) < len(tc.taskTracker.tasks) {
				spawnExpr = tc.taskTracker.tasks[id].SpawnExpr
			}
		}
	}
	if !spawnExpr.IsValid() {
		switch node.Kind {
		case ast.ExprSpawn, ast.ExprTask, ast.ExprCall:
			spawnExpr = inner
		default:
			return
		}
	}
	payload := spawnExpr
	if spawnNode := tc.builder.Exprs.Get(spawnExpr); spawnNode != nil {
		switch spawnNode.Kind {
		case ast.ExprSpawn:
			if d, ok := tc.builder.Exprs.Spawn(spawnExpr); ok && d != nil {
				payload = d.Value
			}
		case ast.ExprTask:
			if d, ok := tc.builder.Exprs.Task(spawnExpr); ok && d != nil {
				payload = d.Value
			}
		}
	}
	call, ok := tc.builder.Exprs.Call(tc.unwrapGroupExpr(payload))
	if !ok || call == nil {
		return
	}
	for _, arg := range call.Args {
		base := tc.borrowedFrameLocalBase(arg.Value)
		if !base.IsValid() {
			continue
		}
		sym := tc.symbolFromID(base)
		if sym == nil {
			continue
		}
		name := tc.lookupName(sym.Name)
		argSpan := tc.exprSpan(arg.Value)
		if tc.reporter == nil {
			tc.report(diag.SemaBorrowEscapesReturn, span,
				"cannot return this task: it borrows '%s', which is freed when the function returns while the task may still be running", name)
			return
		}
		b := diag.ReportError(tc.reporter, diag.SemaBorrowEscapesReturn, span,
			fmt.Sprintf("cannot return this task: it borrows '%s', which is freed when the function returns while the task may still be running", name))
		if b != nil {
			if argSpan != (source.Span{}) {
				b.WithNote(argSpan, fmt.Sprintf("the task borrowed '%s' here", name))
			}
			b.WithNote(span, fmt.Sprintf(
				"hint: await the task inside this function, or spawn it with an owned value: pass %s itself or %s.__clone()", name, name))
			b.Emit()
		}
		return
	}
}

// borrowedFrameLocalBase classifies an argument expression: when it lends
// frame-local storage — `&local` / `&mut local` (or a projection of one), or
// an ident bound to such a borrow — it returns the borrowed base symbol.
// Borrows of reference parameters (the caller's storage) return invalid.
func (tc *typeChecker) borrowedFrameLocalBase(argExpr ast.ExprID) symbols.SymbolID {
	argExpr = tc.unwrapGroupExpr(argExpr)
	node := tc.builder.Exprs.Get(argExpr)
	if node == nil {
		return symbols.NoSymbolID
	}
	var operand ast.ExprID
	switch node.Kind {
	case ast.ExprUnary:
		unary := tc.builder.Exprs.Unaries.Get(uint32(node.Payload))
		if unary == nil || (unary.Op != ast.ExprUnaryRef && unary.Op != ast.ExprUnaryRefMut) {
			return symbols.NoSymbolID
		}
		operand = unary.Operand
	case ast.ExprIdent:
		// An ident argument lends frame storage only when it is itself a
		// reference binding whose loan the table still roots in this frame.
		symID := tc.symbolForExpr(argExpr)
		if !symID.IsValid() || tc.bindingBorrow == nil || tc.borrow == nil {
			return symbols.NoSymbolID
		}
		bid := tc.bindingBorrow[symID]
		if bid == NoBorrowID {
			return symbols.NoSymbolID
		}
		info := tc.borrow.Info(bid)
		if info == nil || !info.Place.Base.IsValid() {
			return symbols.NoSymbolID
		}
		if tc.isFrameLocalStorage(info.Place.Base) {
			return info.Place.Base
		}
		return symbols.NoSymbolID
	default:
		return symbols.NoSymbolID
	}
	desc, ok := tc.resolvePlace(tc.unwrapGroupExpr(operand))
	if !ok || !desc.Base.IsValid() {
		return symbols.NoSymbolID
	}
	if tc.isFrameLocalStorage(desc.Base) {
		return desc.Base
	}
	return symbols.NoSymbolID
}

// isFrameLocalStorage reports whether the symbol's storage dies with the
// current call frame: a local binding, or an owned (by-value) parameter.
func (tc *typeChecker) isFrameLocalStorage(base symbols.SymbolID) bool {
	sym := tc.symbolFromID(base)
	if sym == nil {
		return false
	}
	switch sym.Kind {
	case symbols.SymbolLet:
		return true
	case symbols.SymbolParam:
		return !tc.isReferenceType(tc.bindingType(base))
	default:
		return false
	}
}

func (tc *typeChecker) reportRefInAggregate(label, elemLabel string, span source.Span, container string) {
	article := "a"
	if container != "" {
		switch container[0] {
		case 'a', 'e', 'i', 'o', 'u':
			article = "an"
		}
	}
	if tc.reporter == nil {
		tc.report(diag.SemaRefInAggregate, span, "%s %s cannot hold a reference (%s)", article, container, label)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaRefInAggregate, span,
		fmt.Sprintf("%s %s cannot hold a reference (%s)", article, container, label))
	if b == nil {
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"a reference is only valid while the borrowed value's scope is alive; a %s can outlive that scope and dangle",
		container))
	b.WithNote(span, fmt.Sprintf(
		"hint: store an owned %s instead — move the value in, or copy it with .__clone(); to lend a value to a function, pass %s as a parameter",
		elemLabel, label))
	b.Emit()
}
