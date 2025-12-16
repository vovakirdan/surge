package vm

import (
	"fmt"
	"math"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Options configures VM execution.
type Options struct {
	Trace bool // Enable execution tracing
}

// VM is a direct MIR interpreter.
type VM struct {
	M        *mir.Module
	Stack    []Frame
	RT       Runtime
	Trace    *Tracer
	Files    *source.FileSet
	Types    *types.Interner
	ExitCode int
	Halted   bool

	eb *errorBuilder // for creating errors with backtrace
}

// New creates a new VM for executing the given MIR module.
func New(m *mir.Module, rt Runtime, files *source.FileSet, typeInterner *types.Interner, trace *Tracer) *VM {
	vm := &VM{
		M:        m,
		RT:       rt,
		Files:    files,
		Types:    typeInterner,
		Trace:    trace,
		ExitCode: 0,
		Halted:   false,
	}
	vm.eb = &errorBuilder{vm: vm}
	return vm
}

// Run executes the program starting from __surge_start.
// Returns a VMError if execution fails, nil on successful completion.
func (vm *VM) Run() *VMError {
	// Find __surge_start
	startFn := vm.findFunction("__surge_start")
	if startFn == nil {
		return vm.eb.makeError(PanicUnimplemented, "no entrypoint: __surge_start not found")
	}

	// Push initial frame
	vm.Stack = append(vm.Stack, *NewFrame(startFn))

	// Main execution loop
	for !vm.Halted && len(vm.Stack) > 0 {
		frame := &vm.Stack[len(vm.Stack)-1]
		block := frame.CurrentBlock()
		if block == nil {
			return vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("invalid block id: %d", frame.BB))
		}

		if frame.AtTerminator() {
			// Execute terminator
			if vmErr := vm.execTerminator(frame, &block.Term); vmErr != nil {
				return vmErr
			}
		} else {
			// Execute instruction
			instr := frame.CurrentInstr()
			if vmErr := vm.execInstr(frame, instr); vmErr != nil {
				return vmErr
			}
			frame.IP++
		}
	}

	return nil
}

// findFunction finds a function by name.
func (vm *VM) findFunction(name string) *mir.Func {
	for _, fn := range vm.M.Funcs {
		if fn.Name == name {
			return fn
		}
	}
	return nil
}

// findFunctionBySym finds a function by symbol ID.
func (vm *VM) findFunctionBySym(sym symbols.SymbolID) *mir.Func {
	if fid, ok := vm.M.FuncBySym[sym]; ok {
		return vm.M.Funcs[fid]
	}
	return nil
}

// execInstr executes a single instruction.
func (vm *VM) execInstr(frame *Frame, instr *mir.Instr) *VMError {
	// Update current span for error reporting
	// Span is attached to locals, not instructions directly
	// Use the destination local's span if available
	if instr.Kind == mir.InstrAssign {
		localID := instr.Assign.Dst.Local
		if int(localID) < len(frame.Func.Locals) {
			frame.Span = frame.Func.Locals[localID].Span
		}
	}

	var writes []LocalWrite

	switch instr.Kind {
	case mir.InstrAssign:
		val, vmErr := vm.evalRValue(frame, &instr.Assign.Src)
		if vmErr != nil {
			return vmErr
		}
		localID := instr.Assign.Dst.Local
		vmErr = vm.writeLocal(frame, localID, val)
		if vmErr != nil {
			return vmErr
		}
		writes = append(writes, LocalWrite{
			LocalID: localID,
			Name:    frame.Locals[localID].Name,
			Value:   val,
		})

	case mir.InstrCall:
		vmErr := vm.execCall(frame, &instr.Call, &writes)
		if vmErr != nil {
			return vmErr
		}

	case mir.InstrDrop:
		// No-op in Step 0, but trace it
		// In full implementation would run destructors

	case mir.InstrEndBorrow:
		// No-op in Step 0, but trace it
		// In full implementation would end borrow lifetime

	case mir.InstrNop:
		// Nothing to do

	default:
		return vm.eb.unimplemented(fmt.Sprintf("instruction kind %d", instr.Kind))
	}

	// Trace the instruction
	if vm.Trace != nil {
		vm.Trace.TraceInstr(len(vm.Stack), frame.Func, frame.BB, frame.IP, instr, frame.Span, writes)
	}

	return nil
}

