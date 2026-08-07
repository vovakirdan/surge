package vm

import (
	"fmt"

	"surge/internal/symbols"
	"surge/internal/types"
)

// A tagged union in storage is a discriminant and one payload region its arms
// share. Which members exist is therefore a runtime question and not a static
// one, which is the whole reason unions cannot go through compositeMembers: a
// walk that assumed every arm's members were present would read the arm that is
// not live as though it were.
//
// The discriminant is an INDEX into the arms as the layout registry orders
// them, not a symbol. A symbol would have to be resolved against a tag layout
// on every read; an index is what the layout already agrees with the native
// backend about, and storageCaseIndexOf is the one place the two namings meet.

// buildTag builds a tagged union into its own extent, with one arm live.
//
// The type is required for the same reason every producer needs one — an extent
// has no size without it — and here it carries a second duty: the arm a symbol
// selects is a fact about the union type, so a constructor with no destination
// type has no arm to select either.
func (vm *VM) buildTag(frame *Frame, typeID types.TypeID, tagSym symbols.SymbolID, payload []Value) (Value, *VMError) {
	if typeID == types.NoTypeID {
		return Value{}, vm.eb.makeError(PanicTypeMismatch,
			"a tag constructor has no type of its own and its destination declares none")
	}
	shape, err := vm.unionMembers(typeID)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	index, err := vm.storageCaseIndexOf(typeID, shape, tagSym)
	if err != nil {
		return Value{}, vm.eb.unknownTagLayout(err.Error())
	}
	arm := shape.Cases[index].Payload
	if len(payload) != len(arm) {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf(
			"tag %q expects %d payload value(s), got %d", shape.Cases[index].TagName, len(arm), len(payload)))
	}
	dst, vmErr := vm.buildComposite(frame, typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	// Setting the arm clears the shared payload region, so what a previous
	// occupant of these bytes left behind cannot be read back as this arm's
	// payload. The writes below then fill an extent that owns nothing, which is
	// what makes each of them an initialization rather than a replacement.
	if err := vm.storageSetActiveCase(dst, shape, index); err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	for i, member := range arm {
		ref, projectErr := dst.memberRef(member)
		if projectErr != nil {
			return Value{}, vm.eb.makeError(PanicUnimplemented, projectErr.Error())
		}
		//nolint:gosec // the arm and the payload are the same length, refused above
		if vmErr := vm.storeStorage(frame, ref, payload[i]); vmErr != nil {
			return Value{}, vmErr
		}
	}
	return MakeComposite(dst), nil
}

