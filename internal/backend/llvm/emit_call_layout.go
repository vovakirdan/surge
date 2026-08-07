package llvm

import (
	"fmt"

	"surge/internal/mir"
	"surge/internal/types"
)

// The generated call contract, as this backend reaches it.
//
// The contract itself lives in `internal/mir` and is a pure function of a
// callee's TYPE, never of a function identity. That purity is the whole point:
// a direct call resolves its signature from the definition it names while a
// call through a value has only the callee's type in hand, and if those two
// could be keyed differently they could disagree about hidden result
// destinations and by-address arguments for one callee. Such a disagreement
// emits IR that verifies cleanly and then has the caller and the callee reading
// different argument positions.
//
// So this file is the only place the backend asks, and both the definition site
// and both call sites ask through it.

// surgeABIForSignature classifies a definition site, which carries its
// parameter and result types rather than an interned function type.
func (e *Emitter) surgeABIForSignature(params []types.TypeID, result types.TypeID) (mir.SurgeABI, error) {
	table, err := e.callLayouts()
	if err != nil {
		return mir.SurgeABI{}, err
	}
	layout, err := table.OfSignature(params, result, mir.ABIDomainSurge)
	if err != nil {
		return mir.SurgeABI{}, err
	}
	return layout.Surge()
}

// surgeABIForType classifies a callee named only by its function type, which is
// all a call through a function value has.
func (e *Emitter) surgeABIForType(fnType types.TypeID) (mir.SurgeABI, error) {
	table, err := e.callLayouts()
	if err != nil {
		return mir.SurgeABI{}, err
	}
	layout, err := table.Of(fnType, mir.ABIDomainSurge)
	if err != nil {
		return mir.SurgeABI{}, err
	}
	return layout.Surge()
}

// abiParam is one lowered argument position: how it is spelled in a signature,
// and whether it is there at all.
type abiParam struct {
	// spelling is the LLVM parameter type with its attributes, empty when the
	// argument occupies no position.
	spelling string
	// byval marks an argument the callee receives as its own copy of inline
	// storage, made once by this attribute rather than by the caller.
	byval bool
	// elided marks a zero-sized argument. It keeps its ordering and lifecycle
	// meaning in the source and occupies no argument position: there is no
	// payload byte to invent and no dereferenceable address to promise.
	elided bool
	align  uint64
}

// abiSignature is a callee's whole lowered contract: the LLVM return spelling,
// one entry per declared parameter in declaration order, and whether the result
// travels through a hidden caller-owned destination.
type abiSignature struct {
	ret    string
	params []abiParam
	sret   bool
	// retStorage is the result's storage spelling when sret is set, which the
	// attribute and the caller's destination must both state.
	retStorage string
	retAlign   uint64
}

// loweredSignature spells one signature under the generated call contract.
//
// The classification and the emitted spellings are read from the same frozen
// layouts, so they cannot disagree about a size or an alignment. They can still
// disagree about a CLASS — a parameter the contract classified from its
// declared type while the body allocates a borrow for it — and that
// disagreement is refused rather than resolved, because either answer would be
// an ABI one side does not implement.
func (e *Emitter) loweredSignature(sig *funcSig) (abiSignature, error) {
	out := abiSignature{ret: sig.ret, params: make([]abiParam, 0, len(sig.params))}
	if sig.governedElsewhere() {
		// A hand-written runtime declaration or a foreign C callee. It never
		// takes the hidden destination or the by-value contract, and its
		// parameters are already spelled the way that authority states them.
		for _, spelling := range sig.params {
			out.params = append(out.params, abiParam{spelling: spelling})
		}
		return out, nil
	}
	if isStorageRun(sig.ret) {
		facts, err := e.storageFactsOf(sig.resultType)
		if err != nil {
			return abiSignature{}, err
		}
		out.sret = facts.Size != 0
		out.retStorage = sig.ret
		out.retAlign = facts.Align
		out.ret = "void"
	}
	if err := e.checkResultClass(sig, &out); err != nil {
		return abiSignature{}, err
	}
	for i, spelling := range sig.params {
		param, err := e.loweredParam(sig, i, spelling)
		if err != nil {
			return abiSignature{}, err
		}
		out.params = append(out.params, param)
	}
	return out, nil
}

func (e *Emitter) checkResultClass(sig *funcSig, out *abiSignature) error {
	want := mir.RetDirect
	switch {
	case sig.ret == "void":
		want = mir.RetVoid
	case out.sret:
		want = mir.RetSret
	case isStorageRun(sig.ret):
		// A zero-sized result carries only lifecycle and ordering.
		want = mir.RetVoid
		out.ret = "void"
	}
	if sig.abi.Ret.Class != want {
		return fmt.Errorf(
			"result classified %s but lowers as %s", sig.abi.Ret.Class, want)
	}
	return nil
}

func (e *Emitter) loweredParam(sig *funcSig, index int, spelling string) (abiParam, error) {
	size, inline := storageRunSize(spelling)
	want := mir.ParamDirect
	out := abiParam{spelling: spelling}
	switch {
	case inline && size == 0:
		want = mir.ParamElidedZST
		out.elided = true
		out.spelling = ""
	case inline:
		want = mir.ParamByval
		facts, err := e.storageFactsOf(sig.paramTypes[index])
		if err != nil {
			return abiParam{}, err
		}
		out.byval = true
		out.align = facts.Align
		out.spelling = fmt.Sprintf("ptr byval(%s) align %d", spelling, facts.Align)
	}
	if index >= len(sig.abi.Params) || sig.abi.Params[index].Class != want {
		got := mir.ParamGoverned
		if index < len(sig.abi.Params) {
			got = sig.abi.Params[index].Class
		}
		return abiParam{}, fmt.Errorf(
			"parameter %d classified %s but lowers as %s", index, got, want)
	}
	return out, nil
}

// callLayouts is the finalized contract table, or an error.
//
// A missing table is refused rather than answered with an empty classification.
// An empty one would read as "no hidden destination, no by-address argument"
// for every signature, which is a complete and wrong answer to a question this
// backend cannot otherwise ask.
func (e *Emitter) callLayouts() (*mir.CallLayoutTable, error) {
	if e == nil || e.mod == nil || e.mod.Meta == nil || e.mod.Meta.CallLayouts == nil {
		return nil, fmt.Errorf("missing finalized call layout table")
	}
	return e.mod.Meta.CallLayouts, nil
}
