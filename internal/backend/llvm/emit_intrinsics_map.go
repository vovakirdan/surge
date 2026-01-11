package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	mapKeyString = iota + 1
	mapKeyInt
	mapKeyUint
	mapKeyBigInt
	mapKeyBigUint
)

func (fe *funcEmitter) emitMapIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	switch name {
	case "rt_map_new":
		return true, fe.emitMapNew(call)
	case "rt_map_len":
		return true, fe.emitMapLen(call)
	case "rt_map_contains":
		return true, fe.emitMapContains(call)
	case "rt_map_get_ref":
		return true, fe.emitMapGet(call, "rt_map_get_ref")
	case "rt_map_get_mut":
		return true, fe.emitMapGet(call, "rt_map_get_mut")
	case "rt_map_insert":
		return true, fe.emitMapInsert(call)
	case "rt_map_remove":
		return true, fe.emitMapRemove(call)
	case "rt_map_keys":
		return true, fe.emitMapKeys(call)
	default:
		return false, nil
	}
}

func (fe *funcEmitter) emitMapNew(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 0 {
		return fmt.Errorf("rt_map_new requires 0 arguments")
	}
	if !call.HasDst {
		return nil
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return err
	}
	keyType, err := fe.mapKeyTypeFromType(dstType)
	if err != nil {
		return err
	}
	keyKind, err := fe.mapKeyKindForType(keyType)
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_map_new(i64 %d)\n", tmp, keyKind)
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	return nil
}

func (fe *funcEmitter) emitMapLen(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_map_len requires 1 argument")
	}
	mapType := operandValueType(fe.emitter.types, &call.Args[0])
	if mapType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			mapType = baseType
		}
	}
	keyType, err := fe.mapKeyTypeFromType(mapType)
	if err != nil {
		return err
	}
	if _, keyErr := fe.mapKeyKindForType(keyType); keyErr != nil {
		return keyErr
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i64 @rt_map_len(ptr %s)\n", tmp, handle)
	if call.HasDst {
		dstType := types.NoTypeID
		if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
			dstType = fe.f.Locals[call.Dst.Local].Type
		}
		if err := fe.emitLenStore(call.Dst, dstType, tmp); err != nil {
			return err
		}
	}
	return nil
}

func (fe *funcEmitter) emitMapContains(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_map_contains requires 2 arguments")
	}
	mapType := operandValueType(fe.emitter.types, &call.Args[0])
	if mapType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			mapType = baseType
		}
	}
	keyType, err := fe.mapKeyTypeFromType(mapType)
	if err != nil {
		return err
	}
	if _, keyErr := fe.mapKeyKindForType(keyType); keyErr != nil {
		return keyErr
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	keyBits, err := fe.emitMapKeyBits(&call.Args[1])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_map_contains(ptr %s, i64 %s)\n", tmp, handle, keyBits)
	if call.HasDst {
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "i1" {
			dstTy = "i1"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	}
	return nil
}

func (fe *funcEmitter) emitMapGet(call *mir.CallInstr, name string) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("%s requires 2 arguments", name)
	}
	mapType := operandValueType(fe.emitter.types, &call.Args[0])
	if mapType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			mapType = baseType
		}
	}
	keyType, err := fe.mapKeyTypeFromType(mapType)
	if err != nil {
		return err
	}
	if _, keyErr := fe.mapKeyKindForType(keyType); keyErr != nil {
		return keyErr
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	keyBits, err := fe.emitMapKeyBits(&call.Args[1])
	if err != nil {
		return err
	}
	bitsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", bitsPtr)
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @%s(ptr %s, i64 %s, ptr %s)\n", okVal, name, handle, keyBits, bitsPtr)
	if !call.HasDst {
		return nil
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return err
	}
	someIdx, someMeta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
	if err != nil {
		return err
	}
	if len(someMeta.PayloadTypes) != 1 {
		return fmt.Errorf("Option::Some expects single payload")
	}
	payloadType := someMeta.PayloadTypes[0]
	readyBB := fe.nextInlineBlock()
	noneBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	outPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, readyBB, noneBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
	bitsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)
	payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
	if err != nil {
		return err
	}
	somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", somePtr, outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", noneBB)
	nonePtr, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nonePtr, outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	resultVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", resultVal, outPtr)
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, resultVal, ptr)
	return nil
}

