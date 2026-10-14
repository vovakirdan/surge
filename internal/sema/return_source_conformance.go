package sema

import (
	"slices"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ReturnSourceConformanceKind describes evidence, never an origin verdict.
type ReturnSourceConformanceKind uint8

const (
	// ReturnSourceConformanceSelected records an exact selected declaration.
	ReturnSourceConformanceSelected ReturnSourceConformanceKind = iota
	// ReturnSourceConformanceEntailed is pending until a generic assumption has a concrete witness.
	ReturnSourceConformanceEntailed
)

// ReturnSourceConformanceActual identifies the existing matcher's selected
// declaration. Symbol is authoritative only in this unit; Declaration belongs
// to the original source file, including for imported methods with no local ID.
// Missing identity remains unresolved, never a name/shape lookup or a safe fact.
type ReturnSourceConformanceActual struct {
	Symbol      symbols.SymbolID
	Declaration source.Span
	Sources     types.ReturnSources
}

// ReturnSourceConformance retains a successful ordinary contract match or a
// pending generic assumption. Requirement identifies the original member;
// Bound is the original use and ConcreteBound retains its current substitution.
// Params includes physical self exactly once. Requirement.Sources keeps original
// member slots, mapped only by Requirement.ReceiverPrefix.
type ReturnSourceConformance struct {
	Kind              ReturnSourceConformanceKind
	Requirement       ReturnSourceRequirement
	Actual            ReturnSourceConformanceActual
	Bound             symbols.BoundInstance
	ConcreteBound     symbols.BoundInstance
	Target            types.TypeID
	Params            []types.TypeID
	Result            types.TypeID
	Use               source.Span
	Caller            symbols.SymbolID
	CallerBindings    []InstantiationParamBinding
	GenericParamOwner symbols.SymbolID
	GenericParamIndex uint32
}

type contractMethodMatch struct {
	requirement methodRequirement
	actual      methodSignature
}

// ReturnSourceConformances returns detached facts from their owning unit. They
// must not be merged into another unit or interpreted as semantic acceptance.
func (r *Result) ReturnSourceConformances() []ReturnSourceConformance {
	if r == nil {
		return nil
	}
	out := make([]ReturnSourceConformance, len(r.returnSourceConformances))
	for i := range r.returnSourceConformances {
		out[i] = cloneReturnSourceConformance(&r.returnSourceConformances[i])
	}
	return out
}

func cloneReturnSourceConformance(fact *ReturnSourceConformance) ReturnSourceConformance {
	if fact == nil {
		return ReturnSourceConformance{}
	}
	out := *fact
	out.Bound.GenericArgs = slices.Clone(fact.Bound.GenericArgs)
	out.ConcreteBound.GenericArgs = slices.Clone(fact.ConcreteBound.GenericArgs)
	out.Params = slices.Clone(fact.Params)
	out.CallerBindings = slices.Clone(fact.CallerBindings)
	return out
}

func (tc *typeChecker) returnSourceConformance(target types.TypeID, bound, original symbols.BoundInstance, use source.Span, req *methodRequirement, actual *methodSignature) ReturnSourceConformance {
	if req == nil {
		req = &methodRequirement{}
	}
	if actual == nil {
		actual = &methodSignature{}
	}
	fact := ReturnSourceConformance{
		Kind: ReturnSourceConformanceSelected,
		Requirement: ReturnSourceRequirement{Contract: req.returnSourceOwner, Member: req.span,
			Sources: req.returnSources, ReceiverPrefix: req.receiverPrefix},
		Actual: actual.origin, Bound: original, ConcreteBound: bound, Target: target,
		Params: req.params, Result: req.result, Use: use, Caller: tc.currentFnSym(),
	}
	fact.CallerBindings = tc.instantiationCallerBindings(fact.Caller)
	// Snapshot before shared type interner owner remapping. This also retains a
	// generic type owner when the use has no enclosing function.
	if info, ok := tc.types.TypeParamInfo(tc.resolveAlias(target)); ok && info != nil {
		fact.GenericParamOwner = symbols.SymbolID(info.Owner)
		fact.GenericParamIndex = info.Index
	}
	return cloneReturnSourceConformance(&fact)
}

func (tc *typeChecker) retainReturnSourceEntailment(target types.TypeID, bound, original symbols.BoundInstance, use source.Span) {
	reqs, ok := tc.requirementsForBound(bound)
	if !ok {
		// Keep an unresolved assumption even when its member descriptor is
		// unavailable. Finalization must not mistake missing metadata for safety.
		fact := tc.returnSourceConformance(target, bound, original, use, nil, nil)
		fact.Kind = ReturnSourceConformanceEntailed
		tc.retainReturnSourceConformances([]ReturnSourceConformance{fact})
		return
	}
	var pending []ReturnSourceConformance
	for _, methods := range reqs.methods {
		for i := range methods {
			fact := tc.returnSourceConformance(target, bound, original, use, &methods[i], nil)
			fact.Kind = ReturnSourceConformanceEntailed
			pending = append(pending, fact)
		}
	}
	tc.retainReturnSourceConformances(pending)
}

func (tc *typeChecker) retainReturnSourceConformances(facts []ReturnSourceConformance) {
	if tc.result == nil {
		return
	}
	for i := range facts {
		fact := &facts[i]
		found := false
		for j := range tc.result.returnSourceConformances {
			if returnSourceConformancesEqual(&tc.result.returnSourceConformances[j], fact) {
				found = true
				break
			}
		}
		if !found {
			tc.result.returnSourceConformances = append(tc.result.returnSourceConformances, cloneReturnSourceConformance(fact))
		}
	}
}

func returnSourceConformancesEqual(a, b *ReturnSourceConformance) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Kind == b.Kind && returnSourceRequirementsEqual(a.Requirement, b.Requirement) &&
		a.Actual.Symbol == b.Actual.Symbol && a.Actual.Declaration == b.Actual.Declaration && a.Actual.Sources.Equal(b.Actual.Sources) &&
		a.Bound.Contract == b.Bound.Contract && a.Bound.Span == b.Bound.Span && slices.Equal(a.Bound.GenericArgs, b.Bound.GenericArgs) &&
		a.ConcreteBound.Contract == b.ConcreteBound.Contract && a.ConcreteBound.Span == b.ConcreteBound.Span && slices.Equal(a.ConcreteBound.GenericArgs, b.ConcreteBound.GenericArgs) &&
		a.Target == b.Target && slices.Equal(a.Params, b.Params) && a.Result == b.Result && a.Use == b.Use && a.Caller == b.Caller &&
		slices.Equal(a.CallerBindings, b.CallerBindings) && a.GenericParamOwner == b.GenericParamOwner && a.GenericParamIndex == b.GenericParamIndex
}
