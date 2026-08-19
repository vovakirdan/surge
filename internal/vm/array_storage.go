package vm

import (
	"fmt"

	"surge/internal/types"
)

// A dynamic array's elements live in ONE run of exact element slots inside the
// storage the container already owns.
//
// The run is not a list of values. It is `count * stride` bytes at the element
// type's own layout, and element `i` is the extent at `base + i*stride` — the
// same arithmetic a fixed array's members already use, with the length known at
// runtime instead of in the type. What that buys is the thing a universal
// element list cannot give: an element's type is a property of the ARRAY, so
// every element answers with the same size, the same alignment and the same
// cell kind, and none of them has to be asked what it happens to hold.
//
// Two invariants hold everything else up:
//
//   - `ArrElemType` is the ONLY source of stride, alignment and cell kind. A
//     caller never passes an element type in; it is read off the descriptor, so
//     a `*T` written into a slot cannot read back as a `&T`.
//   - slots in `[ArrLen, ArrCap)` are DEAD. They are never dropped and never
//     read; a `pop` moves the bytes out and leaves the slot zeroed, so the next
//     push must INITIALISE that slot rather than replace it — replacing would
//     drop what the pop already took.

// runStride is how many bytes one element of a run occupies.
//
// It comes from the layout registry and never from the element's LLVM spelling
// or from whatever a particular element happens to hold: only the registry
// answers for both a scalar and a composite.
func (vm *VM) runStride(elem types.TypeID) (uint64, error) {
	if vm == nil || vm.Layouts == nil {
		return 0, fmt.Errorf("storage: no finalized layout registry for an array element")
	}
	facts, err := vm.Layouts.Require(elem)
	if err != nil {
		return 0, fmt.Errorf("storage: array element type#%d has no usable layout: %w", elem, err)
	}
	if facts.Stride == 0 {
		// A zero-sized element still needs a distinct index, and a stride of
		// zero would put every element at one address. One byte per slot keeps
		// indices apart without giving the element storage it does not have.
		return 1, nil
	}
	return facts.Stride, nil
}

// runElemMember describes one slot of a run: its size, its alignment and how it
// is encoded. Offset is filled in per element by runElemRef.
func (vm *VM) runElemMember(elem types.TypeID) (storageMember, error) {
	return vm.memberAt(0, elem)
}

// reserveRun hands out one run of `count` element slots.
//
// It differs from reserveScratch in exactly one way that matters: the entry it
// leaves behind remembers HOW MANY values the extent holds, so a rewind drops
// each of them instead of treating the whole run as one value. A run has no
// type of its own to reserve against — interning a fixed-array type per runtime
// capacity would make the type interner grow with the program's data — so the
// element type plus a count is what the entry carries.
func (vm *VM) reserveRun(s *scratch, elem types.TypeID, count uint64) (StorageRef, error) {
	if s == nil || s.arena == nil {
		return StorageRef{}, fmt.Errorf("storage: this container has no storage for its elements")
	}
	stride, err := vm.runStride(elem)
	if err != nil {
		return StorageRef{}, err
	}
	member, err := vm.runElemMember(elem)
	if err != nil {
		return StorageRef{}, err
	}
	align := member.Align
	if align == 0 {
		align = 1
	}
	offset, ok := alignUpChecked(s.used, align)
	if !ok {
		return StorageRef{}, fmt.Errorf("storage: an element run of type#%d cannot be aligned to %d", elem, align)
	}
	span, ok := mulChecked(stride, count)
	if !ok {
		return StorageRef{}, fmt.Errorf("storage: an element run of %d values of type#%d overflows", count, elem)
	}
	end, ok := addChecked(offset, span)
	if !ok {
		return StorageRef{}, fmt.Errorf("storage: an element run of type#%d overflows its container's storage", elem)
	}
	s.grow(end)
	s.used = end
	s.entries = append(s.entries, scratchEntry{
		offset: offset,
		size:   span,
		typeID: elem,
		live:   true,
		count:  count,
		stride: stride,
	})
	return StorageRef{
		Arena:  s.arena,
		Offset: offset,
		TypeID: elem,
		Gen:    s.arena.Generation(),
		Align:  align,
	}, nil
}

