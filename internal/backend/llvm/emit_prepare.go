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
	for _, __id := range e.mod.SortedFuncIDs() {
		f := e.mod.Funcs[__id]
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
		paramTypes := make([]types.TypeID, 0, len(paramLocals))
		for _, localID := range paramLocals {
			if int(localID) < 0 || int(localID) >= len(f.Locals) {
				return fmt.Errorf("invalid param local %d", localID)
			}
			llvmTy, llvmErr := e.llvmLocalValueType(f.Locals[localID])
			if llvmErr != nil {
				return llvmErr
			}
			params = append(params, llvmTy)
			paramTypes = append(paramTypes, f.Locals[localID].Type)
		}
		result := f.Result
		ret, err := e.llvmType(result)
		if err != nil {
			return err
		}
		if ret == "void" {
			// A lowered function can carry its result only in the operands it
			// returns. The inferred type is adopted as THE result type, not just
			// as an LLVM spelling, so the call contract below classifies the
			// same result the signature declares.
			inferred := e.inferResultType(f)
			if inferred != types.NoTypeID {
				result = inferred
				ret, err = e.llvmType(result)
				if err != nil {
					return err
				}
			}
		}
		abi, err := e.surgeABIForSignature(paramTypes, result)
		if err != nil {
			return fmt.Errorf("call contract for %s: %w", f.Name, err)
		}
		e.funcSigs[f.ID] = funcSig{
			ret: ret, params: params, paramTypes: paramTypes, resultType: result, abi: abi,
		}
	}
	return nil
}

// inferResultType is the type of the first value a lowered function returns,
// for functions whose declared result did not survive lowering.
func (e *Emitter) inferResultType(f *mir.Func) types.TypeID {
	if e == nil || e.types == nil || f == nil {
		return types.NoTypeID
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
		return typeID
	}
	return types.NoTypeID
}

func (e *Emitter) collectParamCounts() error {
	if e.mod == nil {
		return nil
	}
	counts := make(map[mir.FuncID]int)
	nameToID := make(map[string]mir.FuncID, len(e.mod.Funcs))
	for _, __id := range e.mod.SortedFuncIDs() {
		id := __id
		f := e.mod.Funcs[__id]
		if f != nil && f.Name != "" {
			nameToID[f.Name] = mir.FuncID(id)
		}
	}
	for _, __id := range e.mod.SortedFuncIDs() {
		f := e.mod.Funcs[__id]
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
	if isPollFunc(f) {
		return nil, nil
	}
	if f.ParamCount > 0 {
		if f.ParamCount > len(f.Locals) {
			return nil, fmt.Errorf("function %q has %d params but only %d locals", f.Name, f.ParamCount, len(f.Locals))
		}
		params := make([]mir.LocalID, f.ParamCount)
		for i := range params {
			localID, err := safeLocalID(i)
			if err != nil {
				return nil, err
			}
			params[i] = localID
		}
		return params, nil
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
