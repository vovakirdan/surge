package vm

import (
	"fmt"

	"surge/internal/symbols"
	"surge/internal/types"
)

// storageRefAt names a value inside an arena, taking its proven alignment from
// the layout registry rather than from the caller.
func (vm *VM) storageRefAt(arena *Arena, offset uint64, typeID types.TypeID) (StorageRef, error) {
	if vm == nil || vm.Layouts == nil {
		return StorageRef{}, fmt.Errorf("storage: no finalized layout registry")
	}
	facts, err := vm.Layouts.Require(typeID)
	if err != nil {
		return StorageRef{}, fmt.Errorf("storage: type#%d has no usable layout: %w", typeID, err)
	}
	align := facts.Align
	if align == 0 {
		align = 1
	}
	return StorageRef{Arena: arena, Offset: offset, TypeID: typeID, Gen: arena.Generation(), Align: align}, nil
}

// sizeOf returns the layout size of a type.
func (vm *VM) storageSizeOf(typeID types.TypeID) (uint64, error) {
	facts, err := vm.Layouts.Require(typeID)
	if err != nil {
		return 0, fmt.Errorf("storage: type#%d has no usable layout: %w", typeID, err)
	}
	return facts.Size, nil
}

// memberRef projects one member of a composite, keeping the owner identity and
// the generation of the value it comes from.
func (r StorageRef) memberRef(member storageMember) (StorageRef, error) {
	return r.field(member.Offset, member.TypeID, member.Align)
}

// storageZero clears a value's whole extent, including its padding and any
// bytes an inactive union arm left behind.
//
// Zeroing padding is not tidiness. A copy walks members, so a hole that kept
// the bytes of whatever occupied the storage before would be invisible to every
// language-level read and visible to a memory comparison, and the two would
// disagree about whether two equal values are equal.
func (vm *VM) storageZero(ref StorageRef) error {
	size, err := vm.storageSizeOf(ref.TypeID)
	if err != nil {
		return err
	}
	bytes, err := ref.resolve(size)
	if err != nil {
		return err
	}
	for i := range bytes {
		bytes[i] = 0
	}
	return nil
}

// storageWriteCell encodes one non-composite value into its own extent.
func (vm *VM) storageWriteCell(ref StorageRef, member storageMember, v Value) error {
	if member.Kind == cellComposite {
		return fmt.Errorf("storage: type#%d is composite and must be walked, not encoded", member.TypeID)
	}
	dst, err := ref.resolve(member.Size)
	if err != nil {
		return err
	}
	if member.Kind == cellZeroSized {
		return nil
	}
	bits, err := vm.cellBits(ref.Arena, member.Kind, v)
	if err != nil {
		return fmt.Errorf("%w (type#%d)", err, member.TypeID)
	}
	putBits(dst, bits)
	return nil
}

// cellBits turns a value into the bits its cell holds.
func (vm *VM) cellBits(arena *Arena, kind cellKind, v Value) (uint64, error) {
	switch kind {
	case cellBool:
		if v.Kind != VKBool {
			return 0, fmt.Errorf("storage: %s is not a bool", v.Kind)
		}
		if v.Bool {
			return 1, nil
		}
		return 0, nil
	case cellSignedInt, cellUnsignedInt:
		if v.Kind != VKInt {
			return 0, fmt.Errorf("storage: %s is not a sized integer", v.Kind)
		}
		return uint64(v.Int), nil //nolint:gosec // the extent decides how many bits survive
	case cellTaggedWord:
		return taggedWordBits(v)
	case cellHandle:
		return handleBits(v)
	case cellFunc:
		return funcBits(v)
	case cellRefIndex:
		switch v.Kind {
		case VKRef, VKRefMut, VKPtr:
			return arena.addRef(v.Loc)
		case VKInvalid, VKNothing:
			return 0, nil
		default:
			return 0, fmt.Errorf("storage: %s is not a reference", v.Kind)
		}
	default:
		return 0, fmt.Errorf("storage: a %s cell has no encoding", kind)
	}
}