// tagArm reports which arm of a union in storage is live, together with the
// shape that names the arms.
func (vm *VM) tagArm(ref StorageRef) (unionShape, int, *VMError) {
	shape, err := vm.unionMembers(ref.TypeID)
	if err != nil {
		return unionShape{}, 0, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	index, err := vm.storageActiveCase(ref, shape)
	if err != nil {
		return unionShape{}, 0, vm.eb.makeError(PanicRCUseAfterFree, err.Error())
	}
	return shape, index, nil
}

// tagArmName names the live arm of a union in storage.
func (vm *VM) tagArmName(ref StorageRef) (string, *VMError) {
	shape, index, vmErr := vm.tagArm(ref)
	if vmErr != nil {
		return "", vmErr
	}
	return shape.Cases[index].TagName, nil
}

// isTagStorage reports whether a value is a tagged union held in storage.
//
// Every caller that used to ask whether a value was a tag handle asks this
// instead, and the two questions are not quite the same: a union is now
// recognised by its TYPE having arms rather than by an object having been
// allocated as one, so a union that has never been constructed is still a
// union.
func (vm *VM) isTagStorage(v Value) (StorageRef, bool) {
	ref, ok := v.Storage()
	if !ok {
		return StorageRef{}, false
	}
	if _, err := vm.unionMembers(ref.TypeID); err != nil {
		return StorageRef{}, false
	}
	return ref, true
}

// tagPayloadRef projects one value out of a union's live arm.
//
// The arm is the live one and not the one the caller expected: a caller that
// wants a different arm has already been refused by the tag check above it, and
// projecting into an arm that is not live would read the live arm's bytes under
// the wrong types.
func (vm *VM) tagPayloadRef(ref StorageRef, index int) (StorageRef, *VMError) {
	shape, arm, vmErr := vm.tagArm(ref)
	if vmErr != nil {
		return StorageRef{}, vmErr
	}
	payload := shape.Cases[arm].Payload
	if index < 0 || index >= len(payload) {
		return StorageRef{}, vm.eb.tagPayloadIndexOutOfRange(index, len(payload))
	}
	member, err := ref.memberRef(payload[index])
	if err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return member, nil
}

// wrapTypeArm puts a value into the union arm that admits its TYPE.
//
// A type alternative — the `E` in `Erring<T, E> = Success(T) | E` — is an arm a
// value enters without naming it, because it carries no tag to name. So the arm
// is selected by the value's own type, which is the only thing that
// distinguishes one type alternative from another, and is exactly how the arm
// was named when the union's shape was described.
//
// Under a box this needed nothing: the slot held a handle and the handle was
// whatever it was allocated as, so `return Error { ... }` from a function
// returning `Erring` stored the error's own object and the union was a label.
// Exact layout has a discriminant and a payload region, and a value is not the
// union that admits it — the copy was refused, correctly, and this is the
// conversion that was missing on the other side of the refusal.
//
// It reports whether it applied, so a caller can go on asking its other
// questions when the value belongs to no type arm.
func (vm *VM) wrapTypeArm(frame *Frame, val Value, unionType types.TypeID) (Value, bool, *VMError) {
	if unionType == types.NoTypeID || val.TypeID == types.NoTypeID {
		return Value{}, false, nil
	}
	resolved := vm.valueType(unionType)
	if resolved == vm.valueType(val.TypeID) {
		return Value{}, false, nil
	}
	shape, err := vm.unionMembers(resolved)
	if err != nil {
		return Value{}, false, nil
	}
	want := typeArmName(vm.valueType(val.TypeID))
	index := -1
	for i := range shape.Cases {
		if shape.Cases[i].TagName == want {
			index = i
			break
		}
	}
	if index < 0 {
		return Value{}, false, nil
	}
	if len(shape.Cases[index].Payload) != 1 {
		return Value{}, false, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf(
			"union type#%d has a type arm carrying %d values, which is not a type arm",
			resolved, len(shape.Cases[index].Payload)))
	}
	dst, vmErr := vm.buildComposite(frame, unionType)
	if vmErr != nil {
		return Value{}, false, vmErr
	}
	if setErr := vm.storageSetActiveCase(dst, shape, index); setErr != nil {
		return Value{}, false, vm.eb.makeError(PanicUnimplemented, setErr.Error())
	}
	member, projectErr := dst.memberRef(shape.Cases[index].Payload[0])
	if projectErr != nil {
		return Value{}, false, vm.eb.makeError(PanicUnimplemented, projectErr.Error())
	}
	if vmErr := vm.storeStorage(frame, member, val); vmErr != nil {
		return Value{}, false, vmErr
	}
	return MakeComposite(dst), true, nil
}

// unwrapTypeArm reads a union's LIVE type arm out as the type that arm admits.
//
// It is the other half of wrapTypeArm, and it exists because a narrowing match
// on a type alternative binds the value, not the union: `compare self { err =>
// err.code }` gives `err` the type of the arm, and the binding's slot is
// declared as that type. A union written into it whole was refused by the copy,
// and rightly — the two have different layouts.
//
// The arm has to be the LIVE one. A union whose discriminant selects a
// different arm is not a value of this type at all, and answering with its
// bytes read under the wrong type is the one thing exact layout must never do.
//
// The payload is TAKEN rather than copied. This is reached only from a slot
// write, so the binding is about to own what it gets, and the narrowing form
// that reaches it — a catch-all binding over an owned scrutinee — consumes the
// union: its slot is flagged moved and its storage is never walked again, so a
// copy here would leave the arm's contents with nothing that releases them.
// Taking empties the arm in the same step, which also keeps a union that IS
// still walked from releasing what the binding now holds.
func (vm *VM) unwrapTypeArm(frame *Frame, src StorageRef, expectedType types.TypeID) (Value, bool, *VMError) {
	if expectedType == types.NoTypeID {
		return Value{}, false, nil
	}
	resolved := vm.valueType(expectedType)
	if resolved == vm.valueType(src.TypeID) {
		return Value{}, false, nil
	}
	shape, arm, vmErr := vm.tagArm(src)
	if vmErr != nil {
		return Value{}, false, nil
	}
	live := shape.Cases[arm]
	if live.TagName != typeArmName(resolved) || len(live.Payload) != 1 {
		return Value{}, false, nil
	}
	member, projectErr := src.memberRef(live.Payload[0])
	if projectErr != nil {
		return Value{}, false, vm.eb.makeError(PanicUnimplemented, projectErr.Error())
	}
	value, vmErr := vm.takeMember(frame, member)
	return value, true, vmErr
}

