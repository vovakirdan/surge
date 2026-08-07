package vm

import (
	"fmt"

	"surge/internal/layout"
	"surge/internal/types"
)

// storageMember describes one member of a composite exactly as the layout
// registry sees it. Nothing here is inferred: the offset, the size and the
// alignment all come from PhysicalFacts, which is what keeps the VM and the
// native backend describing the same value.
type storageMember struct {
	Offset uint64
	Size   uint64
	Align  uint64
	TypeID types.TypeID
	Kind   cellKind
}

// unionCaseShape describes one arm of a tagged union: where its payload starts
// and what the arm carries. Only the ACTIVE arm's members exist at runtime, so
// the discriminant selects which of these a walk may touch.
type unionCaseShape struct {
	TagName string
	Payload []storageMember
}

// unionShape describes a tagged union's storage: a discriminant at offset zero
// and one payload region the arms share.
type unionShape struct {
	TagSize uint64
	Cases   []unionCaseShape
}

// storageCellKind classifies how one type is encoded inside its extent.
//
// The order matters. A runtime-owned handle is a nominal struct, so asking the
// structural question first would lay a string or a map out as an aggregate; a
// reference resolves to its pointee under some walks, so asking about the
// pointee would have the VM copying through a borrow. Both are settled before
// any structural case is considered, by the same predicates sema and the native
// backend use.
func (vm *VM) storageCellKind(typeID types.TypeID) cellKind {
	if vm == nil || vm.Types == nil || typeID == types.NoTypeID {
		return cellUnsupported
	}
	// The reference question is asked FIRST and through aliases only, because
	// valueType answers a different question: it says what a value IS, and to do
	// that it unwraps `own`, `&` and `*` alike. That is the right answer for
	// comparing two types and the wrong one for encoding a member — it turns
	// `&int` into `int`, which hands a reference an integer cell and a `&Held` a
	// composite one. Both were reached: a `&int` payload arrived at an unsized
	// integer cell, and a `&mut` read back as the composite it points at.
	if vm.isReferenceCell(typeID) {
		return cellRefIndex
	}
	resolved := vm.valueType(typeID)
	tt, ok := vm.Types.Lookup(resolved)
	if !ok {
		return cellUnsupported
	}
	switch tt.Kind {
	case types.KindFn:
		return cellFunc
	case types.KindUnit, types.KindNothing:
		return cellZeroSized
	case types.KindBool:
		return cellBool
	case types.KindFar:
		return cellHandle
	}
	if _, handleBacked := vm.Types.RuntimeHandlePayloads(resolved); handleBacked {
		return cellHandle
	}
	if vm.Types.IsValueComposite(resolved) {
		return cellComposite
	}
	switch tt.Kind {
	case types.KindFloat:
		// Every float the VM holds is an arbitrary-precision object behind a
		// handle, whatever width the layout gives its extent.
		return cellHandle
	case types.KindInt:
		if tt.Width == types.WidthAny {
			return cellTaggedWord
		}
		return cellSignedInt
	case types.KindUint:
		if tt.Width == types.WidthAny {
			return cellTaggedWord
		}
		return cellUnsignedInt
	case types.KindEnum:
		return cellUnsignedInt
	default:
		return cellUnsupported
	}
}

// isReferenceCell reports whether a member is itself a reference — as opposed
// to being the thing a reference points at.
//
// It resolves ALIASES and nothing else. `type IntRef = &int` is a reference and
// has to be seen through; `own T` and `&T` are not the same question and must
// not be unwrapped here, which is exactly where valueType cannot be used.
func (vm *VM) isReferenceCell(typeID types.TypeID) bool {
	tt, ok := vm.throughAliases(typeID)
	return ok && (tt.Kind == types.KindReference || tt.Kind == types.KindPointer)
}

// throughAliases looks a type up with ALIASES resolved and nothing else
// unwrapped, which is the lookup every question about a member's own shape
// needs. valueType is the other lookup — it says what a value IS and so
// unwraps `own`, `&` and `*` — and the two must not be confused.
func (vm *VM) throughAliases(typeID types.TypeID) (types.Type, bool) {
	id := typeID
	for range aliasResolutionLimit {
		tt, ok := vm.Types.Lookup(id)
		if !ok {
			return types.Type{}, false
		}
		if tt.Kind != types.KindAlias {
			return tt, true
		}
		target, ok := vm.Types.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return types.Type{}, false
		}
		id = target
	}
	return types.Type{}, false
}

// aliasResolutionLimit bounds an alias walk so a cycle the interner failed to
// reject ends in an answer rather than in a hang.
const aliasResolutionLimit = 32