// storageReadCell decodes one non-composite value out of its own extent.
func (vm *VM) storageReadCell(ref StorageRef, member storageMember) (Value, error) {
	if member.Kind == cellComposite {
		return Value{}, fmt.Errorf("storage: type#%d is composite and must be walked, not decoded", member.TypeID)
	}
	src, err := ref.resolve(member.Size)
	if err != nil {
		return Value{}, err
	}
	switch member.Kind {
	case cellZeroSized:
		return MakeNothing(), nil
	case cellBool:
		return MakeBool(src[0] != 0, member.TypeID), nil
	case cellSignedInt:
		return MakeInt(signedBits(src), member.TypeID), nil
	case cellUnsignedInt:
		return MakeInt(int64(unsignedBits(src)), member.TypeID), nil //nolint:gosec // the type carries the signedness
	case cellTaggedWord:
		value, handle, inline := decodeTaggedWord(unsignedBits(src))
		if inline {
			return MakeInt(value, member.TypeID), nil
		}
		return vm.handleValue(handle, member.TypeID)
	case cellHandle:
		return vm.handleValue(Handle(unsignedBits(src)), member.TypeID) //nolint:gosec // handles are 32-bit
	case cellFunc:
		return MakeFunc(decodeFuncBits(unsignedBits(src)), member.TypeID), nil
	case cellRefIndex:
		loc, ok := ref.Arena.refAt(unsignedBits(src))
		if !ok {
			return Value{}, fmt.Errorf("storage: type#%d holds no referent", member.TypeID)
		}
		if loc.IsMut {
			return MakeRefMut(loc, member.TypeID), nil
		}
		return MakeRef(loc, member.TypeID), nil
	default:
		return Value{}, fmt.Errorf("storage: a %s cell has no encoding", member.Kind)
	}
}

// handleValue rebuilds a handle-backed value, taking its kind from the object
// the handle names rather than from the static type, so a member declared as an
// alias still reads back as what it is.
func (vm *VM) handleValue(handle Handle, typeID types.TypeID) (Value, error) {
	if handle == 0 {
		return MakeNothing(), nil
	}
	obj, ok := vm.Heap.lookup(handle)
	if !ok || obj == nil {
		return Value{}, fmt.Errorf("storage: handle %d names no object", handle)
	}
	if obj.Freed || obj.RefCount == 0 {
		return Value{}, fmt.Errorf("storage: handle %d (alloc=%d) is already released", handle, obj.AllocID)
	}
	switch obj.Kind {
	case OKString:
		return MakeHandleString(handle, typeID), nil
	case OKArray, OKArraySlice:
		return MakeHandleArray(handle, typeID), nil
	case OKMap:
		return MakeHandleMap(handle, typeID), nil
	case OKRange:
		return MakeHandleRange(handle, typeID), nil
	case OKBigInt:
		return MakeBigInt(handle, typeID), nil
	case OKBigUint:
		return MakeBigUint(handle, typeID), nil
	case OKBigFloat:
		return MakeBigFloat(handle, typeID), nil
	default:
		return Value{}, fmt.Errorf("storage: handle %d names a %v, which inline storage does not carry", handle, obj.Kind)
	}
}

// storageCopy duplicates a value into another extent so that mutating either
// one is invisible through the other.
//
// The walk is type-directed, member by member. A nested composite recurses so
// each level gets its own bytes; a handle-backed member counts a reference,
// because it governs its own lifetime and inventing a deep copy for it would
// change what those types mean; everything else copies its bits. The
// destination is zeroed first, so a copy into storage that held something
// larger cannot leave a tail of the old value behind.
func (vm *VM) storageCopy(dst, src StorageRef) error {
	if dst.TypeID != src.TypeID {
		return fmt.Errorf("storage: cannot copy type#%d into type#%d", src.TypeID, dst.TypeID)
	}
	if err := vm.storageZero(dst); err != nil {
		return err
	}
	return vm.copyWalk(dst, src)
}

func (vm *VM) copyWalk(dst, src StorageRef) error {
	if shape, err := vm.unionMembers(src.TypeID); err == nil {
		return vm.copyUnion(dst, src, shape)
	}
	members, err := vm.compositeMembers(src.TypeID)
	if err != nil {
		return err
	}
	for _, member := range members {
		if err := vm.copyMember(dst, src, member); err != nil {
			return err
		}
	}
	return nil
}

