package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/types"
)

func (e *Emitter) funcByName(name string) (mir.FuncID, bool) {
	if e == nil || e.mod == nil {
		return mir.NoFuncID, false
	}
	base := stripGenericSuffix(name)
	for _, __id := range e.mod.SortedFuncIDs() {
		id := __id
		f := e.mod.Funcs[__id]
		if f == nil {
			continue
		}
		if f.Name == base {
			return mir.FuncID(id), true
		}
	}
	return mir.NoFuncID, false
}

func (e *Emitter) funcByExactName(name string) (mir.FuncID, bool) {
	if e == nil || e.mod == nil || name == "" {
		return mir.NoFuncID, false
	}
	for _, __id := range e.mod.SortedFuncIDs() {
		id := __id
		f := e.mod.Funcs[__id]
		if f == nil {
			continue
		}
		if f.Name == name {
			return mir.FuncID(id), true
		}
	}
	return mir.NoFuncID, false
}

func (e *Emitter) resolveFuncIDForCall(current *mir.Func, call *mir.CallInstr) (mir.FuncID, bool) {
	if e == nil || e.mod == nil || call == nil {
		return mir.NoFuncID, false
	}
	if call.Callee.Kind == mir.CalleeSym && call.Callee.Sym.IsValid() {
		if id, ok := e.mod.FuncBySym[call.Callee.Sym]; ok {
			return id, true
		}
	}
	if call.Callee.Kind == mir.CalleeValue {
		name := call.Callee.Name
		if name == "" {
			return mir.NoFuncID, false
		}
		if id, ok := e.funcByExactName(name); ok {
			return id, true
		}
		return e.funcByName(name)
	}

	name := call.Callee.Name
	if name == "" && call.Callee.Kind == mir.CalleeSym {
		name = e.symbolName(call.Callee.Sym)
	}
	if name == "" {
		return mir.NoFuncID, false
	}
	if shouldDeferToIntrinsicFallback(name) {
		return mir.NoFuncID, false
	}

	argTypes := make([]types.TypeID, 0, len(call.Args))
	for i := range call.Args {
		argTypes = append(argTypes, call.Args[i].Type)
	}
	resultType := types.NoTypeID
	if current != nil && call.HasDst {
		if idx := int(call.Dst.Local); idx >= 0 && idx < len(current.Locals) {
			resultType = current.Locals[idx].Type
		}
	}

	exact := e.collectFuncCandidates(name, true)
	if id, ok := e.pickFuncCandidate(exact, argTypes, resultType); ok {
		return id, true
	}
	base := stripGenericSuffix(name)
	if base != "" && base != name {
		return e.pickFuncCandidate(e.collectFuncCandidates(base, false), argTypes, resultType)
	}
	return mir.NoFuncID, false
}

func (e *Emitter) collectFuncCandidates(name string, exact bool) []mir.FuncID {
	if e == nil || e.mod == nil || name == "" {
		return nil
	}
	candidates := make([]mir.FuncID, 0, 4)
	for _, __id := range e.mod.SortedFuncIDs() {
		id := __id
		f := e.mod.Funcs[__id]
		if f == nil {
			continue
		}
		if exact {
			if f.Name == name {
				candidates = append(candidates, mir.FuncID(id))
			}
			continue
		}
		if stripGenericSuffix(f.Name) == name {
			candidates = append(candidates, mir.FuncID(id))
		}
	}
	return candidates
}

func (e *Emitter) pickFuncCandidate(candidates []mir.FuncID, argTypes []types.TypeID, resultType types.TypeID) (mir.FuncID, bool) {
	if len(candidates) == 0 {
		return mir.NoFuncID, false
	}

	steps := []func(mir.FuncID) bool{
		func(id mir.FuncID) bool { return e.funcSignatureMatches(id, argTypes, resultType, true) },
		func(id mir.FuncID) bool { return e.funcSignatureMatches(id, argTypes, types.NoTypeID, true) },
		func(id mir.FuncID) bool { return e.funcSignatureMatches(id, argTypes, resultType, false) },
		func(id mir.FuncID) bool { return e.funcSignatureMatches(id, argTypes, types.NoTypeID, false) },
	}
	for _, step := range steps {
		match := mir.NoFuncID
		for _, id := range candidates {
			if !step(id) {
				continue
			}
			if match != mir.NoFuncID {
				match = mir.NoFuncID
				break
			}
			match = id
		}
		if match != mir.NoFuncID {
			return match, true
		}
	}
	return mir.NoFuncID, false
}

