package vm

import (
	"fmt"

	"surge/internal/asyncrt"
	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

// Options configures VM execution.
type Options struct {
	Trace bool // Enable execution tracing
}

// VM is a direct MIR interpreter.
type VM struct {
	M             *mir.Module
	Stack         []Frame
	Globals       []LocalSlot
	RT            Runtime
	Recorder      *Recorder
	Replayer      *Replayer
	Trace         *Tracer
	Files         *source.FileSet
	Types         *types.Interner
	Layout        *layout.LayoutEngine
	Heap          *Heap
	rawMem        *rawMemory
	heapCounters  heapCounters
	layouts       *layoutCache
	tagLayouts    *TagLayouts
	Async         *asyncrt.Executor
	AsyncConfig   asyncrt.Config
	ExitCode      int
	Halted        bool
	started       bool
	fsFiles       map[uint64]*vmFile
	fsNextHandle  uint64
	netListeners  map[uint64]*vmNetListener
	netConns      map[uint64]*vmNetConn
	netNextListen uint64
	netNextConn   uint64

	eb                  *errorBuilder // for creating errors with backtrace
	captureReturn       *Value
	asyncCapture        *asyncExit
	asyncPendingParkKey asyncrt.WakerKey
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
	vm.fsFiles = make(map[uint64]*vmFile)
	vm.fsNextHandle = 1
	vm.netListeners = make(map[uint64]*vmNetListener)
	vm.netConns = make(map[uint64]*vmNetConn)
	vm.netNextListen = 1
	vm.netNextConn = 1
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
		vm.started = true
		vm.Halted = true
		vm.Stack = nil
		return nil
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
