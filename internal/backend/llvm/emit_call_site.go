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
//
// A composite result is written straight into the caller's destination: the
// hidden `sret` argument IS that place, so the callee initializes the storage
// the assignment names and nothing is copied afterwards. When the call has no
// destination the caller still owns the storage — a result the source discards
// is still a result the callee must be able to write.
func (fe *funcEmitter) emitCallSite(call *mir.CallInstr, target *callTarget) error {
	if call == nil {
		return nil
	}
	lowered, err := fe.emitter.loweredSignature(&target.sig)
	if err != nil {
		return fmt.Errorf("call contract for %s: %w", target.describe, err)
	}
	var sretPtr string
	if lowered.sret {
		sretPtr, err = fe.callResultDestination(call, &target.sig)
		if err != nil {
			return err
		}
	}
	args := make([]string, 0, len(call.Args)+1)
	if lowered.sret {
		args = append(args, fmt.Sprintf(
			"ptr sret(%s) align %d %s", lowered.retStorage, lowered.retAlign, sretPtr))
	}
	for i := range call.Args {
		arg := call.Args[i]
		fe.patchNothingCallArg(&arg, &target.sig, i)
		val, ty, argErr := fe.emitOperand(&arg)
		if argErr != nil {
			return argErr
		}
		if i < len(lowered.params) {
			param := lowered.params[i]
			if param.elided {
				// Emitted for its effects and its ordering, then dropped: a
				// zero-sized argument has no position to occupy.
				continue
			}
			if param.byval {
				// The attribute spelling is the same one the definition
				// declares, so the copy the callee receives is described
				// identically at both ends of the call.
				args = append(args, fmt.Sprintf("%s %s", param.spelling, val))
				continue
			}
		}
		args = append(args, fmt.Sprintf("%s %s", ty, val))
	}
	callStmt := fmt.Sprintf("call %s %s(%s)", lowered.ret, target.callee, strings.Join(args, ", "))
	if lowered.sret || !call.HasDst {
		fmt.Fprintf(&fe.emitter.buf, "  %s\n", callStmt)
		return nil
	}
	if lowered.ret == "void" {
		if isStorageRun(target.sig.ret) {
			// A zero-sized result: the call carried lifecycle and ordering and
			// no bytes, and the destination is already the whole of the value.
			return nil
		}
		return fmt.Errorf("call has destination but returns void: %s", target.describe)
	}
	tmp := fe.nextTemp()
	fmt.Fprintf(&fe.emitter.buf, "  %s = %s\n", tmp, callStmt)
	fe.emitRuntimeAnswerTest(call, target.callee, tmp)
	ptr, dstTy, dstAlign, err := fe.emitPlaceStorage(call.Dst)
	if err != nil {
		return err
	}
	if isStorageRun(dstTy) {
		// The destination is a run of bytes and the callee answered with a
		// word, so the word is the ADDRESS of the value rather than the value.
		// That is how a runtime entry point returns a composite: it allocates
		// a block, fills it in and hands the block over. The value is adopted
		// into the storage the destination already has, and the block it
		// arrived in is released.
		//
		// Storing the word itself is what this did before, and it type-checked
		// in LLVM because both a pointer and a byte run are addressed the same
		// way: the pointer landed in the first eight bytes of the destination
		// and the rest of the value was never written. Every field then read
		// back either half an address or whatever the slot happened to hold.
		//
		// The type comes from the destination rather than from the signature: a
		// runtime entry point is declared in machine types alone and carries no
		// language result type to ask.
		dstType, dstErr := fe.placeBaseType(call.Dst)
		if dstErr != nil {
			return dstErr
		}
		return fe.emitAdoptFromTransportAllocation(tmp, ptr, dstType)
	}
	if dstTy != lowered.ret {
		dstTy = lowered.ret
	}
	fe.emitValueStore(dstTy, tmp, ptr, dstAlign)
	return nil
}

// callResultDestination is the storage a composite result is written into: the
// assignment's own place when there is one, and otherwise a fresh slot that
// exists only so the callee has somewhere legal to write.
func (fe *funcEmitter) callResultDestination(call *mir.CallInstr, sig *funcSig) (string, error) {
	if !call.HasDst {
		return fe.emitStorageAlloca(sig.resultType)
	}
	ptr, _, err := fe.emitPlacePtr(call.Dst)
	if err != nil {
		return "", err
	}
	return ptr, nil
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
