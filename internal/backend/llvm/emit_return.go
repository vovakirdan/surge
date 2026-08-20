package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

// emitUnionReturn assumes fe/emitter/types are non-nil; callers enforce that invariant.
// emitBareMemberReturn widens a bare member into the union it is being
// returned as.
//
// It exists because a bare member used to be returned AS ITSELF: the value
// went back unchanged, labelled with the union's LLVM type, and no
// discriminant was written anywhere. The caller then copied the union's size
// out of storage only big enough for the member -- for Erring<int,Error> that
// is 24 bytes read out of a 16-byte allocation, and the discriminant it read
// was the low half of a pointer.
//
// A bare member is a member. It gets a discriminant like every other case,
// and its value goes at its case's payload offset.
func (fe *funcEmitter) emitBareMemberReturn(unionType types.TypeID, val, valTy string, valueType types.TypeID) (storage, storageLLVM string, widened bool, err error) {
	unionType = resolveValueType(fe.emitter.types, unionType)
	mem, err := fe.emitValueStorage(unionType)
	if err != nil {
		return "", "", false, err
	}
	facts, err := fe.emitter.layoutOf(unionType)
	if err != nil {
		return "", "", false, err
	}
	align := facts.Align
	if align == 0 {
		align = 1
	}
	materialised, err := fe.emitUnionMaterialiseBareMember(mem, align, unionType, val, valTy, valueType)
	if err != nil {
		return "", "", false, err
	}
	if !materialised {
		return "", "", false, nil
	}
	unionLLVM, err := fe.emitter.llvmType(unionType)
	if err != nil {
		return "", "", false, err
	}
	return mem, unionLLVM, true, nil
}

