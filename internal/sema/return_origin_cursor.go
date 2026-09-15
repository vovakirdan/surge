package sema

import (
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
