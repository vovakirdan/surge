package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// Materialising a union from one of its members.
//
// A union value is a discriminant plus a payload, and the discriminant is the
// DIRECT-MEMBER index — the position in types.UnionInfo.Members, which is the
// index the physical layout is enumerated by. That is the discipline the VM has
// always followed; the native lane is adopting it.
//
// Before this file, a value whose type was a BARE member of the union simply
// passed through as the union: no discriminant was written at all, and on
// assignment the destination's spelling won, so the union's whole size was
// memcpy'd out of whatever the member's handle pointed at. A heap string
// reached that way leaked, measured at 58 bytes (RV2-DEBT-233).

// unionMemberFor finds the direct member a value of this type would occupy.
//
// It matches on the RESOLVED type so an alias or an `own` wrapper does not hide
// the member, and it deliberately considers bare members only: a tag value
// arrives through the tag constructors, which know their own case.
func (e *Emitter) unionMemberFor(unionType, valueType types.TypeID) (mir.UnionCaseMeta, bool) {
	cases, _, err := e.unionCases(unionType)
	if err != nil {
		return mir.UnionCaseMeta{}, false
	}
	resolved := resolveValueType(e.types, valueType)
	if resolved == types.NoTypeID {
		return mir.UnionCaseMeta{}, false
	}
	for index := range cases {
		if cases[index].Kind != mir.UnionCaseBareType {
			continue
		}
		if resolveValueType(e.types, cases[index].BareType) == resolved {
			return cases[index], true
		}
	}
	return mir.UnionCaseMeta{}, false
}

// emitUnionMaterialiseBareMember writes a well-formed union into dstPtr from a
// value of one of its bare members: the direct-member index at offset 0, then
// the member's own value at that case's payload offset.
//
// It reports false when the value is not a bare member of that union, which
// leaves every other path exactly as it was.
func (fe *funcEmitter) emitUnionMaterialiseBareMember(
	dstPtr string,
	dstAlign uint64,
	unionType types.TypeID,
	val, valTy string,
	valueType types.TypeID,
) (bool, error) {
	if fe == nil || fe.emitter == nil {
		return false, nil
	}
	if !isUnionType(fe.emitter.types, resolveValueType(fe.emitter.types, unionType)) {
		return false, nil
	}
	member, ok := fe.emitter.unionMemberFor(unionType, valueType)
	if !ok {
		return false, nil
	}
	_, facts, err := fe.emitter.unionCases(unionType)
	if err != nil {
		return false, err
	}
	caseLayout, ok := facts.UnionCase(member.PhysicalCaseIndex)
	if !ok {
		return false, fmt.Errorf(
			"union type#%d has no physical case %d for member %s",
			unionType, member.PhysicalCaseIndex, member.Name)
	}
	offsets := caseLayout.FieldOffsets()
	if len(offsets) != 1 {
		return false, fmt.Errorf(
			"bare member %s of type#%d has %d payload offsets, want exactly one",
			member.Name, unionType, len(offsets))
	}

	// The discriminant. Its width is the layout engine's, fixed at 4 bytes.
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s, align %d\n",
		member.PhysicalCaseIndex, dstPtr, memberAccessAlign(dstAlign, 0))

	// The payload, at this case's own offset.
	payloadOffset := caseLayout.PayloadOffset + offsets[0]
	payloadPtr := dstPtr
	if payloadOffset != 0 {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", tmp, dstPtr, payloadOffset)
		payloadPtr = tmp
	}
	payloadAlign := memberAccessAlign(dstAlign, payloadOffset)
	fe.emitValueStore(valTy, val, payloadPtr, payloadAlign)
	return true, nil
}
