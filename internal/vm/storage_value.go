package vm

import (
	"surge/internal/mir"
	"surge/internal/types"
)

// The operations that make a composite value BE its storage.
//
// Three questions have to be answered together, and answering them apart is
// how a representation ends up half-migrated: where a producer builds a
// composite, where a slot keeps one, and what happens when the first moves into
// the second.

// buildComposite reserves the storage a producer will build into.
//
// The producer receives the destination TYPE, and this is where that shows: an
// extent cannot be reserved without one, because exact layout decides the size,
// the alignment and every member offset from the type and nothing else.
func (vm *VM) buildComposite(frame *Frame, typeID types.TypeID) (StorageRef, *VMError) {
	if frame == nil || frame.scratch == nil {
		return StorageRef{}, vm.eb.invalidLocation("no activation storage to build a composite in")
	}
	ref, err := vm.reserveScratch(frame.scratch, typeID)
	if err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	// Scratch is REUSED, so a fresh extent starts out holding whatever the
	// previous instruction's temporary left there. Zeroing is what makes those
	// bytes uninitialized rather than a stale value: a producer that fills only
	// some members would otherwise leave the rest reading as the corpse of
	// something that has already been released.
	if err := vm.storageZero(ref); err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return ref, nil
}

// slotStorage names the extent a composite slot owns.
//
// A slot that is not composite-typed has no extent, and says so rather than
// producing one: the plan gives an offset only to value composites, which is
// the rule that keeps the arena meaning per-composite storage instead of a
// blanket byte pool.
func (vm *VM) slotStorage(frame *Frame, id mir.LocalID) (StorageRef, bool, *VMError) {
	if frame == nil || frame.storage == nil || frame.Func == nil {
		return StorageRef{}, false, nil
	}
	offset := vm.storagePlanFor(frame.Func).OffsetOf(int(id))
	if offset == NoStorageOffset {
		return StorageRef{}, false, nil
	}
	ref, err := vm.storageRefAt(frame.storage, offset, frame.Locals[id].TypeID)
	if err != nil {
		return StorageRef{}, false, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return ref, true, nil
}

// globalStorage names the extent a composite global owns.
func (vm *VM) globalStorage(id mir.GlobalID) (StorageRef, bool, *VMError) {
	if vm == nil || vm.globalArena == nil {
		return StorageRef{}, false, nil
	}
	offset := vm.globalPlan.OffsetOf(int(id))
	if offset == NoStorageOffset {
		return StorageRef{}, false, nil
	}
	ref, err := vm.storageRefAt(vm.globalArena, offset, vm.Globals[id].TypeID)
	if err != nil {
		return StorageRef{}, false, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return ref, true, nil
}

// moveComposite puts a composite into the storage that will own it from now on,
// and gives up the storage it came from IN THE SAME STEP.
//
// The handover is not a second operation and must not become one. Between a
// store that has happened and a release that has not, one value has two owners,
// and the rewind at the end of the instruction drops what the slot now holds —
// as a use-after-free reported at the rewind rather than at the store that
// caused it. Everything this function does to the source happens here or not at
// all.
//
// The transfer is a copy followed by a drop of the source rather than a raw
// byte move, because a member that names a referent stores an index into ITS
// arena's table, and those indices mean nothing in another arena. The copy
// retains what it duplicates and the drop releases what the source held, so the
// counts come out where they started and the destination is the only owner.
//
// A value moved onto itself is left entirely alone. It is already where it is
// going, and the copy would be refused while the drop would destroy it.
func (vm *VM) moveComposite(frame *Frame, dst StorageRef, src Value, initialized bool) *VMError {
	ref, ok := src.Storage()
	if !ok {
		return vm.eb.typeMismatch("composite", src.Kind.String())
	}
	if ref.Arena == dst.Arena && ref.Offset == dst.Offset {
		return nil
	}
	copyInto := vm.storageCopy
	if initialized {
		copyInto = vm.storageReplace
	}
	if err := copyInto(dst, ref); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if err := vm.storageDrop(ref); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return vm.releaseTemporary(frame, ref)
}

// releaseTemporary tells the activation's scratch that an extent it was holding
// has moved on.
//
// A reference that is not into this scratch is not this scratch's business and
// is silently accepted; one that IS but names nothing live is refused, because a
// mover that believed it was clearing scratch and was not would leave the
// rewind dropping a value that now has a second owner.
func (vm *VM) releaseTemporary(frame *Frame, ref StorageRef) *VMError {
	if frame == nil || frame.scratch == nil {
		return nil
	}
	if err := frame.scratch.release(ref); err != nil {
		return vm.eb.makeError(PanicMemoryLeakDetected, err.Error())
	}
	return nil
}

// peekStorage decodes whatever an LKStorage location names WITHOUT counting a
// second holder.
//
// A composite reads back as itself — the reference IS the value, so there is
// nothing to decode and nothing to count. Anything else is a member of some
// composite and is decoded out of its own extent by the encoding its type
// selects, and what comes back is valid only while the owner is alive and
// unmodified.
//
// This is the read a LOCATION answers with, which is why it is the uncounted
// one: a location read is a look at where a value lives, and every caller that
// wants to keep what it saw says so afterwards. A read that counted here would
// leak on every caller that only looked, and would count twice for the ones
// that go on to duplicate.
func (vm *VM) peekStorage(ref StorageRef) (Value, *VMError) {
	if vm.storageCellKind(ref.TypeID) == cellComposite {
		if _, err := ref.resolve(0); err != nil {
			return Value{}, vm.eb.makeError(PanicRCUseAfterFree, err.Error())
		}
		return MakeComposite(ref), nil
	}
	member, err := vm.memberAt(ref.Offset, ref.TypeID)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	value, err := vm.storageReadCell(ref, member)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicRCUseAfterFree, err.Error())
	}
	return value, nil
}

