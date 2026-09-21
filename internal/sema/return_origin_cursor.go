package sema

import (
	"slices"

	"surge/internal/source"
	"surge/internal/types"
)

// returnOriginCursorLoanElement refuses a stepped element that is itself an array
// or a cursor: the copied handle keeps the storage loans it was stored with.
const returnOriginCursorLoanElement = "cursor element that can hold storage loans needs its backing loan transfer"

// returnOriginOptionElement answers the single payload type of a certified step's Option result.
// A step only advances the cursor object and copies the walked element out of the base
// (emit_iter_step.go:84–115; VM intrinsic_range_iter.go:140–144).
func returnOriginOptionElement(in *types.Interner, id types.TypeID) (types.TypeID, bool) {
	info, found := in.UnionInfo(returnOriginResolveAlias(in, id))
	if !found || info == nil || len(info.TypeArgs) != 1 {
		return types.NoTypeID, false
	}
	return info.TypeArgs[0], true
}

// freeTemplateElement treats this body's own direct type parameter as holding no
// borrow and no storage loan by recording NoBorrowedState on it: every caller and
// every finalized use must then instantiate it with a type that satisfies it.
func (b *returnOriginBody) freeTemplateElement(elem types.TypeID, span source.Span) bool {
	fn := b.function
	if _, slot := fn.templateSlot(elem); !slot || !fn.item.Body.IsValid() {
		return false
	}
	return len(b.requireOpaqueState(returnOriginView(fn), elem, span).roots) == 0
}

// loanElement says whether a payload-free element moved or copied out of a
// container can still keep storage loans: an array or cursor, or any type
// NoBorrowedState cannot prove free of them (an Option reaching one).
func (b *returnOriginBody) loanElement(c returnOriginIndexType) bool {
	if !b.elementsFree(c) {
		return false
	}
	required := returnOriginView(b.function).requirement(returnOriginNoBorrowedState, c.element)
	return b.analyzer.loanCarrier(c.element) || required.failed() || len(required.atoms) != 0
}

// localLoan says whether loans read at a load site include a frame-local storage
// root; a formal's L(slot) is checked at each caller instead (guardLoanResult).
func localLoan(loans returnOriginValue) bool {
	return slices.ContainsFunc(loans.roots, func(root returnOrigin) bool { return root.kind == returnOriginLocal })
}

// guardLoanResult is the caller half of G6 for loans a callee read out of loan
// elements: an L(slot) erased in the callee can still leave through its result or
// another argument that can hold a loan, so each such slot's loans are refused here
// when local, and are unknowable without proven targets.
func (b *returnOriginBody) guardLoanResult(callee *returnOriginFunction, slots []returnOriginArgument, targets map[int]returnOriginBackingTargets,
	pre returnOriginEnv, result types.TypeID, span source.Span,
) {
	in, holders := b.function.unit.Sema.TypeInterner, 0 // arguments that can hold a loan or reach storage through a reference
	for _, slot := range slots {
		for _, expr := range slot.exprs {
			id, typ, ok := returnOriginIndexResolve(in, b.function.unit.Sema.ExprTypes[expr])
			if ok && typ.Kind == types.KindReference {
				id = typ.Elem
			}
			if b.holdsLoan(id) || returnOriginTypeShape(in, id, nil) != returnOriginRefFree {
				holders++
			}
		}
	}
	for _, slot := range callee.backingSlots {
		c, canonical := b.callSiteContainer(slots, int(slot))
		t, proven := targets[int(slot)]
		switch {
		case !canonical || !b.loanElement(c) || !b.holdsLoan(result) && holders < 2:
		case !proven:
			b.pending(span, returnOriginCursorLoanElement)
		case localLoan(returnOriginTargetLoans(pre, t)):
			b.pending(span, "storage loan would be discarded by a payload-free value")
		}
	}
}

// returnOriginRangeElement answers a Range instance's element. The family is the
// certified rt_range_int_full result, cached once per analysis on every unit index;
// without it (a core-free analysis) nothing is a Range and the caller stays Unknown.
func returnOriginRangeElement(owner *returnOriginFunction, id types.TypeID) (types.TypeID, bool) {
	in := owner.unit.Sema.TypeInterner
	family, known := in.StructInfo(owner.unit.intRangeType)
	info, typed := in.StructInfo(returnOriginResolveAlias(in, id))
	if owner.unit.intRangeType == types.NoTypeID || !known || !typed || family == nil || info == nil ||
		info.Name != family.Name || info.Decl != family.Decl || len(info.TypeArgs) != 1 {
		return types.NoTypeID, false
	}
	return info.TypeArgs[0], true
}

// elementsFreeAt is elementsFree, or this body's own direct type parameter under a
// recorded NoBorrowedState condition that every caller and finalized use discharges.
func (b *returnOriginBody) elementsFreeAt(c returnOriginIndexType, span source.Span) bool {
	return b.elementsFree(c) || b.freeTemplateElement(c.element, span)
}

// cursorDeclaration answers a core declaration whose result borrows its receiver.
// A `__range` cursor over a fixed base points into that storage (emit_iter.go:342–347);
// over a dynamic base it keeps the raw data pointer without retaining the header
// (emit_iter.go:334–341), so it keeps the base value's loans as well. The element stays
// a subject, so an element that can hold a borrow is refused wherever the row is read.
func (a *returnOriginAnalyzer) cursorDeclaration(fn *returnOriginFunction) ([]returnOrigin, []types.TypeID, bool) {
	op, certified := a.coreArrayIntrinsic(fn)
	if !certified || op != returnOriginArrayRange {
		return nil, nil, false
	}
	roots := []returnOrigin{{kind: returnOriginParam}}
	if c, _ := returnOriginContainer(fn.unit.Sema.TypeInterner, fn.info.Params[0]); c.family == fn.unit.Sema.TypeInterner.ArrayNominalType() {
		roots = append(roots, returnOrigin{kind: returnOriginParam, selector: returnOriginInputLoans})
	}
	return roots, []types.TypeID{fn.candidate.TemplateParams[0]}, true
}
