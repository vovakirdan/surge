package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (fe *funcEmitter) emitCall(ins *mir.Instr) error {
	call := &ins.Call
	if call.Callee.Kind == mir.CalleeValue && call.Callee.Value.Type != types.NoTypeID {
		return fe.emitCallValue(call)
	}
	if handled, err := fe.emitTagConstructor(call); handled {
		return err
	}
	if handled, err := fe.emitLayoutIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitLenIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitIndexIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitCloneIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitToIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitFromStrIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitExitIntrinsic(call); handled {
		return err
	}
	if handled, err := fe.emitMagicIntrinsic(call); handled {
		return err
	}
	callee, sig, err := fe.resolveCallee(call)
	if err != nil {
		return err
	}
	args := make([]string, 0, len(call.Args))
	for i := range call.Args {
		val, ty, err := fe.emitOperand(&call.Args[i])
		if err != nil {
			return err
		}
		args = append(args, fmt.Sprintf("%s %s", ty, val))
	}
	callStmt := fmt.Sprintf("call %s @%s(%s)", sig.ret, callee, strings.Join(args, ", "))
	if call.HasDst {
		if sig.ret == "void" {
			return fmt.Errorf("call has destination but returns void: %s", callee)
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

func (fe *funcEmitter) resolveCallee(call *mir.CallInstr) (string, funcSig, error) {
	if call == nil {
		return "", funcSig{}, fmt.Errorf("nil call")
	}
	if call.Callee.Kind == mir.CalleeSym {
		if call.Callee.Sym.IsValid() {
			if fe.emitter.mod != nil {
				if id, ok := fe.emitter.mod.FuncBySym[call.Callee.Sym]; ok {
					name := fe.emitter.funcNames[id]
					sig := fe.emitter.funcSigs[id]
					return name, sig, nil
				}
			}
		}
		name := call.Callee.Name
		if name == "" {
			name = fe.symbolName(call.Callee.Sym)
		}
		if sig, ok := fe.emitter.runtimeSigs[name]; ok {
			return name, sig, nil
		}
		return "", funcSig{}, fmt.Errorf("unknown external function %q", name)
	}
	if call.Callee.Kind == mir.CalleeValue {
		if call.Callee.Value.Type != types.NoTypeID {
			return "", funcSig{}, fmt.Errorf("callee value requires direct call lowering")
		}
		name := stripGenericSuffix(call.Callee.Name)
		if name == "" {
			return "", funcSig{}, fmt.Errorf("missing callee name")
		}
		if id, ok := fe.emitter.funcByName(name); ok {
			return fe.emitter.funcNames[id], fe.emitter.funcSigs[id], nil
		}
		if sig, ok := fe.emitter.runtimeSigs[name]; ok {
			return name, sig, nil
		}
		return "", funcSig{}, fmt.Errorf("unknown external function %q", name)
	}
	return "", funcSig{}, fmt.Errorf("unsupported callee kind %v", call.Callee.Kind)
}

func (fe *funcEmitter) symbolName(symID symbols.SymbolID) string {
	if !symID.IsValid() || fe.emitter.syms == nil || fe.emitter.syms.Symbols == nil || fe.emitter.syms.Strings == nil {
		return ""
	}
	sym := fe.emitter.syms.Symbols.Get(symID)
	if sym == nil {
		return ""
	}
	return fe.emitter.syms.Strings.MustLookup(sym.Name)
}

func stripGenericSuffix(name string) string {
	if idx := strings.Index(name, "::<"); idx >= 0 {
		return name[:idx]
	}
	return name
}
