package llvm

import (
	"fmt"

	"surge/internal/mir"
)

func (fe *funcEmitter) emitReadlineIntrinsic(call *mir.CallInstr) (bool, error) {
	if call == nil || call.Callee.Kind != mir.CalleeSym {
		return false, nil
	}
	name := call.Callee.Name
	if name == "" {
		name = fe.symbolName(call.Callee.Sym)
	}
	name = stripGenericSuffix(name)
	if name != "readline" {
		return false, nil
	}
	if call.Callee.Sym.IsValid() && fe.emitter != nil && fe.emitter.mod != nil {
		if _, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
			return false, nil
		}
	}
	if len(call.Args) != 0 {
		return true, fmt.Errorf("readline expects 0 arguments")
	}
	if call.HasDst {
		tmp := fe.nextTemp()
		fmt.Fprintf(&fe.emitter.buf, "  %s = call ptr @rt_readline()\n", tmp)
		ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
		if err != nil {
			return true, err
		}
		if dstTy != "ptr" {
			return true, fmt.Errorf("%s expects destination ptr, got %s for %v", name, dstTy, call.Dst)
		}
		fmt.Fprintf(&fe.emitter.buf, "  store %s %s, ptr %s\n", dstTy, tmp, ptr)
		return true, nil
	}
	fmt.Fprintf(&fe.emitter.buf, "  call ptr @rt_readline()\n")
	return true, nil
}