// memberAt builds one member description from a type and an offset, refusing a
// type whose layout is not concrete rather than guessing an extent for it.
func (vm *VM) memberAt(offset uint64, typeID types.TypeID) (storageMember, error) {
	if vm.Layouts == nil {
		return storageMember{}, fmt.Errorf("storage: no finalized layout registry")
	}
	facts, err := vm.Layouts.Require(typeID)
	if err != nil {
		return storageMember{}, fmt.Errorf("storage: type#%d has no usable layout: %w", typeID, err)
	}
	kind := vm.storageCellKind(typeID)
	if kind == cellUnsupported {
		return storageMember{}, fmt.Errorf("storage: type#%d has no inline encoding", typeID)
	}
	if err := checkCellExtent(kind, facts.Size); err != nil {
		return storageMember{}, fmt.Errorf("%w (type#%d)", err, typeID)
	}
	align := facts.Align
	if align == 0 {
		align = 1
	}
	return storageMember{Offset: offset, Size: facts.Size, Align: align, TypeID: typeID, Kind: kind}, nil
}

// compositeMembers returns the members of a struct, a tuple or a fixed array in
// declaration order. A tagged union answers through unionMembers instead,
// because which of its members exist depends on the discriminant.
func (vm *VM) compositeMembers(typeID types.TypeID) ([]storageMember, error) {
	if vm == nil || vm.Types == nil || vm.Layouts == nil {
		return nil, fmt.Errorf("storage: the VM has no type or layout information")
	}
	resolved := vm.valueType(typeID)
	facts, err := vm.Layouts.Require(resolved)
	if err != nil {
		return nil, fmt.Errorf("storage: type#%d has no usable layout: %w", resolved, err)
	}

	if elem, length, ok := vm.Types.ArrayFixedInfo(resolved); ok {
		return vm.arrayMembers(elem, uint64(length))
	}
	tt, ok := vm.Types.Lookup(resolved)
	if !ok {
		return nil, fmt.Errorf("storage: type#%d is unknown", resolved)
	}
	if tt.Kind == types.KindArray && tt.Count != types.ArrayDynamicLength {
		return vm.arrayMembers(tt.Elem, uint64(tt.Count))
	}

	offsets := facts.FieldOffsets()
	var memberTypes []types.TypeID
	switch tt.Kind {
	case types.KindStruct:
		info, ok := vm.Types.StructInfo(resolved)
		if !ok || info == nil {
			return nil, fmt.Errorf("storage: struct type#%d has no field metadata", resolved)
		}
		memberTypes = make([]types.TypeID, len(info.Fields))
		for i := range info.Fields {
			memberTypes[i] = info.Fields[i].Type
		}
	case types.KindTuple:
		info, ok := vm.Types.TupleInfo(resolved)
		if !ok || info == nil {
			return nil, fmt.Errorf("storage: tuple type#%d has no element metadata", resolved)
		}
		memberTypes = append([]types.TypeID(nil), info.Elems...)
	default:
		return nil, fmt.Errorf("storage: type#%d is not an inline aggregate", resolved)
	}

	if len(offsets) != len(memberTypes) {
		return nil, fmt.Errorf("storage: type#%d has %d members but %d layout offsets",
			resolved, len(memberTypes), len(offsets))
	}
	// A member's alignment is the CONTAINER's business, not the member type's.
	// `@packed` removes the padding between fields, so a wide field lands at an
	// offset its own natural alignment does not divide — and the registry has
	// recorded that per field, because it is what computed it. Asking the member
	// type for its own alignment answers for a member standing alone, which for
	// a packed field is the wrong question: the access is underaligned by
	// construction, and refusing it rejects a program the language accepts.
	aligns := facts.FieldAligns()
	if len(aligns) != len(memberTypes) {
		return nil, fmt.Errorf("storage: type#%d has %d members but %d layout alignments",
			resolved, len(memberTypes), len(aligns))
	}
	members := make([]storageMember, len(memberTypes))
	for i, memberType := range memberTypes {
		member, err := vm.memberAt(offsets[i], memberType)
		if err != nil {
			return nil, err
		}
		if aligns[i] != 0 {
			member.Align = aligns[i]
		}
		members[i] = member
	}
	return members, nil
}

