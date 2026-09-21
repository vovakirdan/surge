package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/types"
)

const (
	// A walk over elements that can hold a borrow stays outside the admitted language until
	// the analysis models two names reaching one buffer (docs/KNOWN_LIMITATIONS.md, Arrays).
	returnOriginForInBorrowElement = "for-in over elements that can hold a borrow needs the buffer-alias model"
	returnOriginForInProtocol      = "for-in iterable needs its iterator protocol transfer"
	returnOriginForInRangeCall     = "for-in iterator needs its selected __range call transfer"
	returnOriginForInConversion    = "for-in binding requires its element conversion transfer"
)

// forIn is the loop lowering builds (hir/normalize_for.go; the numeric path is the same loop
// over int copies): the iterable once, then a header every step may leave, one element copied
// into the binding per step, and the body. The binding lives in the statement's own scope.
func (b *returnOriginBody) forIn(id ast.StmtID, env returnOriginEnv, targets returnOriginTargets) (returnOriginFlow, error) {
	u := b.function.unit
	node, data, scope := u.Builder.Stmts.Get(id), u.Builder.Stmts.ForIn(id), u.stmtScopes[id]
	if data == nil || !scope.IsValid() {
		return returnOriginFlow{}, fmt.Errorf("return origins: for-in at %v has no owning scope", node.Span)
	}
	targets.scope = scope
	// The iterable runs before the loop exists, so an exit inside it still targets the enclosing loop.
	iterable, err := b.expr(data.Iterable, env, targets)
	if err != nil {
		return returnOriginFlow{}, err
	}
	targets.loop = scope
	flow, err := iterable.flow.then(func(entry returnOriginEnv) (returnOriginFlow, error) {
		element, sym := b.forInElement(id, data, node.Span), u.stmtSymbols[id]
		return solveReturnOriginLoop(b.analyzer.ctx, entry, scope, func(header returnOriginEnv) (returnOriginLoopStep, error) {
			body := header.clone()
			if sym.IsValid() {
				body = body.assign(sym, u.Symbols.Table.Symbols.Get(sym).Scope, element)
			}
			out, stepErr := b.stmt(data.Body, body, targets)
			return returnOriginLoopStep{done: header, body: out}, stepErr
		})
	})
	if err != nil {
		return returnOriginFlow{}, err
	}
	return b.closeFlow(flow, scope, node.Span), nil
}

// forInElement is the value one step binds: a copy of the walked element, which keeps nothing
// of the iterable when the element holds no borrow and can keep no storage loan. Every other
// element is refused by name; a body's own type parameter is admitted under NoBorrowedState,
// which a reference refutes and an array or cursor leaves unsupported at each instance.
func (b *returnOriginBody) forInElement(id ast.StmtID, data *ast.ForInStmt, span source.Span) returnOriginValue {
	elem, reason := b.forInElementType(id, data, span)
	switch {
	case reason != "":
		b.pending(span, reason)
	case b.loanElement(returnOriginIndexType{element: elem}):
		b.pending(span, returnOriginCursorLoanElement)
	case returnOriginView(b.function).shape(elem) == returnOriginRefFree, b.freeTemplateElement(elem, span):
		return returnOriginValueOf()
	default:
		b.pending(span, returnOriginForInBorrowElement)
	}
	return returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
}

// forInElementType follows the checker's protocol (type_checker_iterators.go): a Range, then a
// canonical array, each reached directly or through one reference, then a recorded `__range`.
// The binding must have the element's own type, because lowering binds it without a conversion.
func (b *returnOriginBody) forInElementType(id ast.StmtID, data *ast.ForInStmt, span source.Span) (elem types.TypeID, refusal string) {
	u := b.function.unit
	in, iterable := u.Sema.TypeInterner, u.Sema.ExprTypes[data.Iterable]
	if selected, recorded := u.Sema.RangeSymbols[data.Iterable]; recorded {
		fn, reason := b.analyzer.selectedCallableFunction(u, selected)
		if reason != "" || !returnOriginForInRangeBody(fn, u.Sema.RangeTypes[data.Iterable]) {
			return types.NoTypeID, returnOriginForInRangeCall
		}
		b.inheritRequirements(fn, returnOriginView(fn), span)
		iterable = fn.info.Result
	}
	base, typ, ok := returnOriginIndexResolve(in, iterable)
	if ok && typ.Kind == types.KindReference {
		base = typ.Elem
	}
	var found bool
	elem, found = returnOriginRangeElement(b.function, base)
	if !found {
		c, canonical := returnOriginContainer(in, iterable)
		if !canonical {
			return types.NoTypeID, returnOriginForInProtocol
		}
		elem = c.element
	}
	if sym := u.stmtSymbols[id]; sym.IsValid() && returnOriginResolveAlias(in, u.Sema.BindingTypes[sym]) != returnOriginResolveAlias(in, elem) {
		return types.NoTypeID, returnOriginForInConversion
	}
	return elem, ""
}

// returnOriginForInRangeBody admits a selected `__range` the loop need not run as a call: a plain
// source body over one shared receiver, with no cell or writable container formal it could
// write through, whose result is the cursor type the checker recorded. The cursor reaches no
// binding, and the admitted element keeps nothing of it.
func returnOriginForInRangeBody(fn *returnOriginFunction, cursor types.TypeID) bool {
	if fn == nil || fn.candidate == nil || fn.info == nil || fn.item == nil || !fn.item.Body.IsValid() || len(fn.info.Params) != 1 {
		return false
	}
	kind, reference := returnOriginFormalBorrowKind(fn.unit.Sema.TypeInterner, fn.info.Params[0])
	return fn.info.Result == cursor && len(fn.candidate.TemplateParams) == 0 && fn.candidate.HasSelf && !fn.candidate.Async &&
		reference && kind == BorrowShared && len(fn.cellSlots) == 0 && len(fn.mutableBackingSlots) == 0
}