// loadStorage reads what an LKStorage location names as a value the caller
// OWNS.
//
// It is peekStorage plus the count that ownership needs. A composite is
// answered by peekStorage with its own reference and is NOT counted here —
// there is no count to raise — so a caller that wants an owned composite asks
// for a duplicate instead, and the two callers that can see one both do.
func (vm *VM) loadStorage(ref StorageRef) (Value, *VMError) {
	value, vmErr := vm.peekStorage(ref)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if value.Kind == VKComposite {
		return value, nil
	}
	return vm.cloneForShare(value)
}

// storeStorage writes a value into the extent an LKStorage location names,
// releasing what that extent held.
//
// The release is owed here and nowhere else: inline storage is the only thing
// that names what it holds, so an overwrite that merely wrote over the bytes
// would leave what they counted alive with nothing left to reach it.
func (vm *VM) storeStorage(frame *Frame, ref StorageRef, val Value) *VMError {
	if vm.storageCellKind(ref.TypeID) == cellComposite {
		return vm.moveComposite(frame, ref, val, true)
	}
	member, err := vm.memberAt(ref.Offset, ref.TypeID)
	if err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	previous, readErr := vm.storageReadCell(ref, member)
	if readErr != nil {
		return vm.eb.makeError(PanicRCUseAfterFree, readErr.Error())
	}
	if err := vm.storageWriteCell(ref, member, val); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	vm.dropValue(previous)
	return nil
}

// memberStorage projects one member out of a composite by name or position.
func (vm *VM) memberStorage(owner StorageRef, index int) (StorageRef, *VMError) {
	members, err := vm.compositeMembers(owner.TypeID)
	if err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if index < 0 || index >= len(members) {
		return StorageRef{}, vm.eb.fieldIndexOutOfRange(index, len(members))
	}
	ref, projectErr := owner.memberRef(members[index])
	if projectErr != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, projectErr.Error())
	}
	return ref, nil
}

// readMember reads one member of a composite as a value the caller OWNS.
//
// This is the read every consumer of a composite wants, and it is a copy. A
// composite member gets its own extent, because handing back the member's own
// reference would be a borrow and the caller asked for a value; anything else
// is decoded and counted. The borrow is still available to callers that want
// one — memberStorage hands back the reference itself — and keeping the two
// spellings apart is what stops a read from quietly aliasing.
func (vm *VM) readMember(frame *Frame, owner StorageRef, index int) (Value, *VMError) {
	member, vmErr := vm.memberStorage(owner, index)
	if vmErr != nil {
		return Value{}, vmErr
	}
	return vm.readStorageValue(frame, member)
}

// readStorageValue reads one extent as an owned value the caller must release.
//
// It is separate from readMember because a member is not the only thing that
// has an extent: an element of a run is addressed by arithmetic rather than by
// a member list, and both owe the same thing once the extent is named. Keeping
// the tail in one place is what stops a run's elements and a composite's
// members from disagreeing about when a read counts a second holder.
func (vm *VM) readStorageValue(frame *Frame, ref StorageRef) (Value, *VMError) {
	if vm.storageCellKind(ref.TypeID) == cellComposite {
		return vm.duplicateComposite(frame, MakeComposite(ref))
	}
	return vm.loadStorage(ref)
}

