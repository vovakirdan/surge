package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// What a task holds through a value it was lent, where the loan walk names no loan (RV2-DEBT-365,
// R-b(call) and R-d's call form).
//
// noteReachingLoans asks the borrow table what a reaching position of a Task-valued call carries,
// and the table answers for a borrow made in the call, a reference binding's loan, and a call with
// one source. Three routes carry a reference past it. A PARAMETER: in `fn park(x: &int, out: &mut
// Task<int>) { *out = worker(x); }` the task holds what the caller lent, and the caller's call is
// not Task-valued, so no check the caller runs sees the task. A `let` whose value carries a
// reference its type does not spell as one -- `let o = m.get_ref(&k); let t = w(o);`, a
// `BytesView`, a struct that holds an `Option<&V>` -- because a binding carries a loan only when its
// own type is a reference (bindingBorrowForExpr). And an array a function was handed by a
// parameter, which may be a window into the caller's frame: `fn park2(xs: int64[], out: &mut
// Task<int64>) { *out = first(xs); }`.
//
// A `let` is answered by what was written into it: the value it was bound to and every value
// assigned to it are opened the way a lender written in the call is, and what they borrowed is
// pinned. A compare arm's binding is answered by the compare's subject. A parameter is answered by
// a FLOW-ONLY pin on the parameter itself. The task may be joined, drained, or handed back by
// `return`, where the caller's own call pins what it lent; every other way out -- the function's
// end, a return of another value, a loop's back edge, a body's end -- is refused by the edge that
// already refuses any live pin there. The pin is flow-only because what the parameter refers to is
// the caller's: the frame-local test of a returned task (taskPinnedFrameLocal) and the record of
// what a returned task was built from (callBuildsTaskFromOwnedValues) do not count it, and it
// promotes no storage. A mutable `let` is pinned the same way besides, because an assignment the
// walk has not reached -- written after the call in a loop -- can hand the next task a value no
// source named; the pin makes that assignment, and the back edge, answer for it. A `let` nothing
// was written into (a tuple pattern's binding, a loop variable) is pinned as the frame storage it
// is: what it points at is unknown.

// lentValueState is what the task check knows of values lent through bindings.
type lentValueState struct {
	// sources are the values written into each binding whose type can carry a reference.
	sources map[symbols.SymbolID][]ast.ExprID
	// flowOnly are the pins openLentValuePins opened.
	flowOnly map[taskBorrowPinKey]struct{}
	// spawnLent are the flow-only pins the spawn being typed collects for its task
	// (task_spawn_lent.go).
	spawnLent []spawnBorrowCapture
}

// lentValueCarriesReference reports that a value of this type can carry a reference: a reference,
// a union that hands one out, a borrowed view, an aggregate that holds any of them -- the shape
// the return-origin analysis reads, where carriedReferenceType walks unions only -- or a generic
// parameter, which can be instantiated with any of them.
func (tc *typeChecker) lentValueCarriesReference(ty types.TypeID) bool {
	if ty == types.NoTypeID || tc.types == nil {
		return false
	}
	if tt, found := tc.types.Lookup(tc.resolveAlias(tc.ownStripped(ty))); found && tt.Kind == types.KindGenericParam {
		return true
	}
	return returnOriginTypeShape(tc.types, ty, nil) == returnOriginCarriesRef
}

// noteCarriedValueSource remembers a value written into a binding whose type can carry a
// reference: the value it is bound to, and every value assigned to it.
func (tc *typeChecker) noteCarriedValueSource(symID symbols.SymbolID, value ast.ExprID) {
	if !symID.IsValid() || !value.IsValid() || !tc.lentValueCarriesReference(tc.bindingType(symID)) {
		return
	}
	if tc.lentValues.sources == nil {
		tc.lentValues.sources = make(map[symbols.SymbolID][]ast.ExprID)
	}
	tc.lentValues.sources[symID] = append(tc.lentValues.sources[symID], value)
}

// noteCarriedArmSources answers a compare arm's bindings by the compare's subject: a binding that
// carries a reference took it out of what the subject carries.
func (tc *typeChecker) noteCarriedArmSources(bindings []symbols.SymbolID, subject ast.ExprID) {
	for _, symID := range bindings {
		tc.noteCarriedValueSource(symID, subject)
	}
}