// execCall executes a call instruction.
func (vm *VM) execCall(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	// Check if this is an intrinsic (no symbol ID)
	if call.Callee.Kind == mir.CalleeSym && !call.Callee.Sym.IsValid() {
		return vm.callIntrinsic(frame, call, writes)
	}

	// Find the function to call
	var targetFn *mir.Func
	if call.Callee.Kind == mir.CalleeSym {
		targetFn = vm.findFunctionBySym(call.Callee.Sym)
	}
	if targetFn == nil {
		targetFn = vm.findFunction(call.Callee.Name)
	}
	if targetFn == nil {
		return vm.eb.unsupportedIntrinsic(call.Callee.Name)
	}

	// Evaluate arguments
	args := make([]Value, len(call.Args))
	for i := range call.Args {
		val, vmErr := vm.evalOperand(frame, &call.Args[i])
		if vmErr != nil {
			return vmErr
		}
		args[i] = val
	}

	// Push new frame
	newFrame := NewFrame(targetFn)

	// Pass arguments as first locals (params)
	if len(args) > len(newFrame.Locals) {
		return vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("too many arguments: got %d, expected at most %d", len(args), len(newFrame.Locals)))
	}
	for i, arg := range args {
		newFrame.Locals[i].V = arg
		newFrame.Locals[i].IsInit = true
	}

	vm.Stack = append(vm.Stack, *newFrame)

	return nil
}

// callIntrinsic handles runtime intrinsic calls.
func (vm *VM) callIntrinsic(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) *VMError {
	name := call.Callee.Name

	switch name {
	case "rt_argv":
		argv := vm.RT.Argv()
		val := MakeStringSlice(argv, types.NoTypeID)
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
		val := MakeString(stdin, types.NoTypeID)
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
		if len(call.Args) > 0 {
			val, vmErr := vm.evalOperand(frame, &call.Args[0])
			if vmErr != nil {
				return vmErr
			}
			if val.Kind != VKInt {
				return vm.eb.typeMismatch("int", val.Kind.String())
			}
			vm.ExitCode = int(val.Int)
			vm.RT.Exit(int(val.Int))
		}
		vm.Halted = true

	case "rt_parse_arg":
		if len(call.Args) == 0 {
			return vm.eb.makeError(PanicTypeMismatch, "rt_parse_arg requires 1 argument")
		}
		strVal, vmErr := vm.evalOperand(frame, &call.Args[0])
		if vmErr != nil {
			return vmErr
		}
		if strVal.Kind != VKStringConst {
			return vm.eb.typeMismatch("string", strVal.Kind.String())
		}

		// For Step 0, only support int parsing
		// Check if destination type is int
		if call.HasDst {
			localID := call.Dst.Local
			localType := frame.Locals[localID].TypeID

			// Check if target type is int
			if vm.Types != nil {
				tt, ok := vm.Types.Lookup(localType)
				if ok && tt.Kind != types.KindInt {
					return vm.eb.unsupportedParseType(tt.Kind.String())
				}
			}

			intVal, err := vm.RT.ParseArgInt(strVal.Str)
			if err != nil {
				return vm.eb.makeError(PanicTypeMismatch, fmt.Sprintf("failed to parse %q as int: %v", strVal.Str, err))
			}
			val := MakeInt(int64(intVal), localType)
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

	default:
		return vm.eb.unsupportedIntrinsic(name)
	}

	return nil
}

// execTerminator executes a block terminator.
func (vm *VM) execTerminator(frame *Frame, term *mir.Terminator) *VMError {
	// Trace terminator before execution
	if vm.Trace != nil {
		vm.Trace.TraceTerm(len(vm.Stack), frame.Func, frame.BB, term, frame.Span)
	}

	switch term.Kind {
	case mir.TermReturn:
		// Get return value if any
		var retVal Value
		if term.Return.HasValue {
			val, vmErr := vm.evalOperand(frame, &term.Return.Value)
			if vmErr != nil {
				return vmErr
			}
			retVal = val
		}

		// Pop current frame
		vm.Stack = vm.Stack[:len(vm.Stack)-1]

		// If stack not empty, store return value in caller's destination
		if len(vm.Stack) > 0 {
			callerFrame := &vm.Stack[len(vm.Stack)-1]
			// The caller's IP points to the call instruction that was just executed
			// Find the call instruction and its destination
			block := callerFrame.CurrentBlock()
			if block != nil && callerFrame.IP < len(block.Instrs) {
				instr := &block.Instrs[callerFrame.IP]
				if instr.Kind == mir.InstrCall && instr.Call.HasDst {
					localID := instr.Call.Dst.Local
					vmErr := vm.writeLocal(callerFrame, localID, retVal)
					if vmErr != nil {
						return vmErr
					}
				}
			}
			// Advance caller's IP past the call
			callerFrame.IP++
		}

	case mir.TermGoto:
		frame.BB = term.Goto.Target
		frame.IP = 0

	case mir.TermIf:
		cond, vmErr := vm.evalOperand(frame, &term.If.Cond)
		if vmErr != nil {
			return vmErr
		}
		if cond.Kind != VKBool {
			return vm.eb.typeMismatch("bool", cond.Kind.String())
		}
		if cond.Bool {
			frame.BB = term.If.Then
		} else {
			frame.BB = term.If.Else
		}
		frame.IP = 0

	case mir.TermSwitchTag:
		return vm.eb.unimplemented("switch_tag terminator")

	case mir.TermUnreachable:
		return vm.eb.makeError(PanicUnimplemented, "unreachable code executed")

	default:
		return vm.eb.unimplemented(fmt.Sprintf("terminator kind %d", term.Kind))
	}

	return nil
}

// evalRValue evaluates an rvalue to a Value.
func (vm *VM) evalRValue(frame *Frame, rv *mir.RValue) (Value, *VMError) {
	switch rv.Kind {
	case mir.RValueUse:
		return vm.evalOperand(frame, &rv.Use)

	case mir.RValueBinaryOp:
		left, vmErr := vm.evalOperand(frame, &rv.Binary.Left)
		if vmErr != nil {
			return Value{}, vmErr
		}
		right, vmErr := vm.evalOperand(frame, &rv.Binary.Right)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.evalBinaryOp(rv.Binary.Op, left, right)

	case mir.RValueUnaryOp:
		operand, vmErr := vm.evalOperand(frame, &rv.Unary.Operand)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.evalUnaryOp(rv.Unary.Op, operand)

	case mir.RValueIndex:
		obj, vmErr := vm.evalOperand(frame, &rv.Index.Object)
		if vmErr != nil {
			return Value{}, vmErr
		}
		idx, vmErr := vm.evalOperand(frame, &rv.Index.Index)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return vm.evalIndex(obj, idx)

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("rvalue kind %d", rv.Kind))
	}
}