func (e *Emitter) funcSignatureMatches(id mir.FuncID, argTypes []types.TypeID, resultType types.TypeID, strictArgs bool) bool {
	if e == nil || e.mod == nil || int(id) < 0 || int(id) >= len(e.mod.Funcs) {
		return false
	}
	fn := e.mod.Funcs[id]
	if fn == nil || fn.ParamCount != len(argTypes) {
		return false
	}
	if resultType != types.NoTypeID && normalizeCallType(e.types, fn.Result) != normalizeCallType(e.types, resultType) {
		return false
	}
	if !strictArgs {
		return true
	}
	sig, ok := e.funcSigs[id]
	if !ok {
		return false
	}
	for i, argType := range argTypes {
		if argType == types.NoTypeID || i >= len(sig.paramTypes) {
			continue
		}
		if normalizeCallType(e.types, sig.paramTypes[i]) != normalizeCallType(e.types, argType) {
			return false
		}
	}
	return true
}

func normalizeCallType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	return resolveAliasAndOwn(typesIn, id)
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

func (fe *funcEmitter) funcSigFromType(typeID types.TypeID) (funcSig, error) {
	if fe == nil || fe.emitter == nil || fe.emitter.types == nil {
		return funcSig{}, fmt.Errorf("missing type interner for function value")
	}
	typeID = resolveAliasAndOwn(fe.emitter.types, typeID)
	info, ok := fe.emitter.types.FnInfo(typeID)
	if !ok || info == nil {
		return funcSig{}, fmt.Errorf("missing function signature for type#%d", typeID)
	}
	params := make([]string, 0, len(info.Params))
	paramTypes := make([]types.TypeID, 0, len(info.Params))
	for _, p := range info.Params {
		llvmTy, err := fe.emitter.llvmValueType(p)
		if err != nil {
			return funcSig{}, err
		}
		params = append(params, llvmTy)
		paramTypes = append(paramTypes, p)
	}
	ret, err := fe.emitter.llvmType(info.Result)
	if err != nil {
		return funcSig{}, err
	}
	abi, err := fe.emitter.surgeABIForType(typeID)
	if err != nil {
		return funcSig{}, fmt.Errorf("call contract for function value: %w", err)
	}
	return funcSig{
		ret: ret, params: params, paramTypes: paramTypes, resultType: info.Result, abi: abi,
	}, nil
}

func (fe *funcEmitter) emitLayoutIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "size_of" && name != "align_of" {
		return false, nil
	}
	if !call.HasDst {
		return true, nil
	}
	if fe.emitter == nil || fe.emitter.mod == nil || fe.emitter.mod.Meta == nil {
		return true, fmt.Errorf("missing module metadata for %s", name)
	}
	if !call.Callee.Sym.IsValid() {
		return true, fmt.Errorf("%s requires type arguments", name)
	}
	typeArgs, ok := fe.emitter.mod.Meta.FuncTypeArgs[call.Callee.Sym]
	if !ok || len(typeArgs) != 1 || typeArgs[0] == types.NoTypeID {
		return true, fmt.Errorf("invalid type arguments for %s", name)
	}
	layoutInfo, err := fe.emitter.layoutOf(typeArgs[0])
	if err != nil {
		return true, err
	}
	n := layoutInfo.Size
	if name == "align_of" {
		n = layoutInfo.Align
	}
	dstType := types.NoTypeID
	if call.Dst.Kind == mir.PlaceLocal && int(call.Dst.Local) < len(fe.f.Locals) {
		dstType = fe.f.Locals[call.Dst.Local].Type
	}
	if err := fe.emitLenStore(call.Dst, dstType, fmt.Sprintf("%d", n)); err != nil {
		return true, err
	}
	return true, nil
}

func (fe *funcEmitter) emitCloneIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "__clone" {
		return false, nil
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("__clone requires 1 argument")
	}
	if !call.HasDst {
		return true, nil
	}
	argVal, _, srcType, err := fe.emitToSource(&call.Args[0])
	if err != nil {
		return true, err
	}
	if !isStringLike(fe.emitter.types, srcType) {
		if _, ok := fe.emitter.resolveFuncIDForCall(fe.f, call); ok {
			return false, nil
		}
		return true, fmt.Errorf("__clone unsupported for type")
	}
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return true, err
	}
	if !isStorageRun(dstTy) {
		dstTy = handleType
	}
	// Deep copy: __clone must yield an independent owned string. A bare
	// handle store aliases the buffer, so dropping either copy frees
	// storage the other still reads.
	cloned := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_string_clone(ptr %s)\n", cloned, argVal)
	fe.emitValueStore(dstTy, cloned, ptr, dstAlign)
	return true, nil
}
