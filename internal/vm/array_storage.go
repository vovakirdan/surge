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
