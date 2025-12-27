package vm

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// handleExit handles the exit(ErrorLike) intrinsic.
func (vm *VM) handleExit(frame *Frame, call *mir.CallInstr) *VMError {
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "exit requires 1 argument")
	}
	val, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	if val.Kind == VKRef || val.Kind == VKRefMut {
		loaded, loadErr := vm.loadLocationRaw(val.Loc)
		if loadErr != nil {
			return loadErr
		}
		val = loaded
	}

	msg, code, vmErr := vm.errorLikeMessageAndCode(val)
	if vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	vm.dropValue(val)

	if msg != "" {
		if !strings.HasSuffix(msg, "\n") {
			msg += "\n"
		}
		_, _ = os.Stderr.WriteString(msg)
	}

	vm.ExitCode = code
	vm.RT.Exit(code)

	vm.dropAllFrames()
	vm.dropGlobals()
	vm.checkLeaksOrPanic()

	vm.Halted = true
	vm.Stack = nil
	return nil
}

// handleFromStr handles built-in from_str(&string) -> Erring<T, Error>.
func (vm *VM) handleFromStr(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if len(call.Args) != 1 {
		return vm.eb.makeError(PanicTypeMismatch, "from_str requires 1 argument")
	}
	if !call.HasDst {
		return nil
	}
	strVal, vmErr := vm.evalOperand(frame, &call.Args[0])
	if vmErr != nil {
		return vmErr
	}
	borrowed := strVal.Kind == VKRef || strVal.Kind == VKRefMut
	if strVal.Kind == VKRef || strVal.Kind == VKRefMut {
		loaded, loadErr := vm.loadLocationRaw(strVal.Loc)
		if loadErr != nil {
			return loadErr
		}
		strVal = loaded
	}
	if strVal.Kind != VKHandleString {
		if !borrowed {
			vm.dropValue(strVal)
		}
		return vm.eb.typeMismatch("string", strVal.Kind.String())
	}
	if borrowed {
		strVal, vmErr = vm.cloneForShare(strVal)
		if vmErr != nil {
			return vmErr
		}
	}

	dstLocal := call.Dst.Local
	dstType := frame.Locals[dstLocal].TypeID
	if vm.Types == nil {
		vm.dropValue(strVal)
		return vm.eb.makeError(PanicUnimplemented, "from_str requires type information")
	}

	layout, vmErr := vm.tagLayoutFor(dstType)
	if vmErr != nil {
		vm.dropValue(strVal)
		return vmErr
	}
	successCase, ok := layout.CaseByName("Success")
	if !ok || len(successCase.PayloadTypes) != 1 {
		vm.dropValue(strVal)
		return vm.eb.makeError(PanicTypeMismatch, "from_str requires Erring<T, Error> destination")
	}
	targetType := successCase.PayloadTypes[0]

	parsed, parseErr := vm.parseFromString(strVal, targetType)
	if parseErr != nil {
		vm.dropValue(strVal)
		errType, vmErr := vm.erringErrorType(dstType)
		if vmErr != nil {
			return vmErr
		}
		errVal, vmErr := vm.makeErrorLikeValue(errType, parseErr.Error(), 1)
		if vmErr != nil {
			return vmErr
		}
		if vmErr := vm.writeLocal(frame, dstLocal, errVal); vmErr != nil {
			vm.dropValue(errVal)
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   errVal,
		})
		return nil
	}
	if !vm.isStringType(targetType) {
		vm.dropValue(strVal)
	}

	h := vm.Heap.AllocTag(dstType, successCase.TagSym, []Value{parsed})
	tagVal := MakeHandleTag(h, dstType)
	if vmErr := vm.writeLocal(frame, dstLocal, tagVal); vmErr != nil {
		vm.dropValue(tagVal)
		return vmErr
	}
	*writes = append(*writes, LocalWrite{
		LocalID: dstLocal,
		Name:    frame.Locals[dstLocal].Name,
		Value:   tagVal,
	})
	return nil
}

func (vm *VM) parseFromString(strVal Value, targetType types.TypeID) (Value, error) {
	if vm.Types == nil {
		return Value{}, fmt.Errorf("missing type info")
	}
	tt, ok := vm.Types.Lookup(vm.valueType(targetType))
	if !ok {
		return Value{}, fmt.Errorf("unknown target type")
	}
	obj := vm.Heap.Get(strVal.H)
	if obj == nil {
		return Value{}, fmt.Errorf("invalid string handle")
	}
	raw := string(vm.stringBytes(obj))

	switch tt.Kind {
	case types.KindString:
		strVal.TypeID = targetType
		return strVal, nil
	case types.KindBool:
		parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return Value{}, fmt.Errorf("failed to parse %q as bool: %w", raw, err)
		}
		return MakeBool(parsed, targetType), nil
	default:
		val, vmErr := vm.evalIntrinsicTo(strVal, targetType)
		if vmErr != nil {
			return Value{}, fmt.Errorf("%s", vmErr.Message)
		}
		return val, nil
	}
}

