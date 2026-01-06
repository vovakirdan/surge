package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitDefaultIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "default" {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if len(call.Args) != 0 {
		return true, fmt.Errorf("default requires 0 arguments")
	}
	if !call.HasDst {
		return true, nil
	}
	targetType := types.NoTypeID
	if fe.emitter != nil && fe.emitter.mod != nil && fe.emitter.mod.Meta != nil && call.Callee.Sym.IsValid() {
		if args, ok := fe.emitter.mod.Meta.FuncTypeArgs[call.Callee.Sym]; ok && len(args) == 1 {
			targetType = args[0]
		}
	}
	if targetType == types.NoTypeID && call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
		targetType = fe.f.Locals[call.Dst.Local].Type
	}
	if targetType == types.NoTypeID {
		return true, fmt.Errorf("default requires type arguments")
	}
	val, valTy, err := fe.emitDefaultValue(targetType)
	if err != nil {
		return true, err
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != valTy {
		valTy = dstTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, ptr)
	return true, nil
}

func (fe *funcEmitter) emitDefaultValue(typeID types.TypeID) (val, ty string, err error) {
	if typeID == types.NoTypeID {
		return "", "", fmt.Errorf("invalid default type")
	}
	if fe.emitter == nil || fe.emitter.types == nil {
		return "", "", fmt.Errorf("missing type interner")
	}
	tt, ok := fe.emitter.types.Lookup(typeID)
	if !ok {
		return "", "", fmt.Errorf("missing type info for type#%d", typeID)
	}
	switch tt.Kind {
	case types.KindAlias:
		target, ok := fe.emitter.types.AliasTarget(typeID)
		if !ok || target == types.NoTypeID {
			return "", "", fmt.Errorf("missing alias target for type#%d", typeID)
		}
		return fe.emitDefaultValue(target)
	case types.KindOwn:
		return fe.emitDefaultValue(tt.Elem)
	case types.KindReference:
		return "", "", fmt.Errorf("default is not defined for references")
	case types.KindPointer:
		return "null", "ptr", nil
	case types.KindUnit, types.KindNothing:
		llvmTy, typeErr := llvmValueType(fe.emitter.types, typeID)
		if typeErr != nil {
			return "", "", typeErr
		}
		if llvmTy == "ptr" {
			return "null", llvmTy, nil
		}
		return "0", llvmTy, nil
	case types.KindBool:
		return "0", "i1", nil
	case types.KindString:
		return fe.emitStringConst("")
	case types.KindInt, types.KindUint:
		if isBigIntType(fe.emitter.types, typeID) || isBigUintType(fe.emitter.types, typeID) {
			return "null", "ptr", nil
		}
		llvmTy, err := llvmValueType(fe.emitter.types, typeID)
		if err != nil {
			return "", "", err
		}
		return "0", llvmTy, nil
	case types.KindFloat:
		if isBigFloatType(fe.emitter.types, typeID) {
			return "null", "ptr", nil
		}
		llvmTy, err := llvmValueType(fe.emitter.types, typeID)
		if err != nil {
			return "", "", err
		}
		return "0.0", llvmTy, nil
	case types.KindArray:
		if tt.Count == types.ArrayDynamicLength {
			return fe.emitDefaultArrayDynamic()
		}
		return fe.emitDefaultArrayFixed(typeID, tt.Elem, tt.Count)
	case types.KindStruct:
		if _, ok := fe.emitter.types.ArrayInfo(typeID); ok {
			return fe.emitDefaultArrayDynamic()
		}
		if elem, length, ok := fe.emitter.types.ArrayFixedInfo(typeID); ok {
			return fe.emitDefaultArrayFixed(typeID, elem, length)
		}
		return fe.emitDefaultStruct(typeID)
	case types.KindTuple:
		return fe.emitDefaultTuple(typeID)
	case types.KindUnion:
		if !fe.emitter.hasTagLayout(typeID) {
			return "", "", fmt.Errorf("default requires union with nothing")
		}
		if _, _, err := fe.emitter.tagCaseMeta(typeID, "nothing", symbols.NoSymbolID); err != nil {
			return "", "", fmt.Errorf("default requires union with nothing")
		}
		ptr, err := fe.emitTagValue(typeID, "nothing", symbols.NoSymbolID, nil)
		if err != nil {
			return "", "", err
		}
		return ptr, "ptr", nil
	case types.KindConst:
		llvmTy, err := llvmValueType(fe.emitter.types, typeID)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%d", tt.Count), llvmTy, nil
	default:
		return "", "", fmt.Errorf("default not implemented for type kind %s", tt.Kind)
	}
}