func (fe *funcEmitter) emitUnionReturn(val, valTy string, op *mir.Operand, expected types.TypeID) (outVal, outTy string, err error) {
	expected = resolveAliasAndOwn(fe.emitter.types, expected)
	if !isUnionType(fe.emitter.types, expected) {
		return val, valTy, nil
	}
	expectedStorage, err := fe.emitter.llvmValueType(expected)
	if err != nil {
		return "", "", err
	}
	// The comparisons below check how the value TRAVELS, not where it lives: a
	// union is carried as the address of its storage, so an operand that
	// already names one is the shape a return wants.
	expectedLLVM := operandTypeFor(expectedStorage)
	opType := types.NoTypeID
	if op != nil {
		opType = operandValueType(fe.emitter.types, op)
	}
	if opType == types.NoTypeID && op != nil && op.Kind != mir.OperandConst {
		if baseType, baseErr := fe.placeBaseType(op.Place); baseErr == nil {
			opType = baseType
		}
	}
	opType = resolveAliasAndOwn(fe.emitter.types, opType)
	if isUnionType(fe.emitter.types, opType) {
		if opType == expected {
			if valTy != expectedLLVM {
				return "", "", fmt.Errorf("return type mismatch: expected %s, got %s", expectedLLVM, valTy)
			}
			return val, expectedLLVM, nil
		}
		if info, ok := fe.emitter.types.UnionInfo(expected); ok && info != nil {
			for _, member := range info.Members {
				if member.Kind != types.UnionMemberType {
					continue
				}
				memberType := resolveValueType(fe.emitter.types, member.Type)
				if memberType == opType {
					mem, memTy, done, merr := fe.emitBareMemberReturn(expected, val, valTy, opType)
					if merr != nil {
						return "", "", merr
					}
					if done {
						return mem, memTy, nil
					}
					if valTy != expectedLLVM {
						return "", "", fmt.Errorf("return type mismatch: expected %s, got %s", expectedLLVM, valTy)
					}
					return val, expectedLLVM, nil
				}
				if isUnionType(fe.emitter.types, memberType) {
					ok, err := fe.unionTagsSubset(opType, memberType)
					if err != nil || !ok {
						continue
					}
					casted, castTy, err := fe.emitUnionCast(val, opType, memberType)
					if err != nil {
						return "", "", err
					}
					if castTy != expectedLLVM {
						return "", "", fmt.Errorf("return type mismatch: expected %s, got %s", expectedLLVM, castTy)
					}
					return casted, expectedLLVM, nil
				}
			}
		}
		casted, castTy, err := fe.emitUnionCast(val, opType, expected)
		if err != nil {
			return "", "", err
		}
		return casted, castTy, nil
	}
	info, ok := fe.emitter.types.UnionInfo(expected)
	if ok && info != nil {
		for _, member := range info.Members {
			switch member.Kind {
			case types.UnionMemberType:
				if resolveAliasAndOwn(fe.emitter.types, member.Type) == opType {
					mem, memTy, done, merr := fe.emitBareMemberReturn(expected, val, valTy, opType)
					if merr != nil {
						return "", "", merr
					}
					if done {
						return mem, memTy, nil
					}
					if valTy != expectedLLVM {
						return "", "", fmt.Errorf("return type mismatch: expected %s, got %s", expectedLLVM, valTy)
					}
					return val, expectedLLVM, nil
				}
			case types.UnionMemberTag:
				if len(member.TagArgs) != 1 {
					continue
				}
				if resolveAliasAndOwn(fe.emitter.types, member.TagArgs[0]) != opType {
					continue
				}
				if fe.emitter.types.Strings == nil {
					return "", "", fmt.Errorf("missing tag name for union return")
				}
				tagName := fe.emitter.types.Strings.MustLookup(member.TagName)
				if tagName == "" {
					return "", "", fmt.Errorf("missing tag name for union return")
				}
				tagIndex, meta, err := fe.emitter.tagCaseMeta(expected, tagName, symbols.NoSymbolID)
				if err != nil {
					return "", "", err
				}
				if len(meta.PayloadTypes) != 1 {
					return "", "", fmt.Errorf("tag %q expects 1 payload value, got %d", meta.TagName, len(meta.PayloadTypes))
				}
				payloadStorage, err := fe.emitter.llvmValueType(meta.PayloadTypes[0])
				if err != nil {
					return "", "", err
				}
				payloadLLVM := operandTypeFor(payloadStorage)
				payloadVal := val
				payloadTy := valTy
				if payloadTy != payloadLLVM {
					casted, castTy, castErr := fe.coerceNumericValue(payloadVal, payloadTy, opType, meta.PayloadTypes[0])
					if castErr != nil {
						return "", "", castErr
					}
					payloadVal = casted
					payloadTy = castTy
				}
				if payloadTy != payloadLLVM {
					return "", "", fmt.Errorf("tag payload type mismatch for type#%d tag %d: expected %s, got %s", expected, tagIndex, payloadLLVM, payloadTy)
				}
				tagVal, err := fe.emitTagValueSinglePayload(expected, tagIndex, meta.PayloadTypes[0], payloadVal, payloadTy, meta.PayloadTypes[0])
				if err != nil {
					return "", "", err
				}
				return tagVal, handleType, nil
			case types.UnionMemberNothing:
				if isNothingType(fe.emitter.types, opType) {
					tagVal, err := fe.emitTagValue(expected, "nothing", symbols.NoSymbolID, nil)
					if err != nil {
						return "", "", err
					}
					return tagVal, handleType, nil
				}
			}
		}
	}
	if valTy != expectedLLVM {
		return "", "", fmt.Errorf("return type mismatch: expected %s, got %s", expectedLLVM, valTy)
	}
	return val, expectedLLVM, nil
}

func (fe *funcEmitter) unionTagsSubset(srcType, dstType types.TypeID) (bool, error) {
	if fe == nil || fe.emitter == nil {
		return false, fmt.Errorf("missing emitter")
	}
	srcCases, err := fe.emitter.tagCases(srcType)
	if err != nil {
		return false, err
	}
	dstCases, err := fe.emitter.tagCases(dstType)
	if err != nil {
		return false, err
	}
	for _, srcCase := range srcCases {
		if _, ok := matchTagCase(dstCases, srcCase); !ok {
			return false, nil
		}
	}
	return true, nil
}
