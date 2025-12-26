package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/layout"
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
	Globals    []LocalSlot
	RT         Runtime
	Recorder   *Recorder
	Replayer   *Replayer
	Trace      *Tracer
	Files      *source.FileSet
	Types      *types.Interner
	Layout     *layout.LayoutEngine
	Heap       *Heap
	rawMem     *rawMemory
	layouts    *layoutCache
	tagLayouts *TagLayouts
	ExitCode   int
	Halted     bool
	started    bool

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
	if m != nil && m.Meta != nil && m.Meta.Layout != nil {
		vm.Layout = m.Meta.Layout
	} else {
		vm.Layout = layout.New(layout.X86_64LinuxGNU(), typeInterner)
	}
	vm.eb = &errorBuilder{vm: vm}
	vm.Heap = &Heap{
		next:        1,
		nextAllocID: 1,
		objs:        make(map[Handle]*Object, 128),
		vm:          vm,
	}
	vm.rawMem = newRawMemory()
	vm.layouts = newLayoutCache(vm)
	vm.tagLayouts = NewTagLayouts(m)
	if m != nil && len(m.Globals) != 0 {
		vm.Globals = make([]LocalSlot, len(m.Globals))
		for i, g := range m.Globals {
			vm.Globals[i] = LocalSlot{
				Name:   g.Name,
				TypeID: g.Type,
			}
		}
	}
	if vm.Trace != nil {
		vm.Trace.vm = vm
	}
	return vm
}

// StopPoint describes the current instruction/terminator that would execute next.
type StopPoint struct {
	FuncName string
	BB       mir.BlockID
	IP       int
	Span     source.Span

	IsTerm bool
	Instr  *mir.Instr
	Term   *mir.Terminator
}

// Run executes the program starting from __surge_start.
// Returns a VMError if execution fails, nil on successful completion.
func (vm *VM) Run() (vmErr *VMError) {
	if vmErr := vm.Start(); vmErr != nil {
		return vmErr
	}
	for !vm.Halted && len(vm.Stack) > 0 {
		if stepErr := vm.Step(); stepErr != nil {
			if vm.Replayer != nil {
				stepErr = vm.Replayer.CheckPanic(vm, stepErr)
			}
			if vm.Recorder != nil {
				vm.Recorder.RecordPanic(stepErr, vm.Files)
			}
			return stepErr
		}
	}

	if vm.Replayer != nil {
		if vmErr := vm.Replayer.FinalizeExit(vm, vm.ExitCode); vmErr != nil {
			return vmErr
		}
	}
	if vm.Recorder != nil && !vm.Recorder.Done() {
		vm.Recorder.RecordExit(vm.ExitCode)
	}
	return nil
}

// Start initializes execution by pushing the initial __surge_start frame.
func (vm *VM) Start() *VMError {
	if vm.started || vm.Halted {
		return nil
	}
	if len(vm.Stack) != 0 {
		vm.started = true
		return nil
	}

	// Find __surge_start.
	startFn := vm.findFunction("__surge_start")
	if startFn == nil {
		return vm.eb.makeError(PanicUnimplemented, "no entrypoint: __surge_start not found")
	}

	vm.Stack = append(vm.Stack, *NewFrame(startFn))
	vm.started = true

	if vm.Replayer != nil {
		if err := vm.Replayer.Validate(); err != nil {
			return vm.eb.invalidReplayLogFormat(err.Error())
		}
	}
	return nil
}

// Step executes exactly one instruction or terminator transition.
// It returns a VMError if execution fails.
func (vm *VM) Step() (vmErr *VMError) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*VMError); ok {
				vmErr = e
				return
			}
			panic(r)
		}
	}()

	if vm.Halted || len(vm.Stack) == 0 {
		return nil
	}

	preDepth := len(vm.Stack)
	frameIdx := preDepth - 1
	frame := &vm.Stack[frameIdx]
	block := frame.CurrentBlock()
	if block == nil {
		return vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("invalid block id: %d", frame.BB))
	}

	if frame.AtTerminator() {
		return vm.execTerminator(frame, &block.Term)
	}

	instr := frame.CurrentInstr()
	if instr == nil {
		return vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("invalid instruction pointer: ip=%d", frame.IP))
	}
	vm.setSpanForInstr(frame, instr)

	advanceIP, pushFrame, vmErr := vm.execInstr(frame, instr)
	if vmErr != nil {
		return vmErr
	}
	if pushFrame != nil {
		vm.Stack = append(vm.Stack, *pushFrame)
		return nil
	}
	if advanceIP && !vm.Halted && len(vm.Stack) == preDepth {
		vm.Stack[frameIdx].IP++
	}
	return nil
}

