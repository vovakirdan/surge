package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (vm *VM) tagLayoutFor(typeID types.TypeID) (*TagLayout, *VMError) {
	if vm == nil {
		return nil, nil
	}
	if vm.tagLayouts == nil {
		return nil, vm.eb.unknownTagLayout("no tag layout provider")
	}
	typeID = vm.valueType(typeID)
	if typeID == types.NoTypeID {
		return nil, vm.eb.unknownTagLayout("invalid tag type")
	}
	layout, ok := vm.tagLayouts.Layout(typeID)
	if !ok || layout == nil {
		return nil, vm.eb.unknownTagLayout(fmt.Sprintf("missing tag layout for type#%d", typeID))
	}
	return layout, nil
}

// tagScrutinee is the union a tag operation inspects, together with how the
// operation came by it.
//
// Ownership is the field that matters and it is why this type exists. A
// composite reached THROUGH a reference is a borrow: the reference names bytes
// some slot owns, and the reader releasing them would destroy a live value
// while its owner still names it. A composite reached any other way came from
// evalOperand, which copies, and the reader owes its release. Under a box these
// were one case because both ended in a counted handle; under storage they are
// opposites, and the difference has to be carried rather than guessed.
type tagScrutineeOwnership uint8

const (
	tagScrutineeBorrowed tagScrutineeOwnership = iota
	tagScrutineeOwnsStorage
	tagScrutineeOwnsHeap
)

type tagScrutinee struct {
	storage   StorageRef
	heap      Handle
	kind      ValueKind
	ownership tagScrutineeOwnership
	isTag     bool
	viaRef    bool
	viaRefMut bool
}

// evalTagScrutinee evaluates the operand a tag operation inspects.
func (vm *VM) evalTagScrutinee(frame *Frame, op *mir.Operand) (tagScrutinee, *VMError) {
	val, vmErr := vm.evalOperand(frame, op)
	if vmErr != nil {
		return tagScrutinee{}, vmErr
	}
	s := tagScrutinee{kind: val.Kind}
	if val.Kind == VKRef || val.Kind == VKRefMut {
		s.viaRef = true
		s.viaRefMut = val.Kind == VKRefMut
		loaded, loadErr := vm.loadLocationRaw(val.Loc)
		if loadErr != nil {
			return tagScrutinee{}, loadErr
		}
		s.kind = loaded.Kind
		val = loaded
	}
	s.captureOwnership(val)
	if s.viaRef && s.ownership == tagScrutineeOwnsStorage {
		// A reference names storage its source still owns. Unlike a heap value,
		// exact storage has no count to retain, so this inspection only borrows it.
		s.ownership = tagScrutineeBorrowed
	}
	if s.viaRef && s.ownership == tagScrutineeOwnsHeap {
		vm.Heap.Retain(s.heap)
	}
	if s.kind == VKComposite {
		_, unionErr := vm.unionMembers(s.storage.TypeID)
		s.isTag = unionErr == nil
	}
	return s, nil
}

func (s *tagScrutinee) captureOwnership(value Value) {
	switch {
	case value.Kind == VKComposite:
		if storage, ok := value.Storage(); ok {
			s.storage = storage
			s.ownership = tagScrutineeOwnsStorage
		}
	case value.IsHeap() && value.H != 0:
		s.heap = value.H
		s.ownership = tagScrutineeOwnsHeap
	}
}

// release gives up whatever the scrutinee owns, and nothing it merely borrows.
func (vm *VM) releaseTagScrutinee(s tagScrutinee) {
	if vm == nil || vm.Heap == nil {
		return
	}
	switch s.ownership {
	case tagScrutineeOwnsStorage:
		vm.dropCompositeStorage(s.storage)
	case tagScrutineeOwnsHeap:
		vm.Heap.Release(s.heap)
	}
}

func (vm *VM) evalTagTest(frame *Frame, tt *mir.TagTest) (Value, *VMError) {
	if tt == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil tag_test")
	}
	s, vmErr := vm.evalTagScrutinee(frame, &tt.Value)
	if vmErr != nil {
		return Value{}, vmErr
	}
	defer vm.releaseTagScrutinee(s)
	if !s.isTag {
		return MakeBool(false, types.NoTypeID), nil
	}
	return vm.tagNameMatches(s.storage, tt.TagName)
}

// tagNameMatches reports whether the live arm of a union is the named one.
//
// The comparison is by arm NAME on both sides. Resolving the wanted name to a
// symbol through the tag layout and the live arm to a symbol through its object
// was two lookups to compare two things that are each already named — and the
// two namings could disagree, which is what the old fallback through a symbol's
// any-name existed to paper over. The layout gives the arms their names and the
// discriminant selects one of them, so there is one naming now.
func (vm *VM) tagNameMatches(ref StorageRef, want string) (Value, *VMError) {
	name, vmErr := vm.tagArmName(ref)
	if vmErr != nil {
		return Value{}, vmErr
	}
	return MakeBool(name == want, types.NoTypeID), nil
}

