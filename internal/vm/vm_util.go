package vm

import (
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
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

func (vm *VM) resolveCallTarget(frame *Frame, call *mir.CallInstr) *mir.Func {
	if vm == nil || vm.M == nil || call == nil {
		return nil
	}
	if call.Callee.Kind == mir.CalleeSym && call.Callee.Sym.IsValid() {
		if fn := vm.findFunctionBySym(call.Callee.Sym); fn != nil {
			return fn
		}
	}

	name := call.Callee.Name
	if name == "" {
		return nil
	}
	if shouldDeferToIntrinsicFallback(name) {
		return nil
	}

	argTypes := make([]types.TypeID, 0, len(call.Args))
	for i := range call.Args {
		argTypes = append(argTypes, call.Args[i].Type)
	}
	resultType := types.NoTypeID
	if frame != nil && frame.Func != nil && call.HasDst {
		if idx := int(call.Dst.Local); idx >= 0 && idx < len(frame.Func.Locals) {
			resultType = frame.Func.Locals[idx].Type
		}
	}

	return vm.findFunctionBySignature(name, argTypes, resultType)
}

func (vm *VM) findFunctionBySignature(name string, argTypes []types.TypeID, resultType types.TypeID) *mir.Func {
	if vm == nil || vm.M == nil || name == "" {
		return nil
	}
	exact := vm.collectFunctionCandidates(name, true)
	if fn := vm.pickFunctionCandidate(exact, argTypes, resultType); fn != nil {
		return fn
	}
	base := stripGenericSuffix(name)
	if base == "" || base == name {
		return nil
	}
	return vm.pickFunctionCandidate(vm.collectFunctionCandidates(base, false), argTypes, resultType)
}

func (vm *VM) collectFunctionCandidates(name string, exact bool) []*mir.Func {
	candidates := make([]*mir.Func, 0, 4)
	for _, fn := range vm.M.Funcs {
		if fn == nil {
			continue
		}
		if exact {
			if fn.Name == name {
				candidates = append(candidates, fn)
			}
			continue
		}
		if stripGenericSuffix(fn.Name) == name {
			candidates = append(candidates, fn)
		}
	}
	return candidates
}

func (vm *VM) pickFunctionCandidate(candidates []*mir.Func, argTypes []types.TypeID, resultType types.TypeID) *mir.Func {
	if len(candidates) == 0 {
		return nil
	}

	steps := []func(*mir.Func) bool{
		func(fn *mir.Func) bool { return vm.functionSignatureMatches(fn, argTypes, resultType, true) },
		func(fn *mir.Func) bool { return vm.functionSignatureMatches(fn, argTypes, types.NoTypeID, true) },
		func(fn *mir.Func) bool { return vm.functionSignatureMatches(fn, argTypes, resultType, false) },
		func(fn *mir.Func) bool { return vm.functionSignatureMatches(fn, argTypes, types.NoTypeID, false) },
	}
	for _, step := range steps {
		var match *mir.Func
		for _, fn := range candidates {
			if !step(fn) {
				continue
			}
			if match != nil {
				match = nil
				break
			}
			match = fn
		}
		if match != nil {
			return match
		}
	}
	return nil
}

func (vm *VM) functionSignatureMatches(fn *mir.Func, argTypes []types.TypeID, resultType types.TypeID, strictArgs bool) bool {
	if fn == nil {
		return false
	}
	if fn.ParamCount != len(argTypes) {
		return false
	}
	if resultType != types.NoTypeID && !vm.sameCallType(fn.Result, resultType) {
		return false
	}
	if !strictArgs {
		return true
	}
	for i, argType := range argTypes {
		if argType == types.NoTypeID || i >= len(fn.Locals) {
			continue
		}
		if !vm.sameCallType(fn.Locals[i].Type, argType) {
			return false
		}
	}
	return true
}

func (vm *VM) sameCallType(a, b types.TypeID) bool {
	return vm.normalizeCallType(a) == vm.normalizeCallType(b)
}

func (vm *VM) normalizeCallType(id types.TypeID) types.TypeID {
	if vm == nil || vm.Types == nil {
		return id
	}
	seen := make(map[types.TypeID]struct{}, 8)
	for id != types.NoTypeID {
		if _, ok := seen[id]; ok {
			return id
		}
		seen[id] = struct{}{}
		id = resolveAlias(vm.Types, id)
		tt, ok := vm.Types.Lookup(id)
		if !ok || tt.Kind != types.KindOwn {
			return id
		}
		id = tt.Elem
	}
	return types.NoTypeID
}

func stripGenericSuffix(name string) string {
	if idx := strings.Index(name, "::<"); idx >= 0 {
		return name[:idx]
	}
	return name
}

func shouldDeferToIntrinsicFallback(name string) bool {
	base := stripGenericSuffix(name)
	if base == "" {
		return false
	}
	if strings.HasPrefix(base, "__") || strings.HasPrefix(base, "rt_") {
		return true
	}
	switch base {
	case "size_of", "align_of", "default", "from_str", "from_bytes":
		return true
	default:
		return false
	}
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