// lentValueCaptures answers, for a call that answers a Task, what its reaching positions carry
// that the loan walk named no loan for. A loan found by opening what was written into a `let` is
// noted into the call's own collection, as noteCarriedLoans notes one; the flow-only pins are
// returned.
func (tc *typeChecker) lentValueCaptures(ty types.TypeID, call *ast.ExprCallData) []spawnBorrowCapture {
	if ty == types.NoTypeID || !tc.isTaskType(ty) || tc.result == nil {
		return nil
	}
	var pins []spawnBorrowCapture
	seen := make(map[symbols.SymbolID]struct{})
	for _, lender := range tc.reachingPositions(call) {
		if tc.lentValueCarriesReference(tc.result.ExprTypes[lender]) {
			pins = tc.collectLentValues(lender, tc.exprSpan(lender), seen, pins)
		}
	}
	return pins
}

// collectLentValues opens one reference-carrying expression down to what it carries: a loan the
// table names, the parts it is made of, the place an `own` refers into, or a binding.
func (tc *typeChecker) collectLentValues(expr ast.ExprID, span source.Span, seen map[symbols.SymbolID]struct{}, pins []spawnBorrowCapture) []spawnBorrowCapture {
	if bid := tc.inheritedBorrowForExpr(expr); bid != NoBorrowID {
		tc.noteSpawnBorrowCapture(bid, span)
		return pins
	}
	inner := tc.unwrapGroupExpr(expr)
	if call, isCall := tc.builder.Exprs.Call(inner); isCall && call != nil {
		pins = tc.collectReferenceArguments(inner, call, span, seen, pins)
	}
	if parts := tc.lentValueParts(inner); len(parts) != 0 {
		for _, part := range parts {
			pins = tc.collectLentValues(part, span, seen, pins)
		}
		return pins
	}
	if unary, isUnary := tc.builder.Exprs.Unary(inner); isUnary && unary != nil {
		switch unary.Op {
		case ast.ExprUnaryDeref:
			return tc.collectLentValues(unary.Operand, span, seen, pins)
		case ast.ExprUnaryOwn, ast.ExprUnaryRef, ast.ExprUnaryRefMut:
			return tc.collectOwnedPlace(unary.Operand, span, seen, pins)
		}
		return pins
	}
	if node := tc.builder.Exprs.Get(inner); node != nil && node.Kind == ast.ExprIdent {
		return tc.collectLentBinding(tc.symbolForExpr(inner), span, seen, pins)
	}
	return pins
}

// collectOwnedPlace answers `own <place>`, and a `&<place>` whose borrow record is gone: a value that
// already carries a reference hands on that reference; a place of owned storage is referred into, and
// it is pinned as a borrow of that storage would be.
func (tc *typeChecker) collectOwnedPlace(operand ast.ExprID, span source.Span, seen map[symbols.SymbolID]struct{}, pins []spawnBorrowCapture) []spawnBorrowCapture {
	if tc.lentValueCarriesReference(tc.result.ExprTypes[operand]) {
		return tc.collectLentValues(operand, span, seen, pins)
	}
	desc, placed := tc.resolvePlace(tc.unwrapGroupExpr(operand))
	if !placed || !desc.Base.IsValid() {
		return pins
	}
	if tc.lentValueCarriesReference(tc.bindingType(desc.Base)) {
		return tc.collectLentBinding(desc.Base, span, seen, pins)
	}
	tc.spawnBorrowCaptures = append(tc.spawnBorrowCaptures, spawnBorrowCapture{Place: Place{Base: desc.Base}, Kind: BorrowShared, Span: span})
	return pins
}

// collectReferenceArguments answers a call whose result carries a reference by what the call took by
// reference. The borrow a call makes for a `&` formal is ended at the call when the result's type does
// not SPELL a reference -- dropImplicitBorrowForRefParam asks carriedReferenceType, which walks unions
// only -- and ending it deletes the table's record (BorrowTable.DropBorrow deletes exprBorrow), so a
// `BytesView` out of `s.bytes()` names no loan and its receiver `s` is a plain string. Each actual
// passed to a `&` formal is a place the result can point into.
func (tc *typeChecker) collectReferenceArguments(id ast.ExprID, call *ast.ExprCallData, span source.Span, seen map[symbols.SymbolID]struct{}, pins []spawnBorrowCapture) []spawnBorrowCapture {
	sym := tc.symbolFromID(tc.symbolForExpr(id))
	if sym == nil || sym.Signature == nil {
		return pins
	}
	params := sym.Signature.Params
	offset := 0
	if sym.Signature.HasSelf {
		offset = 1
		if member, isMember := tc.builder.Exprs.Member(call.Target); isMember && member != nil && len(params) > 0 && referenceFormal(params[0]) {
			pins = tc.collectOwnedPlace(member.Target, span, seen, pins)
		}
	}
	for i, arg := range call.Args {
		if i+offset < len(params) && referenceFormal(params[i+offset]) {
			pins = tc.collectOwnedPlace(arg.Value, span, seen, pins)
		}
	}
	return pins
}