// retagUnion converts a union in storage to a WIDER union that has the same
// arm, moving the payload across.
//
// Under a box this was a relabel: the object carried whatever type it was
// allocated with, so widening `Option` into `Option | Error` meant writing a
// different TypeID onto it and changing nothing else. Exact layout has no such
// move available. The two unions have their own discriminant orders and their
// own payload offsets, so the same arm sits at a different place in each, and
// rewriting the type would leave a walk reading one union's bytes through the
// other's layout.
//
// So the conversion is real: find the arm by NAME in the destination, and move
// each payload value into the destination's offset for it. The members MOVE —
// taken out of the source and stored into the destination in one step each —
// which is what keeps the counts unchanged and leaves the source owning
// nothing it has given away.
func (vm *VM) retagUnion(frame *Frame, src StorageRef, dstType types.TypeID) (Value, bool, *VMError) {
	if dstType == types.NoTypeID || vm.valueType(dstType) == vm.valueType(src.TypeID) {
		return Value{}, false, nil
	}
	srcShape, srcArm, vmErr := vm.tagArm(src)
	if vmErr != nil {
		return Value{}, false, nil
	}
	dstShape, err := vm.unionMembers(dstType)
	if err != nil {
		return Value{}, false, nil
	}
	name := srcShape.Cases[srcArm].TagName
	dstArm := -1
	for i := range dstShape.Cases {
		if dstShape.Cases[i].TagName == name {
			dstArm = i
			break
		}
	}
	if dstArm < 0 {
		return Value{}, false, nil
	}
	srcPayload := srcShape.Cases[srcArm].Payload
	dstPayload := dstShape.Cases[dstArm].Payload
	if len(srcPayload) != len(dstPayload) {
		return Value{}, false, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf(
			"arm %q carries %d value(s) in type#%d and %d in type#%d",
			name, len(srcPayload), src.TypeID, len(dstPayload), dstType))
	}
	dst, vmErr := vm.buildComposite(frame, dstType)
	if vmErr != nil {
		return Value{}, false, vmErr
	}
	if err := vm.storageSetActiveCase(dst, dstShape, dstArm); err != nil {
		return Value{}, false, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	for i := range srcPayload {
		from, projectErr := src.memberRef(srcPayload[i])
		if projectErr != nil {
			return Value{}, false, vm.eb.makeError(PanicUnimplemented, projectErr.Error())
		}
		to, projectErr := dst.memberRef(dstPayload[i])
		if projectErr != nil {
			return Value{}, false, vm.eb.makeError(PanicUnimplemented, projectErr.Error())
		}
		taken, vmErr := vm.takeMember(frame, from)
		if vmErr != nil {
			return Value{}, false, vmErr
		}
		if vmErr := vm.storeStorage(frame, to, taken); vmErr != nil {
			return Value{}, false, vmErr
		}
	}
	return MakeComposite(dst), true, nil
}

// buildStruct builds a struct, a tuple or a fixed array into its own extent
// from values the caller already has.
//
// This is what the intrinsics use. They differ from a literal only in where the
// members came from, so they share the discipline: the members MOVE into the
// composite, and after this returns the composite is the only owner of what it
// was given.
func (vm *VM) buildStruct(frame *Frame, typeID types.TypeID, fields []Value) (Value, *VMError) {
	if typeID == types.NoTypeID {
		return Value{}, vm.eb.makeError(PanicTypeMismatch,
			"a composite cannot be built without a type to lay it out")
	}
	members, err := vm.compositeMembers(typeID)
	if err != nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if len(fields) > len(members) {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf(
			"type#%d has %d members and was given %d", typeID, len(members), len(fields)))
	}
	dst, vmErr := vm.buildComposite(frame, typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	// A caller may supply fewer values than the type has members. The extent is
	// already zeroed, so the members it did not name stay zero rather than
	// holding anything, which is the same state a literal leaves a field it did
	// not mention in.
	for i := range fields {
		if fields[i].Kind == VKInvalid {
			continue
		}
		ref, projectErr := dst.memberRef(members[i])
		if projectErr != nil {
			return Value{}, vm.eb.makeError(PanicUnimplemented, projectErr.Error())
		}
		if vmErr := vm.storeStorage(frame, ref, fields[i]); vmErr != nil {
			return Value{}, vmErr
		}
	}
	return MakeComposite(dst), nil
}
