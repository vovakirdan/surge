package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func (vm *VM) handleDefault(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	if !call.HasDst {
		return nil
	}
	if len(call.Args) != 0 {
		return vm.eb.makeError(PanicTypeMismatch, "default requires 0 arguments")
	}
	if vm.M == nil || vm.M.Meta == nil || len(vm.M.Meta.FuncTypeArgs) == 0 || !call.Callee.Sym.IsValid() {
		return vm.eb.makeError(PanicUnimplemented, "missing type arguments for default")
	}
	typeArgs, ok := vm.M.Meta.FuncTypeArgs[call.Callee.Sym]
	if !ok || len(typeArgs) != 1 || typeArgs[0] == types.NoTypeID {
		return vm.eb.makeError(PanicUnimplemented, "invalid type arguments for default")
	}
	targetType := typeArgs[0]
	val, vmErr := vm.defaultValue(targetType)
	if vmErr != nil {
		return vmErr
	}
	dstLocal := call.Dst.Local
	if vmErr := vm.writeLocal(frame, dstLocal, val); vmErr != nil {
		vm.dropValue(val)
		return vmErr
	}
	if writes != nil {
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   val,
		})
	}
	return nil
}

func (vm *VM) defaultValue(typeID types.TypeID) (Value, *VMError) {
	if typeID == types.NoTypeID {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "invalid default type")
	}
	if vm.Types == nil {
		return Value{}, vm.eb.makeError(PanicUnimplemented, "no type interner for default")
	}
	tt, ok := vm.Types.Lookup(typeID)
	if !ok {
		return Value{}, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("missing type info for type#%d", typeID))
	}

	switch tt.Kind {
	case types.KindAlias:
		target, ok := vm.Types.AliasTarget(typeID)
		if !ok || target == types.NoTypeID {
			return Value{}, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("missing alias target for type#%d", typeID))
		}
		val, vmErr := vm.defaultValue(target)
		if vmErr != nil {
			return Value{}, vmErr
		}
		val.TypeID = typeID
		return val, nil
	case types.KindOwn:
		val, vmErr := vm.defaultValue(tt.Elem)
		if vmErr != nil {
			return Value{}, vmErr
		}
		val.TypeID = typeID
		return val, nil
	case types.KindReference:
		return Value{}, vm.eb.makeError(PanicTypeMismatch, "default is not defined for references")
	case types.KindPointer:
		return MakePtr(Location{Kind: LKRawBytes, Handle: 0}, typeID), nil
	case types.KindUnit, types.KindNothing:
		v := MakeNothing()
		v.TypeID = typeID
		return v, nil
	case types.KindBool:
		return MakeBool(false, typeID), nil
	case types.KindString:
		h := vm.Heap.AllocString(typeID, "")
		return MakeHandleString(h, typeID), nil
	case types.KindInt:
		if tt.Width == types.WidthAny {
			return vm.makeBigInt(typeID, bignum.IntFromInt64(0)), nil
		}
		return MakeInt(0, typeID), nil
	case types.KindUint:
		if tt.Width == types.WidthAny {
			return vm.makeBigUint(typeID, bignum.UintFromUint64(0)), nil
		}
		return MakeInt(0, typeID), nil
	case types.KindFloat:
		return vm.makeBigFloat(typeID, bignum.FloatZero()), nil
	case types.KindArray:
		elem := tt.Elem
		if tt.Count == types.ArrayDynamicLength {
			h := vm.Heap.AllocArray(typeID, nil)
			return MakeHandleArray(h, typeID), nil
		}
		return vm.defaultArray(typeID, elem, int(tt.Count))
	case types.KindStruct:
		if _, _, ok := vm.Types.MapInfo(typeID); ok {
			h := vm.Heap.AllocMap(typeID)
			return MakeHandleMap(h, typeID), nil
		}
		if _, ok := vm.Types.ArrayInfo(typeID); ok {
			h := vm.Heap.AllocArray(typeID, nil)
			return MakeHandleArray(h, typeID), nil
		}
		if elem, length, ok := vm.Types.ArrayFixedInfo(typeID); ok {
			return vm.defaultArray(typeID, elem, int(length))
		}
		return vm.defaultStruct(typeID)
	case types.KindUnion:
		layout, vmErr := vm.tagLayoutFor(typeID)
		if vmErr != nil {
			return Value{}, vmErr
		}
		tc, ok := layout.CaseByName("nothing")
		if !ok {
			return Value{}, vm.eb.makeError(PanicTypeMismatch, "default requires union with nothing")
		}
		h := vm.Heap.AllocTag(typeID, tc.TagSym, nil)
		return MakeHandleTag(h, typeID), nil
	case types.KindConst:
		return MakeInt(int64(tt.Count), typeID), nil
	default:
		return Value{}, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("default not implemented for type kind %s", tt.Kind))
	}
}

func (vm *VM) defaultStruct(typeID types.TypeID) (Value, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return Value{}, vmErr
	}
	fields := make([]Value, 0, len(layout.FieldTypes))
	for _, fieldType := range layout.FieldTypes {
		val, vmErr := vm.defaultValue(fieldType)
		if vmErr != nil {
			for _, f := range fields {
				vm.dropValue(f)
			}
			return Value{}, vmErr
		}
		fields = append(fields, val)
	}
	h := vm.Heap.AllocStruct(layout.TypeID, fields)
	return MakeHandleStruct(h, typeID), nil
}

func (vm *VM) defaultArray(typeID, elemType types.TypeID, length int) (Value, *VMError) {
	if length < 0 {
		return Value{}, vm.eb.makeError(PanicInvalidNumericConversion, "array length out of range")
	}
	elems := make([]Value, length)
	for i := range length {
		val, vmErr := vm.defaultValue(elemType)
		if vmErr != nil {
			for j := range i {
				vm.dropValue(elems[j])
			}
			return Value{}, vmErr
		}
		elems[i] = val
	}
	h := vm.Heap.AllocArray(typeID, elems)
	return MakeHandleArray(h, typeID), nil
}