func (vm *VM) evalTagPayload(frame *Frame, tp *mir.TagPayload) (Value, *VMError) {
	if tp == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "nil tag_payload")
	}
	s, vmErr := vm.evalTagScrutinee(frame, &tp.Value)
	if vmErr != nil {
		return Value{}, vmErr
	}
	defer vm.releaseTagScrutinee(s)
	if !s.isTag {
		return Value{}, vm.eb.tagPayloadOnNonTag(s.kind.String())
	}
	// The arm the discriminant selects is the only arm whose members exist, so
	// a payload asked for under a different name is refused BEFORE anything is
	// projected. Reading it anyway would decode the live arm's bytes as the
	// types of an arm that is not there.
	live, vmErr := vm.tagArmName(s.storage)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if live != tp.TagName {
		return Value{}, vm.eb.tagPayloadTagMismatch(tp.TagName, live)
	}
	member, vmErr := vm.tagPayloadRef(s.storage, tp.Index)
	if vmErr != nil {
		return Value{}, vmErr
	}
	// Destructuring THROUGH a reference borrows the payload where it lies. The
	// binding then names the union's own bytes, so a write through it is a
	// write to the union — which is what matching on a `&mut` is for. A payload
	// that is itself a reference is excluded: handing back a reference to it
	// would add a level of indirection the pattern did not ask for.
	payloadIsRef := vm.Types != nil && member.TypeID != types.NoTypeID && isReferenceType(vm.Types, member.TypeID)
	if s.viaRef && !payloadIsRef {
		refType := types.NoTypeID
		if vm.Types != nil && member.TypeID != types.NoTypeID {
			refType = vm.Types.Intern(types.MakeReference(member.TypeID, s.viaRefMut))
		}
		loc := Location{Kind: LKStorage, Storage: member, IsMut: s.viaRefMut}
		if s.viaRefMut {
			return MakeRefMut(loc, refType), nil
		}
		return MakeRef(loc, refType), nil
	}
	// Otherwise the payload is READ, and a read is a copy. A composite gets its
	// own extent for the same reason a field read does; anything else is
	// decoded and counted.
	if vm.storageCellKind(member.TypeID) == cellComposite {
		return vm.duplicateComposite(frame, MakeComposite(member))
	}
	return vm.loadStorage(member)
}

func isReferenceType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	for range 32 {
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return false
		}
		if tt.Kind == types.KindAlias {
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID || target == id {
				return false
			}
			id = target
			continue
		}
		return tt.Kind == types.KindReference
	}
	return false
}

func (vm *VM) execSwitchTag(frame *Frame, st *mir.SwitchTagTerm) *VMError {
	if st == nil {
		return vm.eb.makeError(PanicUnimplemented, "nil switch_tag terminator")
	}
	s, vmErr := vm.evalTagScrutinee(frame, &st.Value)
	if vmErr != nil {
		return vmErr
	}
	defer vm.releaseTagScrutinee(s)
	if !s.isTag {
		return vm.eb.switchTagOnNonTag(s.kind.String())
	}
	live, vmErr := vm.tagArmName(s.storage)
	if vmErr != nil {
		return vmErr
	}

	target := st.Default
	decision := "default"
	for _, c := range st.Cases {
		if c.TagName == live {
			target = c.Target
			decision = c.TagName
			break
		}
	}

	if target == mir.NoBlockID {
		return vm.eb.switchTagMissingDefault()
	}
	if target < 0 || int(target) >= len(frame.Func.Blocks) {
		return vm.eb.switchTagMissingDefault()
	}

	if vm.Trace != nil {
		vm.Trace.TraceSwitchTagDecision(decision, target)
	}

	frame.BB = target
	frame.IP = 0
	return nil
}

func (vm *VM) callTagConstructor(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) (bool, *VMError) {
	if vm == nil || vm.tagLayouts == nil || call == nil {
		return false, nil
	}
	if call.Callee.Kind != mir.CalleeSym || !call.Callee.Sym.IsValid() {
		return false, nil
	}
	if !vm.tagLayouts.KnownTagSym(call.Callee.Sym) {
		return false, nil
	}

	args := make([]Value, len(call.Args))
	for i := range call.Args {
		val, vmErr := vm.evalOperand(frame, &call.Args[i])
		if vmErr != nil {
			return true, vmErr
		}
		args[i] = val
	}

	if !call.HasDst || !call.Dst.IsValid() {
		// The constructed value is unused. The arguments were still moved into
		// this call, so they are released here rather than built into a union
		// that nothing would ever read — which also means no type is needed for
		// a union that is never laid out.
		for _, arg := range args {
			vm.dropValue(arg)
		}
		return true, nil
	}

	localID := call.Dst.Local
	tagSym := vm.tagLayouts.CanonicalTagSym(call.Callee.Sym)
	tagVal, vmErr := vm.buildTag(frame, frame.Locals[localID].TypeID, tagSym, args)
	if vmErr != nil {
		return true, vmErr
	}
	if vmErr := vm.writeLocal(frame, localID, tagVal); vmErr != nil {
		return true, vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: localID,
			Name:    frame.Locals[localID].Name,
			Value:   tagVal,
		})
	}
	return true, nil
}
