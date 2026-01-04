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
	for id, f := range e.mod.Funcs {
		if f == nil {
			continue
		}
		if f.Name == base {
			return mir.FuncID(id), true
		}
	}
	return mir.NoFuncID, false
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
	for _, p := range info.Params {
		llvmTy, err := llvmValueType(fe.emitter.types, p)
		if err != nil {
			return funcSig{}, err
		}
		params = append(params, llvmTy)
	}
	ret, err := llvmType(fe.emitter.types, info.Result)
	if err != nil {
		return funcSig{}, err
	}
	return funcSig{ret: ret, params: params}, nil
}

func (fe *funcEmitter) emitCallValue(call *mir.CallInstr) error {
	if call == nil {
		return nil
	}
	sig, err := fe.funcSigFromType(call.Callee.Value.Type)
	if err != nil {
		return err
	}
	calleeVal, calleeTy, err := fe.emitOperand(&call.Callee.Value)
	if err != nil {
		return err
	}
	if calleeTy != "ptr" {
		return fmt.Errorf("callee value must be ptr, got %s", calleeTy)
	}
	args := make([]string, 0, len(call.Args))
	for i := range call.Args {
		val, ty, err := fe.emitOperand(&call.Args[i])
		if err != nil {
			return err
		}
		args = append(args, fmt.Sprintf("%s %s", ty, val))
	}
	callStmt := fmt.Sprintf("call %s %s(%s)", sig.ret, calleeVal, strings.Join(args, ", "))
	if call.HasDst {
		if sig.ret == "void" {
			return fmt.Errorf("call has destination but returns void")
		}
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = %s\n", tmp, callStmt)
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return err
		}
		if dstTy != sig.ret {
			dstTy = sig.ret
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return nil
	}
	fmt.Fprintf(&fe.emitter.buf, "  %s\n", callStmt)
	return nil
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
	if n < 0 {
		n = 0
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
		return true, fmt.Errorf("__clone unsupported for type")
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != "ptr" {
		dstTy = "ptr"
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, argVal, ptr)
	return true, nil
}