// StopPoint returns the next instruction/terminator that would execute.
// ok=false indicates the VM is halted or has finished execution.
func (vm *VM) StopPoint() (sp StopPoint, ok bool) {
	if vm == nil || vm.Halted || len(vm.Stack) == 0 {
		return StopPoint{}, false
	}

	frame := &vm.Stack[len(vm.Stack)-1]
	block := frame.CurrentBlock()
	if block == nil {
		return StopPoint{}, false
	}

	sp = StopPoint{
		FuncName: frame.Func.Name,
		BB:       frame.BB,
		IP:       frame.IP,
		Span:     frame.Span,
	}

	if frame.AtTerminator() {
		sp.IsTerm = true
		sp.Term = &block.Term
		return sp, true
	}

	instr := frame.CurrentInstr()
	if instr == nil {
		return StopPoint{}, false
	}
	vm.setSpanForInstr(frame, instr)
	sp.Span = frame.Span
	sp.Instr = instr
	return sp, true
}

func (vm *VM) setSpanForInstr(frame *Frame, instr *mir.Instr) {
	if frame == nil || frame.Func == nil || instr == nil {
		return
	}
	switch instr.Kind {
	case mir.InstrAssign:
		localID := instr.Assign.Dst.Local
		if int(localID) < len(frame.Func.Locals) {
			frame.Span = frame.Func.Locals[localID].Span
		}
	case mir.InstrCall:
		if instr.Call.HasDst {
			localID := instr.Call.Dst.Local
			if int(localID) < len(frame.Func.Locals) {
				frame.Span = frame.Func.Locals[localID].Span
			}
		}
	}
}

// RunUntilStop runs the VM until it halts, panics, or stopFn returns true for the current stop point.
// When stopFn triggers, the VM is stopped *before* executing that stop point.
func (vm *VM) RunUntilStop(stopFn func(StopPoint) (breakID int, ok bool)) (stop StopPoint, breakID int, stopped bool, vmErr *VMError) {
	for !vm.Halted && len(vm.Stack) > 0 {
		sp, ok := vm.StopPoint()
		if !ok {
			break
		}
		if stopFn != nil {
			if id, hit := stopFn(sp); hit {
				return sp, id, true, nil
			}
		}
		if vmErr := vm.Step(); vmErr != nil {
			return StopPoint{}, 0, false, vmErr
		}
	}
	return StopPoint{}, 0, false, nil
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
func (vm *VM) execInstr(frame *Frame, instr *mir.Instr) (advanceIP bool, pushFrame *Frame, vmErr *VMError) {
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
			return false, nil, vmErr
		}
		dst := instr.Assign.Dst
		if len(dst.Proj) == 0 {
			switch dst.Kind {
			case mir.PlaceGlobal:
				vmErr = vm.writeGlobal(dst.Global, val)
				if vmErr != nil {
					return false, nil, vmErr
				}
				storeLoc = Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}
				storeVal = val
				hasStore = true
			default:
				localID := dst.Local
				vmErr = vm.writeLocal(frame, localID, val)
				if vmErr != nil {
					return false, nil, vmErr
				}
				stored := frame.Locals[localID].V
				writes = append(writes, LocalWrite{
					LocalID: localID,
					Name:    frame.Locals[localID].Name,
					Value:   stored,
				})
			}
		} else {
			loc, vmErr := vm.EvalPlace(frame, dst)
			if vmErr != nil {
				return false, nil, vmErr
			}
			if vmErr := vm.storeLocation(loc, val); vmErr != nil {
				return false, nil, vmErr
			}
			storeLoc = loc
			storeVal = val
			hasStore = true
		}

	case mir.InstrCall:
		newFrame, vmErr := vm.execCall(frame, &instr.Call, &writes)
		if vmErr != nil {
			return false, nil, vmErr
		}
		if newFrame != nil {
			pushFrame = newFrame
		}

	case mir.InstrDrop:
		switch instr.Drop.Place.Kind {
		case mir.PlaceGlobal:
			vmErr := vm.execDropGlobal(instr.Drop.Place.Global)
			if vmErr != nil {
				return false, nil, vmErr
			}
		default:
			localID := instr.Drop.Place.Local
			vmErr := vm.execDrop(frame, localID)
			if vmErr != nil {
				return false, nil, vmErr
			}
		}

	case mir.InstrEndBorrow:
		switch instr.EndBorrow.Place.Kind {
		case mir.PlaceGlobal:
			globalID := instr.EndBorrow.Place.Global
			if int(globalID) < 0 || int(globalID) >= len(vm.Globals) {
				return false, nil, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", globalID))
			}
			slot := &vm.Globals[globalID]
			slot.V = Value{}
			slot.IsInit = false
			slot.IsMoved = false
			slot.IsDropped = false
		default:
			localID := instr.EndBorrow.Place.Local
			if int(localID) < 0 || int(localID) >= len(frame.Locals) {
				return false, nil, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", localID))
			}
			slot := &frame.Locals[localID]
			slot.V = Value{}
			slot.IsInit = false
			slot.IsMoved = false
			slot.IsDropped = false
		}

	case mir.InstrNop:
		// Nothing to do

	default:
		return false, nil, vm.eb.unimplemented(fmt.Sprintf("instruction kind %d", instr.Kind))
	}

	// Trace the instruction
	if vm.Trace != nil {
		vm.Trace.TraceInstr(len(vm.Stack), frame.Func, frame.BB, frame.IP, instr, frame.Span, writes)
		if hasStore {
			vm.Trace.TraceStore(storeLoc, storeVal)
		}
	}

	if pushFrame != nil {
		return false, pushFrame, nil
	}
	return true, nil, nil
}

