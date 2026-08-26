package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitTagTest(tt *mir.TagTest) (val, ty string, errTagTest error) {
	if tt == nil {
		return "", "", fmt.Errorf("nil tag test")
	}
	var tagVal string
	tagVal, errTagTest = fe.emitTagDiscriminant(&tt.Value)
	if errTagTest != nil {
		return "", "", errTagTest
	}
	typeID := tt.Value.Type
	if typeID == types.NoTypeID && tt.Value.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(tt.Value.Place); err == nil {
			typeID = baseType
		}
	}
	typeID = resolveValueType(fe.emitter.types, typeID)
	var idx int
	idx, errTagTest = fe.emitter.tagCaseIndex(typeID, tt.TagName, symbols.NoSymbolID)
	if errTagTest != nil {
		return "", "", errTagTest
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = icmp eq i32 %s, %d\n", tmp, tagVal, idx)
	return tmp, "i1", nil
}

func (fe *funcEmitter) emitTagPayload(tp *mir.TagPayload) (val, ty string, errTagPayload error) {
	if tp == nil {
		return "", "", fmt.Errorf("nil tag payload")
	}
	typeID := tp.Value.Type
	if typeID == types.NoTypeID && tp.Value.Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(tp.Value.Place); err == nil {
			typeID = baseType
		}
	}
	typeID = resolveValueType(fe.emitter.types, typeID)
	var meta mir.TagCaseMeta
	var caseIndex int
	caseIndex, meta, errTagPayload = fe.emitter.tagCaseMeta(typeID, tp.TagName, symbols.NoSymbolID)
	if errTagPayload != nil {
		return "", "", errTagPayload
	}
	if tp.Index < 0 || tp.Index >= len(meta.PayloadTypes) {
		return "", "", fmt.Errorf("tag payload index out of range")
	}
	layoutInfo, errLayout := fe.emitter.layoutOf(typeID)
	if errLayout != nil {
		return "", "", errLayout
	}
	unionCase, ok := layoutInfo.UnionCase(caseIndex)
	if !ok {
		return "", "", fmt.Errorf("missing finalized union case %d for type#%d", caseIndex, typeID)
	}
	payloadOffset, ok := unionCase.FieldOffset(tp.Index)
	if !ok {
		return "", "", fmt.Errorf("missing finalized payload offset %d for type#%d case %d", tp.Index, typeID, caseIndex)
	}
	offset := unionCase.PayloadOffset + payloadOffset
	var (
		basePtr string
		baseTy  string
	)
	// Same as the discriminant read next door: the operand already names the
	// union's storage, so there is nothing to dereference first.
	basePtr, baseTy, errTagPayload = fe.emitValueOperand(&tp.Value)
	if errTagPayload != nil {
		return "", "", errTagPayload
	}
	if baseTy != handleType {
		return "", "", fmt.Errorf("tag payload requires an addressed base, got %s", baseTy)
	}
	payloadType := meta.PayloadTypes[tp.Index]
	// The case metadata records payload types CANONICALIZED -- MIR's
	// canonicalType strips `&`, `*` and `own` -- so `Option<&Pair>` and
	// `Option<Pair>` carry the same payload type here while their storage
	// differs: a pointer in the first, the composite's bytes in the second.
	// Reading a reference payload as its pointee's storage answers with the
	// ADDRESS of the slot that holds the pointer, one indirection too many
	// (measured: `m[k].a` on a `Map<int, Pair>` read 0 through `__index`'s
	// `Option<&V>`). The union's own declaration still says which it is.
	payloadIsRef := fe.emitter.declaredTagPayloadIsRef(typeID, meta.TagName, tp.Index) ||
		isRefType(fe.emitter.types, payloadType)
	payloadLLVM := handleType
	if !payloadIsRef {
		var errPayloadLLVM error
		payloadLLVM, errPayloadLLVM = fe.emitter.llvmValueType(payloadType)
		if errPayloadLLVM != nil {
			return "", "", errPayloadLLVM
		}
	}
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, basePtr, offset)
	operandIsRef := isRefType(fe.emitter.types, tp.Value.Type)
	if operandIsRef && !payloadIsRef {
		return bytePtr, handleType, nil
	}
	unionAlign := layoutInfo.Align
	if unionAlign == 0 {
		unionAlign = 1
	}
	return fe.emitStorageMemberLoad(payloadLLVM, bytePtr, memberAccessAlign(unionAlign, offset))
}

// declaredTagPayloadIsRef answers whether the union's OWN declaration gives
// this case's payload as a reference or pointer, which the canonicalized case
// metadata no longer says. The tag is matched by name against the union's
// membership, where the instantiated argument types still carry their kind.
func (e *Emitter) declaredTagPayloadIsRef(unionType types.TypeID, tagName string, payloadIndex int) bool {
	if e == nil || e.types == nil || e.syms == nil || e.syms.Strings == nil || tagName == "" {
		return false
	}
	info, ok := e.types.UnionInfo(resolveValueType(e.types, unionType))
	if !ok || info == nil {
		return false
	}
	for _, member := range info.Members {
		if member.Kind != types.UnionMemberTag || payloadIndex < 0 || payloadIndex >= len(member.TagArgs) {
			continue
		}
		if name, found := e.syms.Strings.Lookup(member.TagName); !found || name != tagName {
			continue
		}
		id := resolveAliasAndOwn(e.types, member.TagArgs[payloadIndex])
		tt, found := e.types.Lookup(id)
		return found && (tt.Kind == types.KindReference || tt.Kind == types.KindPointer)
	}
	return false
}
