package vm

import (
	"surge/internal/mir"
	"surge/internal/symbols"
)

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
	case mir.InstrAwait:
		localID := instr.Await.Dst.Local
		if int(localID) < len(frame.Func.Locals) {
			frame.Span = frame.Func.Locals[localID].Span
		}
	case mir.InstrSpawn:
		localID := instr.Spawn.Dst.Local
		if int(localID) < len(frame.Func.Locals) {
			frame.Span = frame.Func.Locals[localID].Span
		}
	case mir.InstrPoll:
		localID := instr.Poll.Dst.Local
		if int(localID) < len(frame.Func.Locals) {
			frame.Span = frame.Func.Locals[localID].Span
		}
	case mir.InstrSelect:
		localID := instr.Select.Dst.Local
		if int(localID) < len(frame.Func.Locals) {
			frame.Span = frame.Func.Locals[localID].Span
		}
	}
}