// setRunLive tells the run's entry how many of its slots are INITIALISED.
//
// The entry tracks `ArrLen` and never `ArrCap`, because a rewind drops what the
// run holds and the slots past the length hold nothing. Every operation that
// changes the length calls this in the same step, so the two cannot drift.
func (s *scratch) setRunLive(base StorageRef, count uint64) error {
	if s == nil || base.Arena != s.arena {
		return fmt.Errorf("storage: an element run outside this container's storage cannot be resized")
	}
	for i := len(s.entries) - 1; i >= 0; i-- {
		if s.entries[i].offset == base.Offset && s.entries[i].live {
			if count > s.entries[i].size/maxU64(s.entries[i].stride, 1) {
				return fmt.Errorf("storage: an element run of %d bytes cannot hold %d values",
					s.entries[i].size, count)
			}
			s.entries[i].count = count
			return nil
		}
	}
	return fmt.Errorf("storage: no live element run at offset %d", base.Offset)
}

func maxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// runElemRef addresses element `i` of a run.
//
// The element type comes from the DESCRIPTOR rather than from the caller. A
// signature that let a caller name the type would be the shape that already
// caused a pointer to read back as a reference: the encoding accepts a pointer
// and a reference identically, and only the member's recorded type tells them
// apart.
//
// The alignment folds rather than replaces, for the same reason a struct
// field's does: inside a packed container the run lands at an offset the
// element's own alignment does not divide, and claiming that alignment would
// describe an address the base cannot produce.
func (vm *VM) runElemRef(base StorageRef, elem types.TypeID, index int) (StorageRef, error) {
	if index < 0 {
		return StorageRef{}, fmt.Errorf("storage: element index %d is negative", index)
	}
	return vm.runElemRefAt(base, elem, uint64(index))
}

// runElemRefAt is runElemRef for a caller that already counts in unsigned
// elements, so a teardown walking a run's own count never converts it.
func (vm *VM) runElemRefAt(base StorageRef, elem types.TypeID, index uint64) (StorageRef, error) {
	stride, err := vm.runStride(elem)
	if err != nil {
		return StorageRef{}, err
	}
	member, err := vm.runElemMember(elem)
	if err != nil {
		return StorageRef{}, err
	}
	delta, ok := mulChecked(stride, index)
	if !ok {
		return StorageRef{}, fmt.Errorf("storage: element %d of type#%d overflows its run", index, elem)
	}
	offset, ok := addChecked(base.Offset, delta)
	if !ok {
		return StorageRef{}, fmt.Errorf("storage: element %d of type#%d overflows its container", index, elem)
	}
	return StorageRef{
		Arena:  base.Arena,
		Offset: offset,
		TypeID: elem,
		Gen:    base.Gen,
		Align:  memberAccessAlign(base.Align, delta, member.Align),
	}, nil
}

// growRunCapacity is the capacity a run grows to when it must hold minCap.
//
// Doubling is what bounds the storage a container's arena cannot reclaim per
// extent: a run that grew away leaves its bytes behind until the object is
// freed, and doubling keeps the total under twice the live run.
func growRunCapacity(current, minCap int) int {
	if minCap <= current {
		return current
	}
	grown := current
	if grown == 0 {
		grown = 4
	}
	for grown < minCap {
		grown *= 2
	}
	return grown
}

// arrayRunElemType answers the element type a dynamic array's run is made of.
//
// It asks the ARRAY type and nothing else. The alternative — looking at what an
// element happens to hold — is the shape that made an untyped array report the
// size of whatever landed in slot zero, through a resolution that unwraps `&`
// and `own` alike.
func (vm *VM) arrayRunElemType(arrayType types.TypeID) (types.TypeID, bool) {
	if vm == nil || vm.Types == nil || arrayType == types.NoTypeID {
		return types.NoTypeID, false
	}
	if elem, ok := vm.Types.ArrayInfo(arrayType); ok {
		return elem, true
	}
	resolved := vm.valueType(arrayType)
	if elem, ok := vm.Types.ArrayInfo(resolved); ok {
		return elem, true
	}
	if tt, ok := vm.Types.Lookup(resolved); ok && tt.Kind == types.KindArray {
		return tt.Elem, true
	}
	return types.NoTypeID, false
}