// peekMember decodes one member WITHOUT counting a second holder.
//
// It is the read for a caller that wants to LOOK at a member — a length, an
// opaque handle word, a discriminant — and not keep it. What comes back is
// valid only while the owner is alive and unmodified, which is why it is a
// separate spelling from readMember: a peek that was quietly counted would
// leak on every one of these paths, since none of them has anything to drop.
func (vm *VM) peekMember(owner StorageRef, index int) (Value, *VMError) {
	member, vmErr := vm.memberStorage(owner, index)
	if vmErr != nil {
		return Value{}, vmErr
	}
	return vm.peekStorage(member)
}

// dropMember releases one member IN PLACE and leaves its extent zeroed.
//
// It reads the member WITHOUT counting a second holder, which is the whole
// reason it is not spelled as a load followed by a store of nothing: loadStorage
// clones what it decodes, so that spelling would raise the count and then lower
// it, releasing the copy it had just made and leaving the original standing.
// The zero is the other half — a member released but left in place would be
// released again by the next walk of its owner.
func (vm *VM) dropMember(ref StorageRef) *VMError {
	if vm.storageCellKind(ref.TypeID) == cellComposite {
		if err := vm.storageDrop(ref); err != nil {
			return vm.eb.makeError(PanicUnimplemented, err.Error())
		}
		return nil
	}
	shape, err := vm.memberAt(ref.Offset, ref.TypeID)
	if err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	value, readErr := vm.storageReadCell(ref, shape)
	if readErr != nil {
		return vm.eb.makeError(PanicRCUseAfterFree, readErr.Error())
	}
	vm.dropValue(value)
	if err := vm.storageZero(ref); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return nil
}

// placeType is the type a place declares, which is what a producer needs to
// know in order to build a composite at all.
//
// A projected place answers with no type: the type of `a.b.c` is a walk this
// does not do, and no composite literal is ever assigned through a projection
// in MIR — a literal is assigned to a slot and the projection is a separate
// instruction. Answering NoTypeID is therefore the honest answer rather than a
// gap, and the producer that needs a type refuses with a message naming what it
// could not find.
func (vm *VM) placeType(frame *Frame, place mir.Place) types.TypeID {
	if len(place.Proj) != 0 {
		return types.NoTypeID
	}
	if place.Kind == mir.PlaceGlobal {
		if int(place.Global) < len(vm.Globals) {
			return vm.Globals[place.Global].TypeID
		}
		return types.NoTypeID
	}
	if frame != nil && int(place.Local) < len(frame.Locals) {
		return frame.Locals[place.Local].TypeID
	}
	return types.NoTypeID
}

// currentFrame is the activation whose instruction is running, and therefore
// the one whose scratch a temporary in flight belongs to.
func (vm *VM) currentFrame() *Frame {
	if vm == nil || len(vm.Stack) == 0 {
		return nil
	}
	return vm.Stack[len(vm.Stack)-1]
}

// dropComposite releases everything a composite owns.
func (vm *VM) dropComposite(v Value) {
	ref, ok := v.Storage()
	if !ok {
		return
	}
	vm.dropCompositeStorage(ref)
}

// dropCompositeStorage releases everything an exact composite extent owns.
func (vm *VM) dropCompositeStorage(ref StorageRef) {
	// A drop that cannot complete is not raised here: dropValue is called from
	// paths that are finishing rather than asking for something, and the extent
	// is going away either way. What it could not release stays on the heap,
	// where the leak check sees it.
	_ = vm.storageDrop(ref) //nolint:errcheck // see above
	// Giving up the extent is the same step as releasing what it held, for the
	// same reason a move is one step. A refusal here means the extent had
	// already been handed over — a value that was moved and then dropped — and
	// the drop above found the zeroes the handover left, so there is nothing to
	// report and nothing to do.
	if frame := vm.currentFrame(); frame != nil && frame.scratch != nil {
		_ = frame.scratch.release(ref) //nolint:errcheck // see above
	}
}

// duplicateComposite gives a composite an INDEPENDENT copy of its storage, so
// that mutating either is invisible through the other.
//
// This is what a value composite has instead of a retained reference. There is
// no count to raise: the storage is the value, so sharing it would make two
// names for one value and the language says they are two.
func (vm *VM) duplicateComposite(frame *Frame, v Value) (Value, *VMError) {
	ref, ok := v.Storage()
	if !ok {
		return v, nil
	}
	fresh, vmErr := vm.buildComposite(frame, ref.TypeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if err := vm.storageCopy(fresh, ref); err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return MakeComposite(fresh), nil
}