func (vm *VM) errorLikeMessageAndCode(val Value) (message string, code int, vmErr *VMError) {
	if val.Kind != VKHandleStruct {
		return "", 0, vm.eb.typeMismatch("ErrorLike", val.Kind.String())
	}
	layout, vmErr := vm.layouts.Struct(val.TypeID)
	if vmErr != nil {
		return "", 0, vmErr
	}
	msgIdx, ok := layout.IndexByName["message"]
	if !ok {
		return "", 0, vm.eb.makeError(PanicTypeMismatch, "ErrorLike missing field 'message'")
	}
	codeIdx, ok := layout.IndexByName["code"]
	if !ok {
		return "", 0, vm.eb.makeError(PanicTypeMismatch, "ErrorLike missing field 'code'")
	}
	obj := vm.Heap.Get(val.H)
	if obj == nil || obj.Kind != OKStruct {
		return "", 0, vm.eb.typeMismatch("struct", fmt.Sprintf("%v", obj.Kind))
	}
	if msgIdx < 0 || msgIdx >= len(obj.Fields) || codeIdx < 0 || codeIdx >= len(obj.Fields) {
		return "", 0, vm.eb.makeError(PanicOutOfBounds, "ErrorLike field index out of range")
	}
	msgVal := obj.Fields[msgIdx]
	if msgVal.Kind != VKHandleString {
		return "", 0, vm.eb.typeMismatch("string", msgVal.Kind.String())
	}
	msgObj := vm.Heap.Get(msgVal.H)
	if msgObj == nil {
		return "", 0, vm.eb.makeError(PanicOutOfBounds, "invalid ErrorLike message handle")
	}
	codeVal := obj.Fields[codeIdx]
	code, vmErr = vm.errorCodeFromValue(codeVal)
	if vmErr != nil {
		return "", 0, vmErr
	}
	return string(vm.stringBytes(msgObj)), code, nil
}

func (vm *VM) errorCodeFromValue(val Value) (int, *VMError) {
	switch val.Kind {
	case VKBigUint:
		u, vmErr := vm.mustBigUint(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := u.Uint64()
		if !ok || n > math.MaxInt {
			return 0, vm.eb.invalidNumericConversion("exit code out of range")
		}
		return int(n), nil
	case VKBigInt:
		i, vmErr := vm.mustBigInt(val)
		if vmErr != nil {
			return 0, vmErr
		}
		n, ok := i.Int64()
		if !ok || n < 0 || n > math.MaxInt {
			return 0, vm.eb.invalidNumericConversion("exit code out of range")
		}
		return int(n), nil
	case VKInt:
		if val.Int < 0 || val.Int > math.MaxInt {
			return 0, vm.eb.invalidNumericConversion("exit code out of range")
		}
		return int(val.Int), nil
	default:
		return 0, vm.eb.typeMismatch("uint", val.Kind.String())
	}
}

func (vm *VM) erringErrorType(typeID types.TypeID) (types.TypeID, *VMError) {
	if vm.Types == nil {
		return types.NoTypeID, vm.eb.makeError(PanicUnimplemented, "missing type info for Erring")
	}
	typeID = vm.valueType(typeID)
	info, ok := vm.Types.UnionInfo(typeID)
	if !ok || info == nil {
		return types.NoTypeID, vm.eb.makeError(PanicTypeMismatch, "from_str destination is not Erring")
	}
	for _, member := range info.Members {
		if member.Kind == types.UnionMemberType {
			return member.Type, nil
		}
	}
	return types.NoTypeID, vm.eb.makeError(PanicTypeMismatch, "Erring missing error variant")
}

func (vm *VM) makeErrorLikeValue(errType types.TypeID, msg string, code uint64) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(errType)
	if vmErr != nil {
		return Value{}, vmErr
	}
	msgIdx, ok := layout.IndexByName["message"]
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "ErrorLike missing field 'message'")
	}
	codeIdx, ok := layout.IndexByName["code"]
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "ErrorLike missing field 'code'")
	}

	fields := make([]Value, len(layout.FieldTypes))
	for i, ft := range layout.FieldTypes {
		if i == msgIdx || i == codeIdx {
			continue
		}
		val, defaultErr := vm.defaultValue(ft)
		if defaultErr != nil {
			for j := range i {
				vm.dropValue(fields[j])
			}
			return Value{}, defaultErr
		}
		fields[i] = val
	}

	msgType := layout.FieldTypes[msgIdx]
	msgHandle := vm.Heap.AllocString(msgType, msg)
	fields[msgIdx] = MakeHandleString(msgHandle, msgType)

	codeType := layout.FieldTypes[codeIdx]
	codeVal, vmErr := vm.makeUintForType(codeType, code)
	if vmErr != nil {
		for _, f := range fields {
			vm.dropValue(f)
		}
		return Value{}, vmErr
	}
	fields[codeIdx] = codeVal

	h := vm.Heap.AllocStruct(layout.TypeID, fields)
	return MakeHandleStruct(h, errType), nil
}

func (vm *VM) makeUintForType(typeID types.TypeID, n uint64) (Value, *VMError) {
	if vm.Types == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "missing type info for uint literal")
	}
	tt, ok := vm.Types.Lookup(vm.valueType(typeID))
	if !ok {
		return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("unknown type#%d", typeID))
	}
	switch tt.Kind {
	case types.KindUint:
		return vm.makeBigUint(typeID, bignum.UintFromUint64(n)), nil
	case types.KindInt:
		if n > math.MaxInt64 {
			return Value{}, vm.eb.invalidNumericConversion("exit code out of range")
		}
		return vm.makeBigInt(typeID, bignum.IntFromInt64(int64(n))), nil
	default:
		return Value{}, vm.eb.typeMismatch("uint", tt.Kind.String())
	}
}

func (vm *VM) isStringType(typeID types.TypeID) bool {
	if vm.Types == nil {
		return false
	}
	tt, ok := vm.Types.Lookup(vm.valueType(typeID))
	return ok && tt.Kind == types.KindString
}