func (fe *funcEmitter) emitDefaultStruct(typeID types.TypeID) (val, ty string, err error) {
	info, ok := fe.emitter.types.StructInfo(resolveAliasAndOwn(fe.emitter.types, typeID))
	if !ok || info == nil {
		return "", "", fmt.Errorf("missing struct info for type#%d", typeID)
	}
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return "", "", err
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
	for i, field := range info.Fields {
		if i >= len(layoutInfo.FieldOffsets) {
			return "", "", fmt.Errorf("struct field %d out of range", i)
		}
		val, valTy, err := fe.emitDefaultValue(field.Type)
		if err != nil {
			return "", "", err
		}
		fieldLLVM, err := llvmValueType(fe.emitter.types, field.Type)
		if err != nil {
			return "", "", err
		}
		if valTy != fieldLLVM {
			valTy = fieldLLVM
		}
		off := layoutInfo.FieldOffsets[i]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
	}
	return mem, "ptr", nil
}

func (fe *funcEmitter) emitDefaultTuple(typeID types.TypeID) (val, ty string, err error) {
	info, ok := fe.emitter.types.TupleInfo(resolveAliasAndOwn(fe.emitter.types, typeID))
	if !ok || info == nil || len(info.Elems) == 0 {
		llvmTy, typeErr := llvmValueType(fe.emitter.types, typeID)
		if typeErr != nil {
			return "", "", typeErr
		}
		return "0", llvmTy, nil
	}
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return "", "", err
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
	for i, elemType := range info.Elems {
		if i >= len(layoutInfo.FieldOffsets) {
			return "", "", fmt.Errorf("tuple field %d out of range", i)
		}
		val, valTy, err := fe.emitDefaultValue(elemType)
		if err != nil {
			return "", "", err
		}
		elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
		if err != nil {
			return "", "", err
		}
		if valTy != elemLLVM {
			valTy = elemLLVM
		}
		off := layoutInfo.FieldOffsets[i]
		bytePtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", bytePtr, mem, off)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, bytePtr)
	}
	return mem, "ptr", nil
}

func (fe *funcEmitter) emitDefaultArrayDynamic() (val, ty string, err error) {
	headPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_alloc(i64 %d, i64 %d)\n", headPtr, arrayHeaderSize, arrayHeaderAlign)
	lenPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", lenPtr, headPtr, arrayLenOffset)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", lenPtr)
	capPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", capPtr, headPtr, arrayCapOffset)
	fmt.Fprintf(&fe.emitter.buf, "  store i64 0, ptr %s\n", capPtr)
	dataPtrPtr := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", dataPtrPtr, headPtr, arrayDataOffset)
	fmt.Fprintf(&fe.emitter.buf, "  store ptr null, ptr %s\n", dataPtrPtr)
	return headPtr, "ptr", nil
}

func (fe *funcEmitter) emitDefaultArrayFixed(typeID, elemType types.TypeID, length uint32) (val, ty string, err error) {
	layoutInfo, err := fe.emitter.layoutOf(typeID)
	if err != nil {
		return "", "", err
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
	if length == 0 {
		return mem, "ptr", nil
	}
	elemLLVM, err := llvmValueType(fe.emitter.types, elemType)
	if err != nil {
		return "", "", err
	}
	elemSize, elemAlign, err := llvmTypeSizeAlign(elemLLVM)
	if err != nil {
		return "", "", err
	}
	if elemAlign <= 0 {
		elemAlign = 1
	}
	stride := roundUpInt(elemSize, elemAlign)
	for i := range length {
		val, valTy, err := fe.emitDefaultValue(elemType)
		if err != nil {
			return "", "", err
		}
		if valTy != elemLLVM {
			valTy = elemLLVM
		}
		offset := int(i) * stride
		elemPtr := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = getelementptr inbounds i8, ptr %s, i64 %d\n", elemPtr, mem, offset)
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", valTy, val, elemPtr)
	}
	return mem, "ptr", nil
}
