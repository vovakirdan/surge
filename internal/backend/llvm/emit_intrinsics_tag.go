package llvm

import (
	"fmt"

	"surge/internal/types"
)

func (fe *funcEmitter) emitTagValueSinglePayload(typeID types.TypeID, tagIndex int, payloadType types.TypeID, val, valTy string, valType types.TypeID) (string, error) {
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return "", err
	}
	if layoutInfo.TagSize != 4 {
		return "", fmt.Errorf("unsupported tag size %d for type#%d", layoutInfo.TagSize, typeID)
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}

	unionCase, ok := layoutInfo.UnionCase(tagIndex)
	if !ok {
		return "", fmt.Errorf("missing finalized union case %d for type#%d", tagIndex, typeID)
	}
	mem, err := fe.emitStorageAlloca(typeID)
	if err != nil {
		return "", err
	}
	if initErr := fe.emitUnionDiscriminant(mem, layoutInfo.Size, align, tagIndex); initErr != nil {
		return "", initErr
	}
	if isNothingType(fe.emitter.types, payloadType) {
		return mem, nil
	}
	payloadOffset, ok := unionCase.FieldOffset(0)
	if !ok {
		return "", fmt.Errorf("missing finalized payload offset for type#%d case %d", typeID, tagIndex)
	}
	payloadStorage, err := fe.emitter.llvmValueType(payloadType)
	if err != nil {
		return "", err
	}
	payloadLLVM := operandTypeFor(payloadStorage)
	if valTy != payloadLLVM {
		casted, castTy, err := fe.coerceNumericValue(val, valTy, valType, payloadType)
		if err != nil {
			return "", err
		}
		val = casted
		valTy = castTy
	}
	if valTy != payloadLLVM {
		return "", fmt.Errorf("tag payload type mismatch for type#%d tag %d: expected %s, got %s", typeID, tagIndex, payloadLLVM, valTy)
	}
	off := unionCase.PayloadOffset + payloadOffset
	bytePtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
	fe.emitValueStore(payloadStorage, val, bytePtr, memberAccessAlign(align, off))
	return mem, nil
}