// arrayMembers lays a fixed array out at its element stride, which is the
// element's own size rounded to its alignment and never a size chosen here.
func (vm *VM) arrayMembers(elem types.TypeID, length uint64) ([]storageMember, error) {
	facts, err := vm.Layouts.Require(elem)
	if err != nil {
		return nil, fmt.Errorf("storage: array element type#%d has no usable layout: %w", elem, err)
	}
	members := make([]storageMember, 0, length)
	for i := range length {
		offset, ok := mulChecked(facts.Stride, i)
		if !ok {
			return nil, fmt.Errorf("storage: element %d of type#%d overflows its own array", i, elem)
		}
		member, err := vm.memberAt(offset, elem)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, nil
}

// unionMembers describes a tagged union's arms.
//
// Arms are matched to their layout cases by POSITION, which is sound because
// both come from UnionInfo.Members, and each arm is named so a walk can ask the
// tag layout which one the discriminant selects. An arm that carries no tag
// name — a bare `int | string` alternative — is refused rather than guessed at,
// because there is nothing to match a runtime discriminant against.
func (vm *VM) unionMembers(typeID types.TypeID) (unionShape, error) {
	resolved := vm.valueType(typeID)
	facts, err := vm.Layouts.Require(resolved)
	if err != nil {
		return unionShape{}, fmt.Errorf("storage: union type#%d has no usable layout: %w", resolved, err)
	}
	info, ok := vm.Types.UnionInfo(resolved)
	if !ok || info == nil {
		return unionShape{}, fmt.Errorf("storage: union type#%d has no member metadata", resolved)
	}
	cases := facts.UnionCases()
	if len(cases) != len(info.Members) {
		return unionShape{}, fmt.Errorf("storage: union type#%d has %d arms but %d layout cases",
			resolved, len(info.Members), len(cases))
	}

	shape := unionShape{TagSize: facts.TagSize, Cases: make([]unionCaseShape, len(cases))}
	for i := range info.Members {
		member := &info.Members[i]
		name, err := vm.unionArmName(resolved, member)
		if err != nil {
			return unionShape{}, err
		}
		payload, err := vm.unionArmPayload(member, cases[i])
		if err != nil {
			return unionShape{}, err
		}
		shape.Cases[i] = unionCaseShape{TagName: name, Payload: payload}
	}
	return shape, nil
}

// unionArmName names one arm so a discriminant can select it.
//
// All three arm kinds are named, because all three are arms the language
// writes and the physical layout gives each of them a case. A TAG is named by
// its tag; a NOTHING arm is named "nothing", which is the name the tag layout
// already gives it and the name coerceToSlotType already looks up; and a TYPE
// alternative — the `E` in `Success(T) | E` — is named by the type it admits,
// because that is the only thing that distinguishes it. Two spellings of one
// type resolve to one name, since the name is taken after alias resolution.
func (vm *VM) unionArmName(unionType types.TypeID, member *types.UnionMember) (string, error) {
	switch member.Kind {
	case types.UnionMemberNothing:
		return nothingArmName, nil
	case types.UnionMemberType:
		if member.Type == types.NoTypeID {
			return "", fmt.Errorf("storage: union type#%d has a type arm that admits no type", unionType)
		}
		return typeArmName(vm.valueType(member.Type)), nil
	case types.UnionMemberTag:
		if vm.Types.Strings == nil {
			return "", fmt.Errorf("storage: union type#%d has no string table for its arm names", unionType)
		}
		name, ok := vm.Types.Strings.Lookup(member.TagName)
		if !ok {
			return "", fmt.Errorf("storage: union type#%d has an arm whose name is not interned", unionType)
		}
		return name, nil
	default:
		return "", fmt.Errorf("storage: union type#%d has an arm of an unknown kind %d", unionType, member.Kind)
	}
}

// nothingArmName is what a nothing arm is called everywhere — the tag layout
// spells it this way and coerceToSlotType looks it up by this name.
const nothingArmName = "nothing"

// typeArmName is the name of a type alternative. It is deliberately not a name
// a tag could collide with: a tag name is an identifier, and this is not.
func typeArmName(resolved types.TypeID) string {
	return fmt.Sprintf("type#%d", resolved)
}

// unionArmPayload describes what one arm carries.
//
// The values an arm carries come from its KIND, not from TagArgs alone: a
// nothing arm carries none, a type alternative carries exactly the one value it
// admits, and only a tag carries its TagArgs. Reading TagArgs for all three
// described a type alternative as empty, so a copy or a drop of that arm walked
// past the value it holds.
func (vm *VM) unionArmPayload(member *types.UnionMember, layoutCase layout.UnionCaseLayout) ([]storageMember, error) {
	carried := member.TagArgs
	switch member.Kind {
	case types.UnionMemberNothing:
		carried = nil
	case types.UnionMemberType:
		carried = []types.TypeID{member.Type}
	case types.UnionMemberTag:
	default:
	}
	offsets := layoutCase.FieldOffsets()
	if len(offsets) != len(carried) {
		return nil, fmt.Errorf("storage: a union arm carries %d values but %d layout offsets",
			len(carried), len(offsets))
	}
	payload := make([]storageMember, len(carried))
	for i, arg := range carried {
		absolute, ok := addChecked(layoutCase.PayloadOffset, offsets[i])
		if !ok {
			return nil, fmt.Errorf("storage: a union payload offset overflows")
		}
		value, err := vm.memberAt(absolute, arg)
		if err != nil {
			return nil, err
		}
		payload[i] = value
	}
	return payload, nil
}

func mulChecked(a, b uint64) (uint64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	product := a * b
	if product/a != b {
		return 0, false
	}
	return product, true
}
