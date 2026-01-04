package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

func (fe *funcEmitter) emitCloneValueIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "clone" {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if !call.HasDst {
		return true, fmt.Errorf("clone requires a destination")
	}
	if len(call.Args) != 1 {
		return true, fmt.Errorf("clone requires 1 argument")
	}
	dstType, err := fe.placeBaseType(call.Dst)
	if err != nil {
		return true, err
	}
	if fe.emitter != nil && fe.emitter.types != nil && dstType != types.NoTypeID {
		if !fe.emitter.types.IsCopy(resolveAliasAndOwn(fe.emitter.types, dstType)) {
			return true, fmt.Errorf("clone requires a Copy type")
		}
	}
	val, valTy, err := fe.emitValueOperand(&call.Args[0])
	if err != nil {
		return true, err
	}
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return true, err
	}
	if dstTy != valTy {
		dstTy = valTy
	}
	fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, val, ptr)
	return true, nil
}
