package vm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// callIntrinsic handles runtime intrinsic calls (and selected extern calls not lowered into MIR).
func (vm *VM) callIntrinsic(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	name := call.Callee.Name

	switch name {
	case "rt_argv":
		argv := vm.RT.Argv()
		strTy := types.NoTypeID
		if vm.Types != nil {
			strTy = vm.Types.Builtins().String
		}
		elems := make([]Value, 0, len(argv))
		for _, s := range argv {
			h := vm.Heap.AllocString(strTy, s)
			elems = append(elems, MakeHandleString(h, strTy))
		}
		arrTy := types.NoTypeID
		if call.HasDst {
			arrTy = frame.Locals[call.Dst.Local].TypeID
		}
		arrH := vm.Heap.AllocArray(arrTy, elems)
		val := MakeHandleArray(arrH, arrTy)
		if call.HasDst {
			localID := call.Dst.Local
			vmErr := vm.writeLocal(frame, localID, val)
			if vmErr != nil {
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   val,
			})
		}

	case "rt_stdin_read_all":
		stdin := vm.RT.StdinReadAll()
		strTy := types.NoTypeID
		if vm.Types != nil {
			strTy = vm.Types.Builtins().String
		}
		h := vm.Heap.AllocString(strTy, stdin)
		val := MakeHandleString(h, strTy)
		if call.HasDst {
			localID := call.Dst.Local
			vmErr := vm.writeLocal(frame, localID, val)
			if vmErr != nil {
				return vmErr
			}
			*writes = append(*writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   val,
			})
		}

	case "rt_exit":
		code := 0
		if len(call.Args) > 0 {
			val, vmErr := vm.evalOperand(frame, &call.Args[0])
			if vmErr != nil {
				return vmErr
			}
			if val.Kind != VKInt {
				return vm.eb.typeMismatch("int", val.Kind.String())
			}
			code = int(val.Int)
		}
		vm.ExitCode = code
		vm.RT.Exit(code)

		// Drop all frames to ensure owned values are freed before leak check.
		vm.dropAllFrames()
		vm.checkLeaksOrPanic()

		vm.Halted = true
		vm.Stack = nil

	case "rt_parse_arg":
		if len(call.Args) == 0 {
			return vm.eb.makeError(PanicTypeMismatch, "rt_parse_arg requires 1 argument")
		}
		strVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		if strVal.Kind != VKHandleString {
			return vm.eb.typeMismatch("string", strVal.Kind.String())
		}

		s := vm.Heap.Get(strVal.H).Str
		// Argument is moved into rt_parse_arg; it is always consumed.
		vm.Heap.Free(strVal.H)

		if !call.HasDst {
			return nil
		}

		// For Step 0, only support int parsing
		// Check if destination type is int
		localID := call.Dst.Local
		localType := frame.Locals[localID].TypeID

		// Check if target type is int
		if vm.Types != nil {
			tt, ok := vm.Types.Lookup(localType)
			if ok && tt.Kind != types.KindInt {
				return vm.eb.unsupportedParseType(tt.Kind.String())
			}
		}

		intVal, err := vm.RT.ParseArgInt(s)
		if err != nil {
			return vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as int: %v", s, err))
		}
		val := MakeInt(int64(intVal), localType)
		vmErr = vm.writeLocal(frame, localID, val)
		if vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: localID,
			Name:    frame.Locals[localID].Name,
			Value:   val,
		})

	case "__to":
		if !call.HasDst {
			return vm.eb.makeError(PanicTypeMismatch, "__to requires a destination")
		}
		if len(call.Args) != 1 {
			return vm.eb.makeError(PanicTypeMismatch, "__to requires 1 argument")
		}
		srcVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		dstLocal := call.Dst.Local
		dstTy := frame.Locals[dstLocal].TypeID

		converted, vmErr := vm.evalIntrinsicTo(srcVal, dstTy)
		if vmErr != nil {
			return vmErr
		}
		vmErr = vm.writeLocal(frame, dstLocal, converted)
		if vmErr != nil {
			return vmErr
		}
		*writes = append(*writes, LocalWrite{
			LocalID: dstLocal,
			Name:    frame.Locals[dstLocal].Name,
			Value:   converted,
		})

	default:
		return vm.eb.unsupportedIntrinsic(name)
	}

	return nil
}

func (vm *VM) evalIntrinsicTo(src Value, dstType types.TypeID) (Value, *VMError) {
	dstValTy := vm.valueType(dstType)
	if vm.Types != nil {
		if dstValTy == vm.Types.Builtins().Int && src.Kind == VKHandleStruct {
			obj := vm.Heap.Get(src.H)
			layout, vmErr := vm.layouts.Struct(obj.TypeID)
			if vmErr != nil {
				return Value{}, vmErr
			}
			idx, ok := layout.IndexByName["code"]
			if !ok {
				return Value{}, vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("type#%d has no field \"code\" for __to(int)", obj.TypeID))
			}
			if idx < 0 || idx >= len(obj.Fields) {
				return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("field index %d out of bounds for type#%d", idx, obj.TypeID))
			}
			field := obj.Fields[idx]
			if field.Kind != VKInt {
				return Value{}, vm.eb.typeMismatch("int", field.Kind.String())
			}
			// __to consumes its argument.
			vm.Heap.Free(src.H)
			return MakeInt(field.Int, dstType), nil
		}
	}
	return Value{}, vm.eb.unimplemented("__to conversion")
}
