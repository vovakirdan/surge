package llvm

import "fmt"

// Reference counting for the arbitrary-precision scalars.
//
// A `float` value is one machine word: NULL for the canonical zero, otherwise a
// pointer to a `SurgeBigFloat` whose FIRST field is a 32-bit count
// (`runtime/native/rt_bignum_internal.h` static-asserts both facts). Giving a
// second place its own reference is therefore a load, an add and a store
// through that pointer — no call.
//
// The bump is emitted INLINE rather than as a call to `rt_bigfloat_retain`
// because it runs on every duplication of a float: at a let-init, a struct
// field, an array element, a channel payload. Paying a call there would cost
// more than the counting saves over the deep copy it replaces, and it would
// block the later work that emits the arithmetic fast path as IR at the call
// site.
//
// Release stays out of line. It happens once per place at scope exit rather
// than per copy, and it has to branch into the destructor anyway, so inlining
// it would buy nothing and duplicate the free logic in every function.

// emitRetainValue bumps the reference count of a just-loaded value.
//
// It is BRANCHLESS: instead of jumping over the bump when the value is the NULL
// zero, it redirects the read-modify-write onto a thread-local scratch word.
// That keeps a float copy as straight-line code and avoids splitting the
// enclosing basic block mid-expression, which would disturb every label the
// surrounding MIR block owns.
//
// The inline form deliberately omits the overflow check that
// `rt_bigfloat_retain` carries: the check exists to name a defect loudly, and
// paying a compare on the hot path to catch a count that cannot be reached
// without 2^32 live references would invert the tradeoff this whole mechanism
// is for. The out-of-line entry point keeps it.
func (fe *funcEmitter) emitRetainValue(val, ty string) {
	if fe == nil || ty != "ptr" {
		return
	}
	isNull := fe.nextTemp()
	slot := fe.nextTemp()
	count := fe.nextTemp()
	bumped := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq ptr %s, null\n", isNull, val)
	fmt.Fprintf(&fe.emitter.buf, "  %s = select i1 %s, ptr @%s, ptr %s\n",
		slot, isNull, retainScratchGlobal, val)
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i32, ptr %s\n", count, slot)
	fmt.Fprintf(&fe.emitter.buf, "  %s = add i32 %s, 1\n", bumped, count)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %s, ptr %s\n", bumped, slot)
}
