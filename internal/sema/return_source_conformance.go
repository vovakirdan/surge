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
	ReturnSourceConformanceSelected ReturnSourceConformanceKind = iota
	// Entailed is pending until the generic assumption has a concrete witness.
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
	for i, fact := range r.returnSourceConformances {
		out[i] = cloneReturnSourceConformance(fact)
	}
	return out
}

func cloneReturnSourceConformance(fact ReturnSourceConformance) ReturnSourceConformance {
	fact.Bound.GenericArgs = slices.Clone(fact.Bound.GenericArgs)
	fact.ConcreteBound.GenericArgs = slices.Clone(fact.ConcreteBound.GenericArgs)
	fact.Params = slices.Clone(fact.Params)
	fact.CallerBindings = slices.Clone(fact.CallerBindings)
	return fact
}

func (tc *typeChecker) returnSourceConformance(target types.TypeID, bound, original symbols.BoundInstance, use source.Span, req methodRequirement, actual methodSignature) ReturnSourceConformance {
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
	return cloneReturnSourceConformance(fact)
}

func (tc *typeChecker) retainReturnSourceEntailment(target types.TypeID, bound, original symbols.BoundInstance, use source.Span) {
	reqs, ok := tc.requirementsForBound(bound)
	if !ok {
		// Keep an unresolved assumption even when its member descriptor is
		// unavailable. Finalization must not mistake missing metadata for safety.
		fact := tc.returnSourceConformance(target, bound, original, use, methodRequirement{}, methodSignature{})
		fact.Kind = ReturnSourceConformanceEntailed
		tc.retainReturnSourceConformances([]ReturnSourceConformance{fact})
		return
	}
	var pending []ReturnSourceConformance
	for _, methods := range reqs.methods {
		for _, req := range methods {
			fact := tc.returnSourceConformance(target, bound, original, use, req, methodSignature{})
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
	for _, fact := range facts {
		if !slices.ContainsFunc(tc.result.returnSourceConformances, func(old ReturnSourceConformance) bool {
			return returnSourceConformancesEqual(old, fact)
		}) {
			tc.result.returnSourceConformances = append(tc.result.returnSourceConformances, cloneReturnSourceConformance(fact))
		}
	}
}

func returnSourceConformancesEqual(a, b ReturnSourceConformance) bool {
	return a.Kind == b.Kind && returnSourceRequirementsEqual(a.Requirement, b.Requirement) &&
		a.Actual.Symbol == b.Actual.Symbol && a.Actual.Declaration == b.Actual.Declaration && a.Actual.Sources.Equal(b.Actual.Sources) &&
		a.Bound.Contract == b.Bound.Contract && a.Bound.Span == b.Bound.Span && slices.Equal(a.Bound.GenericArgs, b.Bound.GenericArgs) &&
		a.ConcreteBound.Contract == b.ConcreteBound.Contract && a.ConcreteBound.Span == b.ConcreteBound.Span && slices.Equal(a.ConcreteBound.GenericArgs, b.ConcreteBound.GenericArgs) &&
		a.Target == b.Target && slices.Equal(a.Params, b.Params) && a.Result == b.Result && a.Use == b.Use && a.Caller == b.Caller &&
		slices.Equal(a.CallerBindings, b.CallerBindings) && a.GenericParamOwner == b.GenericParamOwner && a.GenericParamIndex == b.GenericParamIndex
}
