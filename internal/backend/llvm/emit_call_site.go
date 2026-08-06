package llvm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
)

// callTarget names one callee together with the signature governing its call.
//
// A direct call spells its callee as a module symbol and reads the signature
// from the definition it names; a call through a function value spells it as an
// SSA operand and derives the signature from the callee's type. Everything
// after that — how arguments are marshalled, how a result reaches its
// destination — is one contract. Two emitters can disagree about that contract
// for the same callee, and such a disagreement emits IR that verifies cleanly
// and then passes arguments the callee never reads; one emitter cannot.
type callTarget struct {
	// callee is the LLVM operand naming the function: "@fn.7" for a direct
	// call, an SSA temporary for a call through a value.
	callee string
	sig    funcSig
	// describe names the callee in diagnostics, where an SSA temporary on its
	// own would say nothing a reader could act on.
	describe string
}

// emitCallSite lowers one call once its callee and signature are known.
func (fe *funcEmitter) emitCallSite(call *mir.CallInstr, target *callTarget) error {
	if call == nil {
		return nil
	}
	args := make([]string, 0, len(call.Args))
	for i := range call.Args {
		arg := call.Args[i]
		fe.patchNothingCallArg(&arg, &target.sig, i)
		val, ty, err := fe.emitOperand(&arg)
		if err != nil {
			return err
		}
		args = append(args, fmt.Sprintf("%s %s", ty, val))
	}
	callStmt := fmt.Sprintf("call %s %s(%s)", target.sig.ret, target.callee, strings.Join(args, ", "))
	if !call.HasDst {
		fmt.Fprintf(&fe.emitter.buf, "  %s\n", callStmt)
		return nil
	}
	if target.sig.ret == "void" {
		return fmt.Errorf("call has destination but returns void: %s", target.describe)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = %s\n", tmp, callStmt)
	ptr, dstTy, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return err
	}
	if dstTy != target.sig.ret {
		dstTy = target.sig.ret
	}
	if err := fe.emitStore(dstTy, tmp, ptr); err != nil {
		return err
	}
	return nil
}

// emitDirectCall lowers a call that names its callee.
func (fe *funcEmitter) emitDirectCall(call *mir.CallInstr) error {
	callee, sig, err := fe.resolveCallee(call)
	if err != nil {
		return err
	}
	return fe.emitCallSite(call, &callTarget{
		callee:   "@" + callee,
		sig:      sig,
		describe: callee,
	})
}

// emitValueCall lowers a call through a function value. The callee operand is
// emitted before the arguments, matching the order a direct call resolves in,
// so a call's operand evaluation does not depend on how its callee was spelled.
func (fe *funcEmitter) emitValueCall(call *mir.CallInstr) error {
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
	return fe.emitCallSite(call, &callTarget{
		callee:   calleeVal,
		sig:      sig,
		describe: "function value",
	})
}
