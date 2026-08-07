package vm

import "surge/internal/types"

// What a value owes when it crosses a TRANSPORT boundary.
//
// A transport is a handover between two activations that are not on the stack
// together: a channel queue, a parked sender's payload, a resume value waiting
// on a woken task. The value is held by the runtime in between, and the
// activation that produced it goes away — a channel send is the one place in
// the language where a value routinely outlives its producer without being
// stored anywhere the producer can see.
//
// Under a box that cost nothing: the runtime held a handle, and a handle is
// independent of every frame. A composite is its BYTES, so a payload queued as
// a reference into the sender's arena names storage that has been given back
// the moment the sender's activation rewinds. The generation check catches it —
// `stale reference to type#4 at offset 32 (generation 1, arena is at 2)` — which
// is the storage layer doing its job and no consolation to the program.
//
// So there are three obligations, and they are one set because each is the
// other's counterpart:
//
//   - COPY IN, at the boundary the value enters, into storage the transport
//     owns rather than storage any activation owns;
//   - COPY OUT, at the boundary it leaves, into storage the receiver owns,
//     giving up the transport's copy in the same step;
//   - RELEASE, for a payload nobody ever receives — a channel dropped with a
//     value still queued, a send parked when the program ends, a resume value
//     on a task that was cancelled before it ran.
//
// Exactly one of copy-out and release happens to any payload. That is the
// property the round-trip tests assert, and it is why these three are written
// together rather than each at the site that happens to need it.
//
// This is a Wave C INTERVAL MEASURE. It gives a transported composite an arena
// of its own, which is correct and is not the destination: Wave D gives the
// runtime typed carriers, and a payload will then travel as a carrier the
// runtime understands rather than as bytes the VM copies twice. When that
// lands, this file goes away — the copies it makes are exactly the ones a
// carrier makes unnecessary.

// transportCopyIn gives a value storage that outlives the activation sending
// it, and gives up the storage it came from IN THE SAME STEP.
//
// Anything that is not a composite already outlives its producer — a handle
// governs its own lifetime — and is handed straight back. A composite gets an
// arena holding exactly itself. That arena is reachable only through the value
// that names it, so it needs no reclamation of its own: releasing what the
// payload holds is the whole of the obligation, and the bytes go when the last
// reference to them does.
func (vm *VM) transportCopyIn(val Value) (Value, *VMError) {
	ref, ok := val.Storage()
	if !ok {
		return val, nil
	}
	owned, vmErr := vm.transportExtent(ref.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if vmErr := vm.moveComposite(vm.currentFrame(), owned, val, false); vmErr != nil {
		return Value{}, vmErr
	}
	return MakeComposite(owned), nil
}

// transportCopyOut hands a transported value to its receiver as a value the
// RECEIVER owns, and gives up the transport's copy in the same step.
//
// The receiving frame is where it lands, because that is the activation that
// will decide what happens to it next — it may be stored into a slot, wrapped
// in an option, or dropped where it stands, and all three want a value whose
// storage the frame is already reclaiming.
func (vm *VM) transportCopyOut(frame *Frame, val Value) (Value, *VMError) {
	if _, ok := val.Storage(); !ok {
		return val, nil
	}
	received, vmErr := vm.buildComposite(frame, val.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if vmErr := vm.moveComposite(frame, received, val, false); vmErr != nil {
		return Value{}, vmErr
	}
	return MakeComposite(received), nil
}

// transportRelease releases a payload nobody received.
//
// It is spelled separately from dropValue even though it does the same thing,
// because the two say different things at the call site: dropValue is a value
// reaching the end of its life, and this is a HANDOVER THAT DID NOT HAPPEN. A
// reader auditing whether every payload is accounted for needs to find these,
// and a search for the word that means "the receive never came" finds them.
func (vm *VM) transportRelease(val Value) {
	vm.dropValue(val)
}

// transportExtent reserves an arena holding exactly one value.
//
// It is not a frame arena and not a container's: it belongs to the payload for
// as long as the payload is in flight. A plan of one slot is how it is built,
// because that is the only way an offset is chosen by the layout registry
// rather than by this function.
func (vm *VM) transportExtent(typeID types.TypeID) (StorageRef, *VMError) {
	if vm.Layouts == nil {
		return StorageRef{}, vm.eb.invalidLocation("no finalized layout registry for a transported value")
	}
	plan := buildStoragePlan(vm.Layouts, []types.TypeID{typeID}, vm.isValueCompositeType)
	offset := plan.OffsetOf(0)
	if offset == NoStorageOffset {
		return StorageRef{}, vm.eb.invalidLocation("a transported value has no extent of its own")
	}
	ref, err := vm.storageRefAt(newArena(&plan, 1), offset, typeID)
	if err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return ref, nil
}
