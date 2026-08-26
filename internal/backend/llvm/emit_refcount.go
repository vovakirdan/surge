package llvm

import (
	"fmt"

	"surge/internal/types"
)

// Reference counting for the values that are Copy at the surface and counted
// underneath: the arbitrary-precision scalars and the channel handle.
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
// A `Channel<T>` handle is also one word, but its count is the runtime's: it
// lives inside `struct rt_channel`, whose layout this backend does not know,
// and it is ATOMIC, because a copy of the handle may be retained from another
// shard's frame. So that retain is a call — `rt_channel_handle_retain` — and
// the inline bump above must never be applied to it: it would add one to the
// first word of a channel header, non-atomically, which is neither the count
// nor safe.
//
// Release stays out of line for both. It happens once per place at scope exit
// rather than per copy, and it has to branch into the destructor anyway, so
// inlining it would buy nothing and duplicate the free logic in every function.

// emitRetainValue gives a just-loaded value's destination a reference of its
// own, dispatching on the value's TYPE: a counted scalar takes the inline bump,
// a channel handle takes the runtime's atomic retain.
//
// The scalar form is BRANCHLESS: instead of jumping over the bump when the value
// is the NULL zero, it redirects the read-modify-write onto a thread-local
// scratch word. That keeps a float copy as straight-line code and avoids
// splitting the enclosing basic block mid-expression, which would disturb every
// label the surrounding MIR block owns.
//
// The inline form deliberately omits the overflow check that
// `rt_bigfloat_retain` carries: the check exists to name a defect loudly, and
// paying a compare on the hot path to catch a count that cannot be reached
// without 2^32 live references would invert the tradeoff this whole mechanism
// is for. The out-of-line entry point keeps it.
func (fe *funcEmitter) emitRetainValue(val, ty string, typeID types.TypeID) {
	if fe == nil || ty != "ptr" {
		return
	}
	if fe.emitter != nil && fe.emitter.types.IsRefCountedHandle(typeID) {
		// NULL-safe in the runtime: a slot the handle was moved out of holds
		// NULL and is retained as nothing.
		fmt.Fprintf(&fe.emitter.buf, "  call void @rt_channel_handle_retain(ptr %s)\n", val)
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