// execCall executes a call instruction.
func (vm *VM) execCall(frame *Frame, call *mir.CallInstr, writes *[]LocalWrite) (*Frame, *VMError) {
	// Find the function to call.
	var targetFn *mir.Func
	switch call.Callee.Kind {
	case mir.CalleeSym:
		if !call.Callee.Sym.IsValid() {
			return nil, vm.callIntrinsic(frame, call, writes)
		}
		targetFn = vm.findFunctionBySym(call.Callee.Sym)
		if targetFn == nil {
			// Support selected intrinsics and extern calls that are not lowered into MIR.
			return nil, vm.callIntrinsic(frame, call, writes)
		}
	case mir.CalleeValue:
		targetFn = vm.findFunction(call.Callee.Name)
		if targetFn == nil {
			return nil, vm.callIntrinsic(frame, call, writes)
		}
	default:
		return nil, vm.eb.unimplemented("unknown call target")
	}

	// Evaluate arguments
	args := make([]Value, len(call.Args))
	for i := range call.Args {
		val, vmErr := vm.evalOperand(frame, &call.Args[i])
		if vmErr != nil {
			return nil, vmErr
		}
		args[i] = val
	}

	// Push new frame
	newFrame := NewFrame(targetFn)

	// Pass arguments as first locals (params)
	if len(args) > len(newFrame.Locals) {
		return nil, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("too many arguments: got %d, expected at most %d", len(args), len(newFrame.Locals)))
	}
	for i, arg := range args {
		localID, err := safecast.Conv[mir.LocalID](i)
		if err != nil {
			return nil, vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("invalid argument index %d", i))
		}
		if vmErr := vm.writeLocal(newFrame, localID, arg); vmErr != nil {
			return nil, vmErr
		}
	}

	return newFrame, nil
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

	if slot.IsDropped {
		return Value{}, vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: local %q used after drop", slot.Name))
	}

	if slot.IsMoved {
		return Value{}, vm.eb.useAfterMove(slot.Name)
	}

	return slot.V, nil
}

// readGlobal reads a global variable, checking initialization and move status.
func (vm *VM) readGlobal(id mir.GlobalID) (Value, *VMError) {
	if int(id) < 0 || int(id) >= len(vm.Globals) {
		return Value{}, vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", id))
	}

	slot := &vm.Globals[id]

	if !slot.IsInit {
		return Value{}, vm.eb.useBeforeInit(slot.Name)
	}

	if slot.IsDropped {
		return Value{}, vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: global %q used after drop", slot.Name))
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
		if tagLayout, ok := vm.tagLayouts.Layout(vm.valueType(expectedType)); ok && tagLayout != nil {
			if tc, ok := tagLayout.CaseByName("nothing"); ok {
				h := vm.Heap.AllocTag(expectedType, tc.TagSym, nil)
				val = MakeHandleTag(h, expectedType)
			}
		}
	}

	slot := &frame.Locals[id]
	if slot.IsInit && !slot.IsMoved && !slot.IsDropped {
		vm.dropValue(slot.V)
	}
	slot.V = val
	slot.IsInit = true
	slot.IsMoved = false
	slot.IsDropped = false
	return nil
}

// writeGlobal writes a value to a global variable.
func (vm *VM) writeGlobal(id mir.GlobalID, val Value) *VMError {
	if int(id) < 0 || int(id) >= len(vm.Globals) {
		return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", id))
	}

	expectedType := vm.Globals[id].TypeID
	if val.TypeID == types.NoTypeID && expectedType != types.NoTypeID {
		val.TypeID = expectedType
	}
	if val.Kind == VKNothing && expectedType != types.NoTypeID && vm.tagLayouts != nil {
		if tagLayout, ok := vm.tagLayouts.Layout(vm.valueType(expectedType)); ok && tagLayout != nil {
			if tc, ok := tagLayout.CaseByName("nothing"); ok {
				h := vm.Heap.AllocTag(expectedType, tc.TagSym, nil)
				val = MakeHandleTag(h, expectedType)
			}
		}
	}

	slot := &vm.Globals[id]
	if slot.IsInit && !slot.IsMoved && !slot.IsDropped {
		vm.dropValue(slot.V)
	}
	slot.V = val
	slot.IsInit = true
	slot.IsMoved = false
	slot.IsDropped = false
	return nil
}

// moveLocal marks a local as moved.
func (vm *VM) moveLocal(frame *Frame, id mir.LocalID) {
	if int(id) < 0 || int(id) >= len(frame.Locals) {
		return
	}
	frame.Locals[id].IsMoved = true
}

// moveGlobal marks a global as moved.
func (vm *VM) moveGlobal(id mir.GlobalID) {
	if int(id) < 0 || int(id) >= len(vm.Globals) {
		return
	}
	vm.Globals[id].IsMoved = true
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
