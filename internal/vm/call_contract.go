package vm

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
)

// callContract is how one callee's arguments and result travel.
//
// The interpreter asks the module's single call-layout authority the same
// question the native backend asks it, keyed by the callee's signature and the
// generated-call domain and by nothing else. Two backends that classified one
// callee differently would disagree about which side owns an argument and about
// who initializes the result, and each backend's own corpus run would still
// pass — the disagreement is only visible when the two are compared, which is
// why the classification has one source rather than one per backend.
type callContract struct {
	abi mir.SurgeABI
	// classified is false only for a module that carries no call-layout
	// authority at all. A finalized module always carries one, so this is the
	// state of a module assembled directly rather than compiled.
	classified bool
}

// resultDest names where one activation writes its result.
//
// The caller decides this before the callee runs rather than the callee
// reconstructing it afterwards from wherever the caller's instruction pointer
// happens to be. That ordering is what a hidden-destination result requires: a
// composite result is initialized in storage the CALLER owns, because the
// callee's own storage stops existing when its activation ends, and a result
// recovered after the fact would be recovered from storage that is gone.
type resultDest struct {
	Frame *Frame
	Local mir.LocalID
	Has   bool
}

// resultProtocol is the classified result of one activation together with the
// destination its caller provided.
type resultProtocol struct {
	Class mir.RetClass
	Dest  resultDest
}

// contractOf returns the call contract of one callee definition, remembering
// each answer.
//
// The query is by SIGNATURE, never by function identity: a direct call knows
// the definition it names while an indirect call has only a function value in
// hand, and two routes to one callee that could be keyed differently could
// disagree about hidden destinations and by-value arguments for the same
// callee.
func (vm *VM) contractOf(fn *mir.Func) (callContract, *VMError) {
	if fn == nil {
		return callContract{}, vm.eb.unimplemented("call contract for no function")
	}
	if cached, ok := vm.contracts[fn]; ok {
		return cached, nil
	}
	table := vm.callLayoutTable()
	if table == nil {
		return vm.rememberContract(fn, callContract{}), nil
	}
	params := make([]types.TypeID, 0, fn.ParamCount)
	for i := 0; i < fn.ParamCount && i < len(fn.Locals); i++ {
		params = append(params, fn.Locals[i].Type)
	}
	classification, err := table.OfSignature(params, fn.Result, mir.ABIDomainSurge)
	if err != nil {
		return callContract{}, vm.eb.makeError(PanicUnimplemented,
			fmt.Sprintf("%s has no call classification: %v", fn.Name, err))
	}
	abi, err := classification.Surge()
	if err != nil {
		return callContract{}, vm.eb.makeError(PanicUnimplemented,
			fmt.Sprintf("%s is generated and must be governed by the generated contract: %v", fn.Name, err))
	}
	return vm.rememberContract(fn, callContract{abi: abi, classified: true}), nil
}

func (vm *VM) rememberContract(fn *mir.Func, contract callContract) callContract {
	if vm.contracts == nil {
		vm.contracts = make(map[*mir.Func]callContract, 64)
	}
	vm.contracts[fn] = contract
	return contract
}

func (vm *VM) callLayoutTable() *mir.CallLayoutTable {
	if vm.M == nil || vm.M.Meta == nil {
		return nil
	}
	return vm.M.Meta.CallLayouts
}

// resultProtocolFor pairs the callee's classified result with the destination
// the caller is providing for it.
func (c callContract) resultProtocolFor(dest resultDest) resultProtocol {
	if !c.classified {
		return resultProtocol{Class: mir.RetGoverned, Dest: dest}
	}
	return resultProtocol{Class: c.abi.Ret.Class, Dest: dest}
}

// passArguments moves the evaluated arguments into the callee's parameter
// slots under the contract.
//
// Each class is named here rather than left implicit, because the class is what
// says who owns the argument once the call has happened. A direct argument is
// its own concrete value. A by-value composite is the callee's own — the
// operand that produced it either duplicated the caller's value or took it, so
// exactly one copy exists and the caller cannot reach it. A zero-sized argument
// keeps its lifecycle and ordering meaning and carries no payload: there are no
// bytes to share, so nothing about it can be observed through the caller.
func (vm *VM) passArguments(callee *Frame, contract callContract, args []Value) *VMError {
	if len(args) > len(callee.Locals) {
		return vm.eb.makeError(PanicUnimplemented,
			fmt.Sprintf("too many arguments: got %d, expected at most %d", len(args), len(callee.Locals)))
	}
	if contract.classified && len(args) != len(contract.abi.Params) {
		return vm.eb.makeError(PanicUnimplemented,
			fmt.Sprintf("%s takes %d classified arguments but the call site passes %d",
				callee.Func.Name, len(contract.abi.Params), len(args)))
	}
	for i, arg := range args {
		if contract.classified {
			if class := contract.abi.Params[i].Class; class == mir.ParamGoverned {
				return vm.eb.makeError(PanicUnimplemented,
					fmt.Sprintf("%s argument %d was never classified", callee.Func.Name, i))
			}
		}
		localID, err := safecast.Conv[mir.LocalID](i)
		if err != nil {
			return vm.eb.makeError(PanicUnimplemented, fmt.Sprintf("invalid argument index %d", i))
		}
		if vmErr := vm.writeLocal(callee, localID, arg); vmErr != nil {
			return vmErr
		}
	}
	return nil
}

// deliverResult writes a hidden-destination result into the caller's slot
// while the returning activation is still standing, and reports whether it did.
//
// Only a hidden destination is delivered here. A direct result is a value and
// travels back as one; a void result has no payload to place. Delivering the
// two kinds through one path would make the ordering of the composite case —
// initialize the caller's destination, then tear the callee down — look like an
// incidental detail of the return sequence rather than the contract it is.
func (vm *VM) deliverResult(protocol resultProtocol, result Value) (bool, *VMError) {
	if protocol.Class != mir.RetSret || !protocol.Dest.Has || protocol.Dest.Frame == nil {
		return false, nil
	}
	if vmErr := vm.writeLocal(protocol.Dest.Frame, protocol.Dest.Local, result); vmErr != nil {
		return false, vmErr
	}
	return true, nil
}