func (vm *VM) copyMember(dst, src StorageRef, member storageMember) error {
	dstMember, err := dst.memberRef(member)
	if err != nil {
		return err
	}
	srcMember, err := src.memberRef(member)
	if err != nil {
		return err
	}
	if member.Kind == cellComposite {
		return vm.copyWalk(dstMember, srcMember)
	}
	value, err := vm.storageReadCell(srcMember, member)
	if err != nil {
		return err
	}
	retained, vmErr := vm.cloneForShare(value)
	if vmErr != nil {
		return fmt.Errorf("storage: %s", vmErr.Message)
	}
	return vm.storageWriteCell(dstMember, member, retained)
}

func (vm *VM) copyUnion(dst, src StorageRef, shape unionShape) error {
	index, err := vm.storageActiveCase(src, shape)
	if err != nil {
		return err
	}
	if err := vm.storageSetActiveCase(dst, shape, index); err != nil {
		return err
	}
	for _, member := range shape.Cases[index].Payload {
		if err := vm.copyMember(dst, src, member); err != nil {
			return err
		}
	}
	return nil
}

// storageDrop releases everything one value owns, member by member, and leaves
// its extent zeroed so a second drop finds nothing to release twice.
func (vm *VM) storageDrop(ref StorageRef) error {
	if err := vm.dropWalk(ref); err != nil {
		return err
	}
	return vm.storageZero(ref)
}

func (vm *VM) dropWalk(ref StorageRef) error {
	if shape, err := vm.unionMembers(ref.TypeID); err == nil {
		index, err := vm.storageActiveCase(ref, shape)
		if err != nil {
			return err
		}
		return vm.dropMembers(ref, shape.Cases[index].Payload)
	}
	members, err := vm.compositeMembers(ref.TypeID)
	if err != nil {
		return err
	}
	return vm.dropMembers(ref, members)
}

func (vm *VM) dropMembers(ref StorageRef, members []storageMember) error {
	for _, member := range members {
		memberRef, err := ref.memberRef(member)
		if err != nil {
			return err
		}
		switch member.Kind {
		case cellComposite:
			if err := vm.dropWalk(memberRef); err != nil {
				return err
			}
		case cellHandle, cellTaggedWord:
			value, err := vm.storageReadCell(memberRef, member)
			if err != nil {
				return err
			}
			vm.dropValue(value)
		default:
		}
	}
	return nil
}

// storageActiveCase reads which arm of a union is live.
func (vm *VM) storageActiveCase(ref StorageRef, shape unionShape) (int, error) {
	tag, err := ref.resolve(shape.TagSize)
	if err != nil {
		return 0, err
	}
	index := unsignedBits(tag)
	if index >= uint64(len(shape.Cases)) {
		return 0, fmt.Errorf("storage: type#%d has %d arms and its discriminant reads %d",
			ref.TypeID, len(shape.Cases), index)
	}
	return int(index), nil //nolint:gosec // bounded by the arm count just above
}

// storageSetActiveCase writes which arm of a union is live, clearing the
// payload region first so no byte of the arm being replaced survives into the
// new one.
func (vm *VM) storageSetActiveCase(ref StorageRef, shape unionShape, index int) error {
	if index < 0 || index >= len(shape.Cases) {
		return fmt.Errorf("storage: type#%d has no arm %d", ref.TypeID, index)
	}
	if err := vm.storageZero(ref); err != nil {
		return err
	}
	tag, err := ref.resolve(shape.TagSize)
	if err != nil {
		return err
	}
	putBits(tag, uint64(index)) //nolint:gosec // bounded by the arm count just above
	return nil
}

// storageCaseIndexOf maps a runtime tag symbol to the arm it selects, going
// through the tag layout so the two namings of an arm cannot drift.
func (vm *VM) storageCaseIndexOf(typeID types.TypeID, shape unionShape, tagSym symbols.SymbolID) (int, error) {
	tagLayout, vmErr := vm.tagLayoutFor(typeID)
	if vmErr != nil {
		return 0, fmt.Errorf("storage: %s", vmErr.Message)
	}
	tagCase, ok := tagLayout.CaseBySym(tagSym)
	if !ok {
		return 0, fmt.Errorf("storage: type#%d has no arm for tag symbol %d", typeID, tagSym)
	}
	for i := range shape.Cases {
		if shape.Cases[i].TagName == tagCase.TagName {
			return i, nil
		}
	}
	return 0, fmt.Errorf("storage: type#%d has no arm named %q", typeID, tagCase.TagName)
}
