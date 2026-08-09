package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitUnionCast(val string, srcType, dstType types.TypeID) (outVal, outTy string, err error) {
	if fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	srcResolved := resolveValueType(fe.emitter.types, srcType)
	dstResolved := resolveValueType(fe.emitter.types, dstType)
	if srcResolved == dstResolved {
		return val, handleType, nil
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
		fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s, align %d\n", deref, val, alignPtr)
		val = deref
	}

	srcLayout, err := fe.emitter.layoutOf(srcResolved)
	if err != nil {
		return "", "", err
	}
	srcAlign := srcLayout.Align
	if srcAlign == 0 {
		srcAlign = 1
	}
	tagVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i32, ptr %s, align %d\n", tagVal, val, srcAlign)
	// The result is built in storage of its own, once, and each arm writes
	// into it. There is no box to hand back and no source envelope to free:
	// the source belongs to whoever declared it and outlives this cast.
	resPtr, err := fe.emitStorageAlloca(dstResolved)
	if err != nil {
		return "", "", err
	}
	cont := fe.nextInlineBlock()
	def := fe.nextInlineBlock()
	castID := fe.inlineBlock
	fe.inlineBlock++
	fmt.Fprintf(&fe.emitter.buf, "  switch i32 %s, label %%%s [", tagVal, def)
	for i := range srcCases {
		fmt.Fprintf(&fe.emitter.buf, " i32 %d, label %%tagcast%d.%d", i, castID, i)
	}
	fmt.Fprintf(&fe.emitter.buf, " ]\n")
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
			srcCaseLayout, ok := srcLayout.UnionCase(i)
			if !ok {
				return "", "", fmt.Errorf("missing finalized union case %d for type#%d", i, srcResolved)
			}
			offsets := srcCaseLayout.FieldOffsets()
			if len(offsets) != len(srcCase.PayloadTypes) {
				return "", "", fmt.Errorf("finalized union case %d for type#%d has %d payload offsets, want %d", i, srcResolved, len(offsets), len(srcCase.PayloadTypes))
			}
			for j, payloadType := range srcCase.PayloadTypes {
				srcPayload := resolveValueType(fe.emitter.types, payloadType)
				dstPayload := resolveValueType(fe.emitter.types, dstCase.PayloadTypes[j])
				if isNothingType(fe.emitter.types, srcPayload) {
					liftedVal, liftedLLVM, lifted, err := fe.emitNothingPayloadValue(dstPayload)
					if err != nil {
						return "", "", err
					}
					if lifted {
						payloadVals = append(payloadVals, liftedVal)
						payloadLLVM = append(payloadLLVM, liftedLLVM)
						continue
					}
				}
				srcLLVM, err := fe.emitter.llvmValueType(payloadType)
				if err != nil {
					return "", "", err
				}
				dstLLVM, err := fe.emitter.llvmValueType(dstCase.PayloadTypes[j])
				if err != nil {
					return "", "", err
				}
				// A payload that is itself widened below arrives in the
				// SOURCE's shape and leaves in the destination's, so the two
				// spellings differing is what that conversion is for. Asking
				// them to agree first would refuse the very case the arm loop
				// converts, and refusing is still right for every other way
				// two payload types can disagree.
				widensPayload := srcPayload != dstPayload &&
					isUnionType(fe.emitter.types, srcPayload) &&
					isUnionType(fe.emitter.types, dstPayload)
				if srcLLVM != dstLLVM && !widensPayload {
					return "", "", fmt.Errorf("union cast payload type mismatch for tag %q", srcCase.TagName)
				}
				off := srcCaseLayout.PayloadOffset + offsets[j]
				bytePtr := fe.nextTemp()
				fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, val, off)
				loaded, loadedTy, loadErr := fe.emitStorageMemberLoad(
					srcLLVM, bytePtr, memberAccessAlign(srcAlign, off))
				if loadErr != nil {
					return "", "", loadErr
				}
				srcLLVM = loadedTy
				if widensPayload {
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
		if err := fe.emitTagValueIntoStorage(
			resPtr, dstResolved, dstIdx, dstCase.PayloadTypes, payloadVals, payloadLLVM,
		); err != nil {
			return "", "", err
		}
		fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", cont)
	}

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", def)
	fmt.Fprintf(&fe.emitter.buf, "  unreachable\n")

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", cont)
	return resPtr, handleType, nil
}

func (fe *funcEmitter) emitNothingPayloadValue(dstType types.TypeID) (value, llvmTy string, ok bool, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return "", "", false, fmt.Errorf("missing emitter")
	}
	dstType = resolveValueType(fe.emitter.types, dstType)
	if isNothingType(fe.emitter.types, dstType) {
		return "0", "i8", true, nil
	}
	if !isUnionType(fe.emitter.types, dstType) {
		return "", "", false, nil
	}
	if _, _, lookupErr := fe.emitter.tagCaseMeta(dstType, "nothing", symbols.NoSymbolID); lookupErr != nil {
		return "", "", false, nil
	}
	ptr, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return "", "", false, err
	}
	return ptr, handleType, true, nil
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

// emitTagValueIntoStorage writes one tag and its payloads into storage the
// caller owns. Every arm of a cast writes into the same destination, which is
// what lets the cast produce one value rather than one box per arm.
func (fe *funcEmitter) emitTagValueIntoStorage(
	dst string, typeID types.TypeID, tagIndex int,
	payloadTypes []types.TypeID, payloadVals, payloadLLVM []string,
) error {
	if len(payloadTypes) != len(payloadVals) || len(payloadTypes) != len(payloadLLVM) {
		return fmt.Errorf("tag payload length mismatch")
	}
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return err
	}
	if layoutInfo.TagSize != 4 {
		return fmt.Errorf("unsupported tag size %d for type#%d", layoutInfo.TagSize, typeID)
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}
	fmt.Fprintf(&fe.emitter.buf, "  store i32 %d, ptr %s, align %d\n", tagIndex, dst, align)
	if len(payloadTypes) == 0 {
		return nil
	}
	unionCase, ok := layoutInfo.UnionCase(tagIndex)
	if !ok {
		return fmt.Errorf("missing finalized union case %d for type#%d", tagIndex, typeID)
	}
	offsets := unionCase.FieldOffsets()
	if len(offsets) != len(payloadTypes) {
		return fmt.Errorf("finalized union case %d for type#%d has %d payload offsets, want %d", tagIndex, typeID, len(offsets), len(payloadTypes))
	}
	for i := range payloadTypes {
		if isNothingType(fe.emitter.types, payloadTypes[i]) {
			continue
		}
		payloadStorage, storageErr := fe.emitter.llvmValueType(payloadTypes[i])
		if storageErr != nil {
			return storageErr
		}
		off := unionCase.PayloadOffset + offsets[i]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, dst, off)
		fe.emitValueStore(payloadStorage, payloadVals[i], bytePtr, memberAccessAlign(align, off))
	}
	return nil
}

func isUnionType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	id = resolveValueType(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindUnion
}