// referenceFormal reports that a formal takes its actual by reference.
func referenceFormal(param symbols.TypeKey) bool {
	return strings.HasPrefix(strings.TrimSpace(string(param)), "&")
}

// collectLentBinding answers a binding that carries a reference with no loan the table names. A
// task handle is not looked into: the task tracker owns its story, as it does for a borrow of one.
func (tc *typeChecker) collectLentBinding(symID symbols.SymbolID, span source.Span, seen map[symbols.SymbolID]struct{}, pins []spawnBorrowCapture) []spawnBorrowCapture {
	ty := tc.bindingType(symID)
	sym := tc.symbolFromID(symID)
	if sym == nil || !tc.lentValueCarriesReference(ty) || tc.isTaskType(tc.valueType(ty)) {
		return pins
	}
	if _, dup := seen[symID]; dup {
		return pins
	}
	seen[symID] = struct{}{}
	pin := spawnBorrowCapture{Place: Place{Base: symID}, Kind: BorrowShared, Span: span}
	if carried, carries := tc.carriedReferenceType(tc.ownStripped(ty)); carries && tc.isMutRefType(carried) {
		pin.Kind = BorrowMut
	}
	switch sym.Kind {
	case symbols.SymbolParam:
		pins = append(pins, pin)
	case symbols.SymbolLet:
		written := tc.lentValues.sources[symID]
		for _, value := range written {
			pins = tc.collectLentValues(value, span, seen, pins)
		}
		if len(written) == 0 {
			tc.spawnBorrowCaptures = append(tc.spawnBorrowCaptures, pin)
		} else if sym.Flags&symbols.SymbolFlagMutable != 0 {
			pins = append(pins, pin)
		}
	}
	return pins
}

// lentValueParts lists what a reference-carrying value is made of when no loan names it: what
// carriedLoanSources opens (a block's results, a call's reaching positions, a choice's arms), a
// literal's parts, and the value a projection reads out of.
func (tc *typeChecker) lentValueParts(expr ast.ExprID) []ast.ExprID {
	if sources := tc.carriedLoanSources(expr); len(sources) != 0 {
		return sources
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return nil
	}
	switch node.Kind {
	case ast.ExprStruct:
		if data, found := tc.builder.Exprs.Struct(expr); found && data != nil {
			fields := make([]ast.ExprID, 0, len(data.Fields))
			for _, field := range data.Fields {
				fields = append(fields, field.Value)
			}
			return fields
		}
	case ast.ExprTuple:
		if data, found := tc.builder.Exprs.Tuple(expr); found && data != nil {
			return data.Elements
		}
	case ast.ExprArray:
		if data, found := tc.builder.Exprs.Array(expr); found && data != nil {
			return data.Elements
		}
	case ast.ExprMember:
		if data, found := tc.builder.Exprs.Member(expr); found && data != nil {
			return []ast.ExprID{data.Target}
		}
	case ast.ExprIndex:
		if data, found := tc.builder.Exprs.Index(expr); found && data != nil {
			return []ast.ExprID{data.Target}
		}
	case ast.ExprTupleIndex:
		if data, found := tc.builder.Exprs.TupleIndex(expr); found && data != nil {
			return []ast.ExprID{data.Target}
		}
	}
	return nil
}

