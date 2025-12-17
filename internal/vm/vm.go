package vm

import (
	"fmt"

	"fortio.org/safecast"

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
	M          *mir.Module
	Stack      []Frame
	RT         Runtime
	Trace      *Tracer
	Files      *source.FileSet
	Types      *types.Interner
	Heap       *Heap
	layouts    *layoutCache
	tagLayouts *TagLayouts
	ExitCode   int
	Halted     bool

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
	vm.Heap = &Heap{
		next:        1,
		nextAllocID: 1,
		objs:        make(map[Handle]*Object, 128),
		vm:          vm,
	}
	vm.layouts = newLayoutCache(vm)
	vm.tagLayouts = NewTagLayouts(m)
	if vm.Trace != nil {
		vm.Trace.vm = vm
	}
	return vm
}

// Run executes the program starting from __surge_start.
// Returns a VMError if execution fails, nil on successful completion.
func (vm *VM) Run() (vmErr *VMError) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*VMError); ok {
				vmErr = e
				return
			}
			panic(r)
		}
	}()

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

func (vm *VM) panic(code PanicCode, msg string) {
	panic(vm.eb.makeError(code, msg))
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
	var (
		storeLoc Location
		storeVal Value
		hasStore bool
	)

	switch instr.Kind {
	case mir.InstrAssign:
		val, vmErr := vm.evalRValue(frame, &instr.Assign.Src)
		if vmErr != nil {
			return vmErr
		}
		dst := instr.Assign.Dst
		if len(dst.Proj) == 0 {
			localID := dst.Local
			vmErr = vm.writeLocal(frame, localID, val)
			if vmErr != nil {
				return vmErr
			}
			stored := frame.Locals[localID].V
			writes = append(writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   stored,
			})
		} else {
			loc, vmErr := vm.EvalPlace(frame, dst)
			if vmErr != nil {
				return vmErr
			}
			if vmErr := vm.storeLocation(loc, val); vmErr != nil {
				return vmErr
			}
			storeLoc = loc
			storeVal = val
			hasStore = true
		}

	case mir.InstrCall:
		vmErr := vm.execCall(frame, &instr.Call, &writes)
		if vmErr != nil {
			return vmErr
		}

	case mir.InstrDrop:
		localID := instr.Drop.Place.Local
		vmErr := vm.execDrop(frame, localID)
		if vmErr != nil {
			return vmErr
		}

	case mir.InstrEndBorrow:
		localID := instr.EndBorrow.Place.Local
		if int(localID) < 0 || int(localID) >= len(frame.Locals) {
			return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", localID))
		}
		slot := &frame.Locals[localID]
		slot.V = Value{}
		slot.IsInit = false
		slot.IsMoved = false

	case mir.InstrNop:
		// Nothing to do

	default:
		return vm.eb.unimplemented(fmt.Sprintf("instruction kind %d", instr.Kind))
	}

	// Trace the instruction
	if vm.Trace != nil {
		vm.Trace.TraceInstr(len(vm.Stack), frame.Func, frame.BB, frame.IP, instr, frame.Span, writes)
		if hasStore {
			vm.Trace.TraceStore(storeLoc, storeVal)
		}
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
		// Support selected intrinsics and extern calls that are not lowered into MIR.
		return vm.callIntrinsic(frame, call, writes)
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
		localID, err := safecast.Conv[mir.LocalID](i)
		if err != nil {
			return vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("invalid argument index %d", i))
		}
		if vmErr := vm.writeLocal(newFrame, localID, arg); vmErr != nil {
			return vmErr
		}
	}

	vm.Stack = append(vm.Stack, *newFrame)

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

		// Implicit drops before returning.
		vm.dropFrameLocals(frame)

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
		return vm.execSwitchTag(frame, &term.SwitchTag)

	case mir.TermUnreachable:
		return vm.eb.makeError(PanicUnimplemented, "unreachable code executed")

	default:
		return vm.eb.unimplemented(fmt.Sprintf("terminator kind %d", term.Kind))
	}

	return nil
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

	expectedType := frame.Locals[id].TypeID
	if val.TypeID == types.NoTypeID && expectedType != types.NoTypeID {
		val.TypeID = expectedType
	}
	if val.Kind == VKNothing && expectedType != types.NoTypeID && vm.tagLayouts != nil {
		if layout, ok := vm.tagLayouts.Layout(vm.valueType(expectedType)); ok && layout != nil {
			if tc, ok := layout.CaseByName("nothing"); ok {
				h := vm.Heap.AllocTag(expectedType, tc.TagSym, nil)
				val = MakeHandleTag(h, expectedType)
			}
		}
	}

	slot := &frame.Locals[id]
	if slot.IsInit && !slot.IsMoved && frame.Func != nil {
		if vm.localOwnsHeap(frame.Func.Locals[id]) {
			vm.dropValue(slot.V)
		}
	}
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

func (vm *VM) valueType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || vm.Types == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		seen++
		tt, ok := vm.Types.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := vm.Types.AliasTarget(id)
			if !ok || target == types.NoTypeID || target == id {
				return id
			}
			id = target
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}