func (fe *funcEmitter) emitMapInsert(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 3 {
		return fmt.Errorf("rt_map_insert requires 3 arguments")
	}
	mapType := operandValueType(fe.emitter.types, &call.Args[0])
	if mapType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			mapType = baseType
		}
	}
	keyType, err := fe.mapKeyTypeFromType(mapType)
	if err != nil {
		return err
	}
	if _, keyErr := fe.mapKeyKindForType(keyType); keyErr != nil {
		return keyErr
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	keyBits, err := fe.emitMapKeyBits(&call.Args[1])
	if err != nil {
		return err
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[2])
	if err != nil {
		return err
	}
	valueType := operandValueType(fe.emitter.types, &call.Args[2])
	if valueType == types.NoTypeID && call.Args[2].Kind != mir.OperandConst {
		if baseType, baseErr := fe.placeBaseType(call.Args[2].Place); baseErr == nil {
			valueType = baseType
		}
	}
	valueBits, err := fe.emitValueToI64(val, valTy, valueType)
	if err != nil {
		return err
	}
	bitsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", bitsPtr)
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_map_insert(ptr %s, i64 %s, i64 %s, ptr %s)\n", okVal, handle, keyBits, valueBits, bitsPtr)
	if !call.HasDst {
		return nil
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return err
	}
	someIdx, someMeta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
	if err != nil {
		return err
	}
	if len(someMeta.PayloadTypes) != 1 {
		return fmt.Errorf("Option::Some expects single payload")
	}
	payloadType := someMeta.PayloadTypes[0]
	readyBB := fe.nextInlineBlock()
	noneBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	outPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, readyBB, noneBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
	bitsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)
	payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
	if err != nil {
		return err
	}
	somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", somePtr, outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", noneBB)
	nonePtr, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nonePtr, outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	resultVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", resultVal, outPtr)
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, resultVal, ptr)
	return nil
}

func (fe *funcEmitter) emitMapRemove(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 2 {
		return fmt.Errorf("rt_map_remove requires 2 arguments")
	}
	mapType := operandValueType(fe.emitter.types, &call.Args[0])
	if mapType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			mapType = baseType
		}
	}
	keyType, err := fe.mapKeyTypeFromType(mapType)
	if err != nil {
		return err
	}
	if _, keyErr := fe.mapKeyKindForType(keyType); keyErr != nil {
		return keyErr
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	keyBits, err := fe.emitMapKeyBits(&call.Args[1])
	if err != nil {
		return err
	}
	bitsPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca i64\n", bitsPtr)
	okVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call i1 @rt_map_remove(ptr %s, i64 %s, ptr %s)\n", okVal, handle, keyBits, bitsPtr)
	if !call.HasDst {
		return nil
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return err
	}
	someIdx, someMeta, err := fe.emitter.tagCaseMeta(dstType, "Some", symbols.NoSymbolID)
	if err != nil {
		return err
	}
	if len(someMeta.PayloadTypes) != 1 {
		return fmt.Errorf("Option::Some expects single payload")
	}
	payloadType := someMeta.PayloadTypes[0]
	readyBB := fe.nextInlineBlock()
	noneBB := fe.nextInlineBlock()
	contBB := fe.nextInlineBlock()
	outPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = alloca ptr\n", outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br i1 %s, label %%%s, label %%%s\n", okVal, readyBB, noneBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", readyBB)
	bitsVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load i64, ptr %s\n", bitsVal, bitsPtr)
	payloadVal, payloadTy, err := fe.emitI64ToValue(bitsVal, payloadType)
	if err != nil {
		return err
	}
	somePtr, err := fe.emitTagValueSinglePayload(dstType, someIdx, payloadType, payloadVal, payloadTy, payloadType)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", somePtr, outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", noneBB)
	nonePtr, err := fe.emitTagValue(dstType, "nothing", symbols.NoSymbolID, nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(&fe.emitter.buf, "  store ptr %s, ptr %s\n", nonePtr, outPtr)
	fmt.Fprintf(&fe.emitter.buf, "  br label %%%s\n", contBB)

	fmt.Fprintf(&fe.emitter.buf, "%s:\n", contBB)
	resultVal := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", resultVal, outPtr)
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, resultVal, ptr)
	return nil
}

