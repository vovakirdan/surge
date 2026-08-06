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
