package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// placeStore answers a store whose place is neither an external cell (`*p`)
// nor an index store. A reference-free place holds no reference or borrowed
// state and a reference-free value brings none, so the store cannot change a
// caller's cell or any reference-bearing contents; as on the borrow-free
// binary path, only loans riding on a payload-free value are refused. Every
// other place keeps its unproved-effect obligation.
// The skip is decided only by returnOriginTypeShape of the stored place's own
// type (the field itself, never its container) and of the stored value's
// type; never by a cached shape or a type name. A field whose type becomes a
// borrow mark (for example BytesView) therefore keeps its refusal.
// An implicitly converted operand stands for the conversion's result, which
// this store never evaluates, so it keeps the obligation like an index store.
func (b *returnOriginBody) placeStore(place, stored ast.ExprID, value returnOriginValue, env returnOriginEnv, span source.Span) (returnOriginEnv, returnOriginValue) {
	conversions := b.function.unit.Sema.ImplicitConversions
	_, placeConverted := conversions[place]
	_, storedConverted := conversions[stored]
	if !placeConverted && !storedConverted && b.shape(place) == returnOriginRefFree && b.shape(stored) == returnOriginRefFree {
		return env, b.discardLoans(value, span)
	}
	return b.taintExternalCellEffects(env, span, "store through a place needs reference-content transfer"), value
}