// evalOperand evaluates an operand to a Value.
func (vm *VM) evalOperand(frame *Frame, op *mir.Operand) (Value, *VMError) {
	switch op.Kind {
	case mir.OperandConst:
		return vm.evalConst(&op.Const), nil

	case mir.OperandCopy:
		val, vmErr := vm.readLocal(frame, op.Place.Local)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return val, nil

	case mir.OperandMove:
		val, vmErr := vm.readLocal(frame, op.Place.Local)
		if vmErr != nil {
			return Value{}, vmErr
		}
		vm.moveLocal(frame, op.Place.Local)
		return val, nil

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("operand kind %d", op.Kind))
	}
}

// evalConst converts a MIR constant to a Value.
func (vm *VM) evalConst(c *mir.Const) Value {
	switch c.Kind {
	case mir.ConstInt:
		return MakeInt(c.IntValue, c.Type)
	case mir.ConstUint:
		intVal, err := safecast.Convert[int64](c.UintValue)
		if err != nil {
			// Could return error here if strict overflow checking is desired
			// For now, saturate to max int64
			intVal = math.MaxInt64
		}
		return MakeInt(intVal, c.Type)
	case mir.ConstBool:
		return MakeBool(c.BoolValue, c.Type)
	case mir.ConstString:
		return MakeString(c.StringValue, c.Type)
	case mir.ConstNothing:
		return MakeNothing()
	default:
		return Value{Kind: VKInvalid}
	}
}