// windowParameterCaptures answers an array the checker holds no provenance for, reaching a task in
// a function whose parameter can hand it a window or a cursor into the caller's frame: the task is
// pinned, flow-only, on that parameter (R-b(call)'s window half). A window or a cursor the checker
// traced is reachingFixedViews's.
func (tc *typeChecker) windowParameterCaptures(call *ast.ExprCallData) []spawnBorrowCapture {
	if tc.result == nil {
		return nil
	}
	param := tc.windowParameter()
	if !param.IsValid() {
		return nil
	}
	var pins []spawnBorrowCapture
	for _, lender := range tc.reachingPositions(call) {
		if tc.fixedViewEscapeBase(lender).IsValid() || tc.rangeCursorEscapeBase(lender).IsValid() {
			continue
		}
		if node := tc.builder.Exprs.Get(tc.unwrapGroupExpr(lender)); node != nil && node.Kind == ast.ExprRangeLit {
			continue
		}
		if tc.arrayHoldings(tc.result.ExprTypes[lender], true, nil)&(holdsDynamicArray|holdsRange) != 0 {
			pins = append(pins, spawnBorrowCapture{Place: Place{Base: param}, Kind: BorrowShared, Span: tc.exprSpan(lender)})
		}
	}
	return pins
}

// windowParameter names a parameter through which the caller can hand this function a window or a
// cursor into its own frame that no check of the caller's sees: a reference that reaches an array,
// or -- in a function whose own call does not answer a Task, so that the caller's call is no task
// position -- a value taken by value that holds a dynamic array or a range. Where the call does
// answer a Task, the caller's call is one, and untracedArrayCaptures judges the argument there.
func (tc *typeChecker) windowParameter() symbols.SymbolID {
	byValue := !tc.currentCallableAnswersTask()
	for _, param := range tc.currentFnParams() {
		ty := tc.bindingType(param)
		if tc.isReferenceType(ty) && tc.arrayHoldings(ty, true, nil) != 0 {
			return param
		}
		if byValue && !tc.isReferenceType(ty) && tc.arrayHoldings(ty, false, nil)&(holdsDynamicArray|holdsRange) != 0 {
			return param
		}
	}
	return symbols.NoSymbolID
}

// currentCallableAnswersTask reports that a call of the callable being walked answers a Task: an
// `async fn`, or a function declared to return one.
func (tc *typeChecker) currentCallableAnswersTask() bool {
	sym := tc.symbolFromID(tc.currentFnSym())
	if sym == nil || tc.types == nil {
		return false
	}
	info, found := tc.types.FnInfo(sym.Type)
	return found && info != nil && tc.isTaskType(info.Result)
}

// openLentValuePins opens the flow-only pins of one task. A place the task already holds by a
// real borrow keeps that pin: it is the stronger claim.
func (tc *typeChecker) openLentValuePins(task uint32, lent []spawnBorrowCapture) {
	if task == 0 || len(lent) == 0 {
		return
	}
	if tc.taskBorrowPins == nil {
		tc.taskBorrowPins = make(map[taskBorrowPinKey]taskBorrowPin)
	}
	if tc.lentValues.flowOnly == nil {
		tc.lentValues.flowOnly = make(map[taskBorrowPinKey]struct{})
	}
	for _, capture := range lent {
		key := taskBorrowPinKey{Task: task, Place: capture.Place}
		if _, held := tc.taskBorrowPins[key]; held {
			continue
		}
		tc.taskBorrowPins[key] = taskBorrowPin{Kind: capture.Kind, Span: capture.Span, LoopDepth: tc.currentLoopDepth()}
		tc.lentValues.flowOnly[key] = struct{}{}
	}
}

// isFlowOnlyPin reports that a pin stands for what a task was lent, not for storage of this frame
// it holds.
func (tc *typeChecker) isFlowOnlyPin(key taskBorrowPinKey) bool {
	_, flowOnly := tc.lentValues.flowOnly[key]
	return flowOnly
}

// callTaskCapturedAnything reports that the call answered a task with an identity of its own for
// more than flow-only pins: it captured a borrow, a loan or a window of this frame, or its pins
// are gone. A task with flow-only pins alone is read by its callee's formals, as one with no
// identity is.
func (tc *typeChecker) callTaskCapturedAnything(call ast.ExprID) bool {
	if tc.taskTracker == nil {
		return false
	}
	task := tc.taskTracker.TaskIDForExpr(call)
	if task == 0 {
		return false
	}
	flowOnly := false
	for key := range tc.taskBorrowPins {
		if key.Task != task {
			continue
		}
		if !tc.isFlowOnlyPin(key) {
			return true
		}
		flowOnly = true
	}
	return !flowOnly
}