func (fe *funcEmitter) emitMapKeys(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	if len(call.Args) != 1 {
		return fmt.Errorf("rt_map_keys requires 1 argument")
	}
	mapType := operandValueType(fe.emitter.types, &call.Args[0])
	if mapType == types.NoTypeID && call.Args[0].Kind != mir.OperandConst {
		if baseType, err := fe.placeBaseType(call.Args[0].Place); err == nil {
			mapType = baseType
		}
	}
	keyType, err := fe.mapKeyTypeFromType(mapType)
	if err != nil {
		return err
	}
	if _, keyErr := fe.mapKeyKindForType(keyType); keyErr != nil {
		return keyErr
	}
	keyType = resolveMapKeyType(fe.emitter.types, keyType)
	keyLLVM, err := llvmValueType(fe.emitter.types, keyType)
	if err != nil {
		return err
	}
	elemSize, elemAlign, err := llvmTypeSizeAlign(keyLLVM)
	if err != nil {
		return err
	}
	if elemSize <= 0 {
		elemSize = 1
	}
	if elemAlign <= 0 {
		elemAlign = 1
	}
	handle, err := fe.emitMapHandle(&call.Args[0])
	if err != nil {
		return err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_map_keys(ptr %s, i64 %d, i64 %d)\n", tmp, handle, elemSize, elemAlign)
	if call.HasDst {
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != "ptr" {
			dstTy = "ptr"
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
	}
	return nil
}

func (fe *funcEmitter) emitMapHandle(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("missing map operand")
	}
	handlePtr, err := fe.emitHandleOperandPtr(op)
	if err != nil {
		return "", err
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = load ptr, ptr %s\n", tmp, handlePtr)
	return tmp, nil
}

func (fe *funcEmitter) emitMapKeyBits(op *mir.Operand) (string, error) {
	if op == nil {
		return "", fmt.Errorf("missing map key operand")
	}
	val, valTy, valType, err := fe.emitToSource(op)
	if err != nil {
		return "", err
	}
	return fe.emitValueToI64(val, valTy, valType)
}

func (fe *funcEmitter) mapKeyTypeFromType(mapType types.TypeID) (types.TypeID, error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return types.NoTypeID, fmt.Errorf("missing type interner")
	}
	mapType = resolveValueType(fe.emitter.types, mapType)
	key, _, ok := fe.emitter.types.MapInfo(mapType)
	if !ok {
		return types.NoTypeID, fmt.Errorf("map intrinsic requires Map type")
	}
	return key, nil
}

func resolveMapKeyType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	for i := 0; i < 32 && id != types.NoTypeID; i++ {
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func (fe *funcEmitter) mapKeyKindForType(typeID types.TypeID) (int, error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return 0, fmt.Errorf("missing type interner")
	}
	typeID = resolveMapKeyType(fe.emitter.types, typeID)
	tt, ok := fe.emitter.types.Lookup(typeID)
	if !ok {
		return 0, fmt.Errorf("missing key type")
	}
	switch tt.Kind {
	case types.KindString:
		return mapKeyString, nil
	case types.KindInt:
		if tt.Width == types.WidthAny {
			return mapKeyBigInt, nil
		}
		return mapKeyInt, nil
	case types.KindUint:
		if tt.Width == types.WidthAny {
			return mapKeyBigUint, nil
		}
		return mapKeyUint, nil
	default:
		return 0, fmt.Errorf("unsupported map key type %s", tt.Kind.String())
	}
}