// initRunElem writes a value into a slot that holds NOTHING.
//
// It is deliberately not storeStorage. That one REPLACES, and releases what the
// slot held — right for `xs[i] = v` and wrong here, because a slot being
// initialised owns nothing and the release would be a drop nobody owes. The
// slot a pop vacated is the case that makes this more than a technicality: its
// bytes still look like a value, and replacing there frees what the pop took.
func (vm *VM) initRunElem(frame *Frame, obj *Object, index int, val Value) *VMError {
	ref, vmErr := vm.runElemSlot(obj, index)
	if vmErr != nil {
		return vmErr
	}
	if err := vm.storageZero(ref); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if vm.storageCellKind(obj.ArrElemType) == cellComposite {
		return vm.moveComposite(frame, ref, val, false)
	}
	member, err := vm.runElemMember(obj.ArrElemType)
	if err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if err := vm.storageWriteCell(ref, member, val); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return nil
}

// runElemSlot addresses one slot of an object's run, live or dead.
//
// It does NOT bounds-check against the length, because the two writers that
// reach past it are the ones that make the length grow: a push initialises the
// slot at the length before raising it.
func (vm *VM) runElemSlot(obj *Object, index int) (StorageRef, *VMError) {
	if obj == nil {
		return StorageRef{}, vm.eb.makeError(PanicOutOfBounds, "no array object")
	}
	if index < 0 || index >= obj.ArrCap {
		return StorageRef{}, vm.eb.outOfBounds(index, obj.ArrCap)
	}
	ref, err := vm.runElemRef(obj.ArrElems, obj.ArrElemType, index)
	if err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return ref, nil
}

// reserveArrayRun gives an object a run of `capacity` slots for `elem`.
func (vm *VM) reserveArrayRun(obj *Object, elem types.TypeID, capacity int) *VMError {
	if obj.storage == nil {
		obj.storage = newScratch()
	}
	base, err := vm.reserveRun(obj.storage, elem, safeUint64FromInt(max(capacity, 0)))
	if err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	obj.ArrElems = base
	obj.ArrElemType = elem
	obj.ArrLen = 0
	obj.ArrCap = max(capacity, 0)
	return nil
}

// setArrayLen records the run's new length in the object AND in the entry that
// will tear it down, so the two cannot drift into a leak or a double free.
func (vm *VM) setArrayLen(obj *Object, length int) *VMError {
	if obj == nil || obj.storage == nil {
		return nil
	}
	if err := obj.storage.setRunLive(obj.ArrElems, safeUint64FromInt(max(length, 0))); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	obj.ArrLen = max(length, 0)
	return nil
}

// growArrayRun makes room for at least minCap elements and MOVES the live ones.
//
// Order is the whole difficulty. Reserving can grow the arena, and growing
// REPLACES its byte slice, so both extents are resolved only after the
// reservation returns — a source slice taken beforehand would name bytes
// nothing will write to again. The copy is a move: the values are not cloned
// and the old slots are not dropped, so the old entry is marked dead in the
// same step and the teardown owes them exactly once, through the new run.
func (vm *VM) growArrayRun(obj *Object, minCap int) *VMError {
	if obj == nil || minCap <= obj.ArrCap {
		return nil
	}
	if obj.storage == nil {
		return vm.eb.makeError(PanicUnimplemented, "an array must own storage before it can grow")
	}
	newCap := growRunCapacity(obj.ArrCap, minCap)
	oldBase, oldLen, elem := obj.ArrElems, obj.ArrLen, obj.ArrElemType

	// The old extent is retired BEFORE the new one is reserved, not after.
	// A run of zero elements occupies no bytes and so does not advance the
	// high-water mark, which means the new reservation lands on the SAME
	// offset — and a retirement that ran afterwards would search by offset and
	// find the new entry instead of the old one, killing the run it had just
	// created. Retiring first cannot be ambiguous: the new entry does not
	// exist yet. It marks the extent dead without touching its bytes, so the
	// copy below still reads them.
	if releaseErr := obj.storage.release(oldBase); releaseErr != nil {
		return vm.eb.makeError(PanicUnimplemented, releaseErr.Error())
	}
	newBase, err := vm.reserveRun(obj.storage, elem, safeUint64FromInt(newCap))
	if err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if oldLen > 0 {
		stride, strideErr := vm.runStride(elem)
		if strideErr != nil {
			return vm.eb.makeError(PanicUnimplemented, strideErr.Error())
		}
		span, ok := mulChecked(stride, safeUint64FromInt(oldLen))
		if !ok {
			return vm.eb.makeError(PanicUnimplemented, "an element run overflows while growing")
		}
		src, resolveErr := oldBase.resolve(span)
		if resolveErr != nil {
			return vm.eb.makeError(PanicUnimplemented, resolveErr.Error())
		}
		dst, resolveErr := newBase.resolve(span)
		if resolveErr != nil {
			return vm.eb.makeError(PanicUnimplemented, resolveErr.Error())
		}
		copy(dst, src)
	}
	obj.ArrElems = newBase
	obj.ArrCap = newCap
	return vm.setArrayLen(obj, oldLen)
}

