package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// A select answers with an INDEX into its own arm list, and this file is the
// one place that says so: the runtime hands the index back in a machine word,
// the emitter narrows that word to the index's own type, and a destination
// which is not that type refuses the module.
//
// This is what replaces emitI64ToValue on the select paths, which were the last
// two callers that helper had. It existed because the winner index shared one
// bridge with transported payloads: handed a bare i64 and whatever type the
// destination happened to carry, it rebuilt a value of that type by
// reinterpretation -- inttoptr for a pointer, bitcast for a float, adopt out of
// a transport allocation for a composite. A winner index is none of those. It
// is a control answer the runtime computed by value, so nothing here is owned,
// adopted, cloned, dropped or released; the word was only how it travelled.
//
// Naming the type at this boundary is what removes the reinterpretation. It
// also turns a destination the select lowering never meant into an error
// instead of into silently rebuilt bits.

// selectWinnerIndexType is the type a select's winner index has.
//
// Both select lowerings give their destination local exactly this type, local
// and crossing alike, and both share the arm dispatch that reads it
// (internal/mir/lower_expr_select.go). The emitter checks rather than assumes:
// the two live in different packages and nothing but this check ties them
// together.
func (fe *funcEmitter) selectWinnerIndexType() types.TypeID {
	return fe.emitter.types.Builtins().Int32
}

// emitSelectWinnerIndex narrows the winner index the runtime returned in `bits`
// and stores it into `dst`.
//
// `bits` is rt_select_poll's i64 return on the local path and the anchored
// reply's out_bits on the crossing path. Both already hold the index -- the
// local caller has taken the pending branch before arriving here, and the
// crossing caller has taken the winner branch -- so the narrowing is the whole
// of the conversion.
func (fe *funcEmitter) emitSelectWinnerIndex(bits string, dst mir.Place) error {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return fmt.Errorf("missing type info")
	}
	dstType, err := fe.placeBaseType(dst)
	if err != nil {
		return err
	}
	indexType := fe.selectWinnerIndexType()
	if resolveValueType(fe.emitter.types, dstType) != indexType {
		return fmt.Errorf("select winner index destination is %s, want %s",
			types.Label(fe.emitter.types, dstType), types.Label(fe.emitter.types, indexType))
	}
	indexTy, err := fe.emitter.llvmValueType(indexType)
	if err != nil {
		return err
	}
	narrowed := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = trunc i64 %s to %s\n", narrowed, bits, indexTy)
	ptr, _, align, err := fe.emitPlaceStorage(dst)
	if err != nil {
		return err
	}
	fe.emitValueStore(indexTy, narrowed, ptr, align)
	return nil
}
