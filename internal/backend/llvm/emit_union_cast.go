package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitUnionCast(val string, srcType, dstType types.TypeID) (outVal, outTy string, err error) {
	if fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	srcResolved := resolveValueType(fe.emitter.types, srcType)
	dstResolved := resolveValueType(fe.emitter.types, dstType)
	if srcResolved == dstResolved {
		return val, "ptr", nil
	}
	srcCases, err := fe.emitter.tagCases(srcResolved)
	if err != nil {
		return "", "", err
	}
	dstCases, err := fe.emitter.tagCases(dstResolved)
	if err != nil {
		return "", "", err
	}
	if isRefType(fe.emitter.types, srcType) {
		deref := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", deref, val)
		val = deref
	}
	tagVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i32, ptr %s\n", tagVal, val)
	resPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", resPtr)

	cont := fe.nextInlineBlock()
	def := fe.nextInlineBlock()
	castID := fe.inlineBlock
	fe.inlineBlock++
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [", tagVal, def)
	for i := range srcCases {
		fmt.Fprintf(&fe.emitter.buf, " i32 %d, label %%tagcast%d.%d", i, castID, i)
	}
	fmt.Fprintf(&fe.emitter.buf, " ]\n")

	srcLayout, err := fe.emitter.layoutOf(srcResolved)
	if err != nil {
		return "", "", err
	}
	for i, srcCase := range srcCases {
		fmt.Fprintf(&fe.emitter.buf, "tagcast%d.%d:\n", castID, i)
		dstIdx, dstCase, ok := matchTagCase(dstCases, srcCase)
		if !ok {
			fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")
			continue
		}
		if len(srcCase.PayloadTypes) != len(dstCase.PayloadTypes) {
			return "", "", fmt.Errorf("union cast payload mismatch for tag %q", srcCase.TagName)
		}
		payloadVals := make([]string, 0, len(srcCase.PayloadTypes))
		payloadLLVM := make([]string, 0, len(srcCase.PayloadTypes))
		if len(srcCase.PayloadTypes) > 0 {
			offsets, err := fe.emitter.payloadOffsets(srcCase.PayloadTypes)
			if err != nil {
				return "", "", err
			}
			for j, payloadType := range srcCase.PayloadTypes {
				srcLLVM, err := llvmValueType(fe.emitter.types, payloadType)
				if err != nil {
					return "", "", err
				}
				dstLLVM, err := llvmValueType(fe.emitter.types, dstCase.PayloadTypes[j])
				if err != nil {
					return "", "", err
				}
				if srcLLVM != dstLLVM {
					return "", "", fmt.Errorf("union cast payload type mismatch for tag %q", srcCase.TagName)
				}
				off := srcLayout.PayloadOffset + offsets[j]
				bytePtr := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, val, off)
				loaded := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = load %s, ptr %s\n", loaded, srcLLVM, bytePtr)
				srcPayload := resolveValueType(fe.emitter.types, payloadType)
				dstPayload := resolveValueType(fe.emitter.types, dstCase.PayloadTypes[j])
				if srcPayload != dstPayload && isUnionType(fe.emitter.types, srcPayload) && isUnionType(fe.emitter.types, dstPayload) {
					casted, castTy, err := fe.emitUnionCast(loaded, payloadType, dstCase.PayloadTypes[j])
					if err != nil {
						return "", "", err
					}
					loaded = casted
					srcLLVM = castTy
				}
				payloadVals = append(payloadVals, loaded)
				payloadLLVM = append(payloadLLVM, srcLLVM)
			}
		}
		newTag, err := fe.emitTagValueFromValues(dstResolved, dstIdx, dstCase.PayloadTypes, payloadVals, payloadLLVM)
		if err != nil {
			return "", "", err
		}
		fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", newTag, resPtr)
		fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", cont)
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", def)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	out := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", out, resPtr)
	return out, "ptr", nil
}

func matchTagCase(cases []mir.TagCaseMeta, src mir.TagCaseMeta) (int, mir.TagCaseMeta, bool) {
	if src.TagSym.IsValid() {
		for i, c := range cases {
			if c.TagSym == src.TagSym {
				return i, c, true
			}
		}
	}
	if src.TagName != "" {
		for i, c := range cases {
			if c.TagName == src.TagName {
				return i, c, true
			}
		}
	}
	return -1, mir.TagCaseMeta{}, false
}

func (fe *funcEmitter) emitTagValueFromValues(typeID types.TypeID, tagIndex int, payloadTypes []types.TypeID, payloadVals, payloadLLVM []string) (string, error) {
	if len(payloadTypes) != len(payloadVals) || len(payloadTypes) != len(payloadLLVM) {
		return "", fmt.Errorf("tag payload length mismatch")
	}
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
	mem := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", mem, size, align)
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s\n", tagIndex, mem)
	if len(payloadTypes) == 0 {
		return mem, nil
	}
	offsets, err := fe.emitter.payloadOffsets(payloadTypes)
	if err != nil {
		return "", err
	}
	for i := range payloadTypes {
		off := layoutInfo.PayloadOffset + offsets[i]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", payloadLLVM[i], payloadVals[i], bytePtr)
	}
	return mem, nil
}

func isUnionType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	id = resolveValueType(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindUnion
}
