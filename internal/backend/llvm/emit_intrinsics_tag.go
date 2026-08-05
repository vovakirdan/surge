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
	size := layoutInfo.Size
	align := layoutInfo.Align
	if size <= 0 {
		size = 1
	}
	if align <= 0 {
		align = 1
	}

	unionCase, ok := layoutInfo.UnionCase(tagIndex)
	if !ok {
		return "", fmt.Errorf("missing finalized union case %d for type#%d", tagIndex, typeID)
	}
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s\n", tagIndex, mem)
	if isNothingType(fe.emitter.types, payloadType) {
		return mem, nil
	}
	payloadOffset, ok := unionCase.FieldOffset(0)
	if !ok {
		return "", fmt.Errorf("missing finalized payload offset for type#%d case %d", typeID, tagIndex)
	}
	payloadLLVM, err := llvmValueType(fe.emitter.types, payloadType)
	if err != nil {
		return "", err
	}
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
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
	return mem, nil
}