// evalBinaryOp evaluates a binary operation.
func (vm *VM) evalBinaryOp(op ast.ExprBinaryOp, left, right Value) (Value, *VMError) {
	switch op {
	case ast.ExprBinaryAdd:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeInt(left.Int+right.Int, left.TypeID), nil

	case ast.ExprBinarySub:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeInt(left.Int-right.Int, left.TypeID), nil

	case ast.ExprBinaryMul:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeInt(left.Int*right.Int, left.TypeID), nil

	case ast.ExprBinaryDiv:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		if right.Int == 0 {
			return Value{}, vm.eb.makeError(PanicOutOfBounds, "division by zero")
		}
		return MakeInt(left.Int/right.Int, left.TypeID), nil

	case ast.ExprBinaryEq:
		if left.Kind != right.Kind {
			return Value{}, vm.eb.typeMismatch(left.Kind.String(), right.Kind.String())
		}
		var result bool
		switch left.Kind {
		case VKInt:
			result = left.Int == right.Int
		case VKBool:
			result = left.Bool == right.Bool
		case VKStringConst:
			result = left.Str == right.Str
		default:
			result = false
		}
		return MakeBool(result, types.NoTypeID), nil

	case ast.ExprBinaryNotEq:
		if left.Kind != right.Kind {
			return Value{}, vm.eb.typeMismatch(left.Kind.String(), right.Kind.String())
		}
		var result bool
		switch left.Kind {
		case VKInt:
			result = left.Int != right.Int
		case VKBool:
			result = left.Bool != right.Bool
		case VKStringConst:
			result = left.Str != right.Str
		default:
			result = true
		}
		return MakeBool(result, types.NoTypeID), nil

	case ast.ExprBinaryLess:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Int < right.Int, types.NoTypeID), nil

	case ast.ExprBinaryLessEq:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Int <= right.Int, types.NoTypeID), nil

	case ast.ExprBinaryGreater:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Int > right.Int, types.NoTypeID), nil

	case ast.ExprBinaryGreaterEq:
		if left.Kind != VKInt || right.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", fmt.Sprintf("%s and %s", left.Kind, right.Kind))
		}
		return MakeBool(left.Int >= right.Int, types.NoTypeID), nil

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("binary op %s", op))
	}
}

// evalUnaryOp evaluates a unary operation.
func (vm *VM) evalUnaryOp(op ast.ExprUnaryOp, operand Value) (Value, *VMError) {
	switch op {
	case ast.ExprUnaryMinus:
		if operand.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", operand.Kind.String())
		}
		return MakeInt(-operand.Int, operand.TypeID), nil

	case ast.ExprUnaryNot:
		if operand.Kind != VKBool {
			return Value{}, vm.eb.typeMismatch("bool", operand.Kind.String())
		}
		return MakeBool(!operand.Bool, operand.TypeID), nil

	case ast.ExprUnaryPlus:
		// Unary plus is a no-op for integers
		if operand.Kind != VKInt {
			return Value{}, vm.eb.typeMismatch("int", operand.Kind.String())
		}
		return operand, nil

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("unary op %s", op))
	}
}

// evalIndex evaluates an index operation.
func (vm *VM) evalIndex(obj, idx Value) (Value, *VMError) {
	if idx.Kind != VKInt {
		return Value{}, vm.eb.typeMismatch("int", idx.Kind.String())
	}
	index := int(idx.Int)

	switch obj.Kind {
	case VKStringSlice:
		if index < 0 || index >= len(obj.Strs) {
			return Value{}, vm.eb.outOfBounds(index, len(obj.Strs))
		}
		return MakeString(obj.Strs[index], types.NoTypeID), nil

	default:
		return Value{}, vm.eb.unimplemented(fmt.Sprintf("indexing %s", obj.Kind))
	}
}

// readLocal reads a local variable, checking initialization and move status.
func (vm *VM) readLocal(frame *Frame, id mir.LocalID) (Value, *VMError) {
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", id))
	}

	slot := &frame.Locals[id]

	if !slot.IsInit {
		return Value{}, vm.eb.useBeforeInit(slot.Name)
	}

	if slot.IsMoved {
		return Value{}, vm.eb.useAfterMove(slot.Name)
	}

	return slot.V, nil
}

// writeLocal writes a value to a local variable.
func (vm *VM) writeLocal(frame *Frame, id mir.LocalID, val Value) *VMError {
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", id))
	}

	slot := &frame.Locals[id]
	slot.V = val
	slot.IsInit = true
	slot.IsMoved = false
	return nil
}

// moveLocal marks a local as moved.
func (vm *VM) moveLocal(frame *Frame, id mir.LocalID) {
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return
	}
	frame.Locals[id].IsMoved = true
}
