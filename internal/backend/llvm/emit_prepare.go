package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (e *Emitter) prepareGlobals() error {
	if e.mod == nil {
		return nil
	}
	for id := range e.mod.Globals {
		gid, err := safeGlobalID(id)
		if err != nil {
			return err
		}
		e.globalNames[gid] = fmt.Sprintf("g%d", id)
	}
	return nil
}

func (e *Emitter) prepareFunctions() error {
	if e.mod == nil {
		return nil
	}
	funcs := make([]*mir.Func, 0, len(e.mod.Funcs))
	for _, f := range e.mod.Funcs {
		if f != nil {
			funcs = append(funcs, f)
		}
	}
	for _, f := range funcs {
		name := fmt.Sprintf("fn.%d", f.ID)
		if f.Name == "__surge_start" {
			name = f.Name
		}
		e.funcNames[f.ID] = name
		paramLocals, err := e.paramLocals(f)
		if err != nil {
			return err
		}
		params := make([]string, 0, len(paramLocals))
		for _, localID := range paramLocals {
			if int(localID) < 0 || int(localID) >= len(f.Locals) {
				return fmt.Errorf("invalid param local %d", localID)
			}
			llvmTy, llvmErr := llvmValueType(e.types, f.Locals[localID].Type)
			if llvmErr != nil {
				return llvmErr
			}
			params = append(params, llvmTy)
		}
		ret, err := llvmType(e.types, f.Result)
		if err != nil {
			return err
		}
		if ret == "void" {
			inferred, inferErr := e.inferReturnType(f)
			if inferErr != nil {
				return inferErr
			}
			ret = inferred
		}
		e.funcSigs[f.ID] = funcSig{ret: ret, params: params}
	}
	return nil
}

func (e *Emitter) inferReturnType(f *mir.Func) (string, error) {
	if e == nil || e.types == nil || f == nil {
		return "void", nil
	}
	for i := range f.Blocks {
		term := &f.Blocks[i].Term
		if term.Kind != mir.TermReturn || !term.Return.HasValue {
			continue
		}
		op := &term.Return.Value
		typeID := operandValueType(e.types, op)
		if typeID == types.NoTypeID && op.Kind != mir.OperandConst {
			switch op.Place.Kind {
			case mir.PlaceLocal:
				if int(op.Place.Local) >= 0 && int(op.Place.Local) < len(f.Locals) {
					typeID = f.Locals[op.Place.Local].Type
				}
			case mir.PlaceGlobal:
				if e.mod != nil && int(op.Place.Global) >= 0 && int(op.Place.Global) < len(e.mod.Globals) {
					typeID = e.mod.Globals[op.Place.Global].Type
				}
			}
		}
		if typeID == types.NoTypeID {
			continue
		}
		ret, err := llvmType(e.types, typeID)
		if err != nil {
			return "", err
		}
		return ret, nil
	}
	return "void", nil
}

func (e *Emitter) collectParamCounts() error {
	if e.mod == nil {
		return nil
	}
	counts := make(map[mir.FuncID]int)
	nameToID := make(map[string]mir.FuncID, len(e.mod.Funcs))
	for id, f := range e.mod.Funcs {
		if f != nil && f.Name != "" {
			nameToID[f.Name] = mir.FuncID(id)
		}
	}
	for _, f := range e.mod.Funcs {
		if f == nil {
			continue
		}
		for i := range f.Blocks {
			bb := &f.Blocks[i]
			for j := range bb.Instrs {
				ins := &bb.Instrs[j]
				if ins.Kind != mir.InstrCall {
					continue
				}
				call := &ins.Call
				if call.Callee.Kind != mir.CalleeSym {
					continue
				}
				targetID := mir.NoFuncID
				if call.Callee.Sym.IsValid() {
					if id, ok := e.mod.FuncBySym[call.Callee.Sym]; ok {
						targetID = id
					}
				} else if call.Callee.Name != "" {
					if id, ok := nameToID[call.Callee.Name]; ok {
						targetID = id
					}
				}
				if targetID == mir.NoFuncID {
					continue
				}
				argCount := len(call.Args)
				if prev, ok := counts[targetID]; ok && prev != argCount {
					targetName := e.mod.Funcs[targetID].Name
					if targetName == "" {
						targetName = fmt.Sprintf("fn.%d", targetID)
					}
					return fmt.Errorf("function %s called with %d and %d args", targetName, prev, argCount)
				}
				counts[targetID] = argCount
			}
		}
	}
	e.paramCounts = counts
	return nil
}

func (e *Emitter) paramLocals(f *mir.Func) ([]mir.LocalID, error) {
	if f == nil {
		return nil, nil
	}
	if e.syms == nil || e.syms.Symbols == nil || e.syms.Strings == nil {
		return nil, fmt.Errorf("missing symbol table")
	}
	if f.Sym.IsValid() {
		sym := e.syms.Symbols.Get(f.Sym)
		if sym != nil {
			if e.types != nil && sym.Type != types.NoTypeID {
				if info, ok := e.types.FnInfo(sym.Type); ok {
					count := len(info.Params)
					if count > len(f.Locals) {
						return nil, fmt.Errorf("function %q has %d params but only %d locals", f.Name, count, len(f.Locals))
					}
					params := make([]mir.LocalID, count)
					for i := range params {
						localID, err := safeLocalID(i)
						if err != nil {
							return nil, err
						}
						params[i] = localID
					}
					return params, nil
				}
			}
			if sym.Signature != nil {
				count := len(sym.Signature.Params)
				if count > len(f.Locals) {
					return nil, fmt.Errorf("function %q has %d params but only %d locals", f.Name, count, len(f.Locals))
				}
				params := make([]mir.LocalID, count)
				for i := range params {
					localID, err := safeLocalID(i)
					if err != nil {
						return nil, err
					}
					params[i] = localID
				}
				return params, nil
			}
		}
	}
	if e.paramCounts != nil {
		if count, ok := e.paramCounts[f.ID]; ok {
			if count > len(f.Locals) {
				return nil, fmt.Errorf("function %q has %d params but only %d locals", f.Name, count, len(f.Locals))
			}
			params := make([]mir.LocalID, count)
			for i := range params {
				localID, err := safeLocalID(i)
				if err != nil {
					return nil, err
				}
				params[i] = localID
			}
			return params, nil
		}
	}
	params := make([]mir.LocalID, 0, len(f.Locals))
	for idx, local := range f.Locals {
		if !local.Sym.IsValid() {
			continue
		}
		sym := e.syms.Symbols.Get(local.Sym)
		if sym == nil {
			continue
		}
		if sym.Kind == symbols.SymbolParam {
			localID, err := safeLocalID(idx)
			if err != nil {
				return nil, err
			}
			params = append(params, localID)
		}
	}
	return params, nil
}