// takeRunElem moves element `index` OUT of the run and leaves its slot zeroed.
//
// This is the read no member operation spells. A load clones what it decodes,
// which would raise the count and leave the original standing; a drop releases
// it. A take does neither: the value leaves with exactly the ownership the slot
// had, which is what `pop` means.
func (vm *VM) takeRunElem(frame *Frame, obj *Object, index int) (Value, *VMError) {
	ref, vmErr := vm.runElemSlot(obj, index)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if vm.storageCellKind(obj.ArrElemType) == cellComposite {
		// Build the destination in the caller's activation and move the bytes
		// into it, which drops nothing and leaves the slot zeroed.
		dst, buildErr := vm.buildComposite(frame, obj.ArrElemType)
		if buildErr != nil {
			return Value{}, buildErr
		}
		if moveErr := vm.moveComposite(frame, dst, MakeComposite(ref), false); moveErr != nil {
			return Value{}, moveErr
		}
		return MakeComposite(dst), nil
	}
	member, err := vm.runElemMember(obj.ArrElemType)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	val, readErr := vm.storageReadCell(ref, member)
	if readErr != nil {
		return Value{}, vm.eb.makeError(PanicRCUseAfterFree, readErr.Error())
	}
	if zeroErr := vm.storageZero(ref); zeroErr != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, zeroErr.Error())
	}
	return val, nil
}

// appendRunBytes appends raw bytes as elements.
//
// The element type comes from the DESCRIPTOR and not from a builtin the caller
// names: an append that stamped `uint8` on the cells would be describing the
// array from the outside, and a byte view over an array of something else would
// encode at the wrong width.
func (vm *VM) appendRunBytes(frame *Frame, obj *Object, data []byte) *VMError {
	if len(data) == 0 {
		return nil
	}
	oldLen := obj.ArrLen
	newLen := oldLen + len(data)
	if newLen < oldLen {
		return vm.eb.invalidNumericConversion("array length out of range")
	}
	if newLen > obj.ArrCap {
		if vmErr := vm.growArrayRun(obj, newLen); vmErr != nil {
			return vmErr
		}
	}
	for i, b := range data {
		if vmErr := vm.initRunElem(frame, obj, oldLen+i, MakeInt(int64(b), obj.ArrElemType)); vmErr != nil {
			return vmErr
		}
	}
	return vm.setArrayLen(obj, newLen)
}

// dropRunPrefix releases the first count elements and moves the rest down.
//
// The shift is raw bytes, which leaves the tail holding stale duplicates of
// values that now live at lower indices. Shortening the length is what makes
// those slots DEAD: nothing reads them, and the teardown walks the length, so
// they are never released a second time.
func (vm *VM) dropRunPrefix(obj *Object, count int) *VMError {
	if count <= 0 {
		return nil
	}
	if count > obj.ArrLen {
		return vm.eb.outOfBounds(count, obj.ArrLen)
	}
	for i := range count {
		ref, vmErr := vm.runElemSlot(obj, i)
		if vmErr != nil {
			return vmErr
		}
		if vmErr := vm.dropMember(ref); vmErr != nil {
			return vmErr
		}
	}
	newLen := obj.ArrLen - count
	if newLen > 0 {
		stride, err := vm.runStride(obj.ArrElemType)
		if err != nil {
			return vm.eb.makeError(PanicUnimplemented, err.Error())
		}
		span, ok := mulChecked(stride, safeUint64FromInt(newLen))
		if !ok {
			return vm.eb.makeError(PanicUnimplemented, "an element run overflows while shifting")
		}
		src, vmErr := vm.runElemSlot(obj, count)
		if vmErr != nil {
			return vmErr
		}
		dst, vmErr := vm.runElemSlot(obj, 0)
		if vmErr != nil {
			return vmErr
		}
		from, resolveErr := src.resolve(span)
		if resolveErr != nil {
			return vm.eb.makeError(PanicUnimplemented, resolveErr.Error())
		}
		to, resolveErr := dst.resolve(span)
		if resolveErr != nil {
			return vm.eb.makeError(PanicUnimplemented, resolveErr.Error())
		}
		copy(to, from)
	}
	return vm.setArrayLen(obj, newLen)
}
