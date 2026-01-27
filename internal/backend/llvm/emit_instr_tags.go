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
	_, meta, errTagPayload = fe.emitter.tagCaseMeta(typeID, tp.TagName, symbols.NoSymbolID)
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
	payloadOffsets, errPayloadOffsets := fe.emitter.payloadOffsets(meta.PayloadTypes)
	if errPayloadOffsets != nil {
		return "", "", errPayloadOffsets
	}
	offset := layoutInfo.PayloadOffset + payloadOffsets[tp.Index]
	var (
		basePtr string
		baseTy  string
	)
	basePtr, baseTy, errTagPayload = fe.emitValueOperand(&tp.Value)
	if errTagPayload != nil {
		return "", "", errTagPayload
	}
	if isRefType(fe.emitter.types, tp.Value.Type) {
		if baseTy != "ptr" {
			return "", "", fmt.Errorf("tag payload requires ptr base, got %s", baseTy)
		}
		deref := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", deref, basePtr)
		basePtr = deref
		baseTy = "ptr"
	}
	if baseTy != "ptr" {
		return "", "", fmt.Errorf("tag payload requires ptr base, got %s", baseTy)
	}
	payloadType := meta.PayloadTypes[tp.Index]
	payloadLLVM, errPayloadLLVM := llvmValueType(fe.emitter.types, payloadType)
	if errPayloadLLVM != nil {
		return "", "", errPayloadLLVM
	}
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, basePtr, offset)
	operandIsRef := isRefType(fe.emitter.types, tp.Value.Type)
	payloadIsRef := isRefType(fe.emitter.types, payloadType)
	if operandIsRef && !payloadIsRef {
		return bytePtr, "ptr", nil
	}
	val = fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", val, payloadLLVM, bytePtr)
	return val, payloadLLVM, nil
}
