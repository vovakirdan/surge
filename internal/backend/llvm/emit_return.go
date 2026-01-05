package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitUnionReturn(val, valTy string, op *mir.Operand, expected types.TypeID) (outVal, outTy string, err error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return val, valTy, nil
	}
	expected = resolveAliasAndOwn(fe.emitter.types, expected)
	if !isUnionType(fe.emitter.types, expected) {
		return val, valTy, nil
	}
	expectedLLVM, err := llvmValueType(fe.emitter.types, expected)
	if err != nil {
		return "", "", err
	}
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
				payloadLLVM, err := llvmValueType(fe.emitter.types, meta.PayloadTypes[0])
				if err != nil {
					return "", "", err
				}
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
				return tagVal, "ptr", nil
			case types.UnionMemberNothing:
				if isNothingType(fe.emitter.types, opType) {
					tagVal, err := fe.emitTagValue(expected, "nothing", symbols.NoSymbolID, nil)
					if err != nil {
						return "", "", err
					}
					return tagVal, "ptr", nil
				}
			}
		}
	}
	if valTy != expectedLLVM {
		return "", "", fmt.Errorf("return type mismatch: expected %s, got %s", expectedLLVM, valTy)
	}
	return val, expectedLLVM, nil
}
