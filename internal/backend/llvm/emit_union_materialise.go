package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// emitStructStorageZero clears a struct literal's storage before a PARTIAL
// literal writes the fields it does name. It is deliberately separate from the
// union initializer next door: that one also enforces a discriminant's minimum
// size, which is a union's rule and not a struct's.
func (fe *funcEmitter) emitStructStorageZero(dst string, size, align uint64) error {
	if fe == nil || fe.emitter == nil {
		return fmt.Errorf("missing emitter for struct storage initialization")
	}
	if size == 0 {
		return nil
	}
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&fe.emitter.buf,
		"  call void @llvm.memset.p0.i64(ptr align %d %s, i8 0, i64 %d, i1 false)\n",
		align, dst, size)
	return nil
}

// unionZeroStoreLimit divides the two ways a union's storage is cleared. Below
// it the bytes are written as word stores; from it up, as one llvm.memset.
//
// The object step compiles the IR unoptimised (`clang -c -x ir`, no -O, in
// buildpipeline), and at that level every memset intrinsic is a call into
// libc whatever its size: the carrier bench's array-teardown row, which
// materialises one Option per operation, read +21 ns per operation against a
// base that wrote its Option with plain stores. A word store is one
// instruction, and the storage sizes the union layouts produce are a few of
// them.
const unionZeroStoreLimit = 256

func (fe *funcEmitter) emitUnionStorageInit(dst string, size, align uint64) error {
	if fe == nil || fe.emitter == nil {
		return fmt.Errorf("missing emitter for union storage initialization")
	}
	if size < 4 {
		return fmt.Errorf("union storage is %d bytes, need at least the 4-byte discriminant", size)
	}
	if align == 0 {
		align = 1
	}
	if size >= unionZeroStoreLimit {
		fmt.Fprintf(&fe.emitter.buf,
			"  call void @llvm.memset.p0.i64(ptr align %d %s, i8 0, i64 %d, i1 false)\n",
			align, dst, size)
		return nil
	}
	fe.emitZeroWordStores(dst, size, align)
	return nil
}

// emitZeroWordStores writes `size` zero bytes at dst as the widest stores the
// alignment admits (at most 8 bytes each), narrowing only for a tail the
// layout would not normally leave.
func (fe *funcEmitter) emitZeroWordStores(dst string, size, align uint64) {
	width := min(align, 8)
	if width == 0 {
		width = 1
	}
	for offset := uint64(0); offset < size; {
		store := width
		for store > 1 && offset+store > size {
			store /= 2
		}
		ptr := dst
		if offset != 0 {
			ptr = fe.nextTemp()
			fmt.Fprintf(&fe.emitter.buf,
				"  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", ptr, dst, offset)
		}
		fmt.Fprintf(&fe.emitter.buf, "  store i%d 0, ptr %s, align %d\n", store*8, ptr, store)
		offset += store
	}
}

func (fe *funcEmitter) emitUnionDiscriminant(dst string, size, align uint64, caseIndex int) error {
	if initErr := fe.emitUnionStorageInit(dst, size, align); initErr != nil {
		return initErr
	}
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s, align %d\n", caseIndex, dst, align)
	return nil
}

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
	// Start from deterministic bytes so padding and the inactive arms never
	// inherit whatever happened to occupy this destination before it was empty.
	if initErr := fe.emitUnionDiscriminant(
		dstPtr, facts.Size, memberAccessAlign(dstAlign, 0), member.PhysicalCaseIndex,
	); initErr != nil {
		return false, initErr
	}

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

	// A composite member IS its bytes. Storing the value here would store the
	// ADDRESS of the storage holding those bytes, and the union would then be
	// carrying a pointer where its own payload belongs -- which the drop walk
	// reads as the composite itself and frees fields out of an alloca.
	if memberFacts, ferr := fe.emitter.layoutOf(member.BareType); ferr == nil && fe.emitter.hasInlineStorage(member.BareType) {
		memberAlign := memberFacts.Align
		if memberAlign == 0 {
			memberAlign = 1
		}
		fe.emitter.emitGlueStorageCopy(payloadPtr, val, memberFacts.Size, memberAlign)
		return true, nil
	}
	fe.emitValueStore(valTy, val, payloadPtr, payloadAlign)
	return true, nil
}

// emitUnionNarrowToBareMember reads a bare member back out of the union it was
// widened into.
//
// It is the exact inverse of emitUnionMaterialiseBareMember, and it has to
// exist for the same reason that one does. MIR spells the binding in a
// compare arm as a plain move -- `L5 = move L0`, where L0 is the union and L5
// is the member -- because MIR is representation-neutral and does not know
// where a payload sits. The backend does, so the backend narrows.
//
// Without this the union's ADDRESS was handed on as if it were the member's
// value: for a `string` arm that meant a string handle pointing at the
// discriminant, which the first use freed as though it were a string.
func (fe *funcEmitter) emitUnionNarrowToBareMember(
	srcPtr string,
	srcAlign uint64,
	unionType types.TypeID,
	memberType types.TypeID,
) (payload, payloadLLVM string, narrowed bool, err error) {
	if fe == nil || fe.emitter == nil {
		return "", "", false, nil
	}
	unionType = resolveValueType(fe.emitter.types, unionType)
	if !isUnionType(fe.emitter.types, unionType) {
		return "", "", false, nil
	}
	member, ok := fe.emitter.unionMemberFor(unionType, memberType)
	if !ok {
		return "", "", false, nil
	}
	_, facts, err := fe.emitter.unionCases(unionType)
	if err != nil {
		return "", "", false, err
	}
	caseLayout, ok := facts.UnionCase(member.PhysicalCaseIndex)
	if !ok {
		return "", "", false, fmt.Errorf(
			"union type#%d has no physical case %d for member %s",
			unionType, member.PhysicalCaseIndex, member.Name)
	}
	offsets := caseLayout.FieldOffsets()
	if len(offsets) != 1 {
		return "", "", false, fmt.Errorf(
			"bare member %s of type#%d has %d payload offsets, want exactly one",
			member.Name, unionType, len(offsets))
	}
	payloadOffset := caseLayout.PayloadOffset + offsets[0]
	payloadPtr := srcPtr
	if payloadOffset != 0 {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf,
			"  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", tmp, srcPtr, payloadOffset)
		payloadPtr = tmp
	}
	memberLLVM, err := fe.emitter.llvmType(memberType)
	if err != nil {
		return "", "", false, err
	}
	payloadAlign := memberAccessAlign(srcAlign, payloadOffset)
	if fe.emitter.hasInlineStorage(memberType) {
		// A composite member IS its bytes, so its storage is the address.
		return payloadPtr, memberLLVM, true, nil
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s, align %d\n",
		tmp, memberLLVM, payloadPtr, payloadAlign)
	return tmp, memberLLVM, true, nil
}
