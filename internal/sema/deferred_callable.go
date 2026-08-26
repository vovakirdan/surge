package sema

import (
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// DeferredUseID is a deterministic, canonical source-use identity carried
// unchanged from sema through HIR into mono.
type DeferredUseID string

// DeferredUseRef is the local AST lookup key used only while lowering the
// source file. The portable DeferredUseID is the value stored in HIR.
type DeferredUseRef struct {
	Expr ast.ExprID
	Kind DeferredCallableKind
}

// DeferredCallableOutcomeKind is the closed set of post-substitution call
// outcomes. Clone on a concrete Copy type is intentionally not a callable.
type DeferredCallableOutcomeKind uint8

// Outcomes a post-substitution deferred call can have.
const (
	DeferredCallableResolved DeferredCallableOutcomeKind = iota + 1
	DeferredCallableBuiltinCopy
)

// DeferredCallableRequest is a fully concrete call shape passed to the one
// post-merge semantic resolver.
type DeferredCallableRequest struct {
	Kind             DeferredCallableKind
	Receiver         types.TypeID
	Method           string
	Args             []types.TypeID
	ExplicitTypeArgs []types.TypeID
	ExpectedResult   types.TypeID
	StaticReceiver   bool
	AccessModule     string
	SourceKey        string
	// Site is the requesting source span. It carries no resolution meaning and
	// exists so a post-merge failure can be reported where the user wrote it.
	Site        source.Span
	Requirement DeferredCallableRequirement
}

// DeferredCallableResolution names either an exact callable and its
// declaration-ordered template arguments, or the builtin Copy operation.
type DeferredCallableResolution struct {
	Outcome      DeferredCallableOutcomeKind
	Callee       symbols.SymbolID
	CalleeKey    string
	TemplateArgs []types.TypeID
	ParamTypes   []types.TypeID
	ResultType   types.TypeID
}

// ResolvedDeferredCall is the concrete callable map consumed by mono. The key
// is the concrete caller instance plus canonical source site; mono never scans
// the symbol table to rediscover this decision.
type ResolvedDeferredCall struct {
	UseID              DeferredUseID
	Caller             InstanceKey
	CallerTemplate     symbols.SymbolID
	CallerTemplateArgs []types.TypeID
	Kind               DeferredCallableKind
	Outcome            DeferredCallableOutcomeKind
	Callee             symbols.SymbolID
	CalleeKey          string
	CalleeTemplateArgs []types.TypeID
	CalleeParamTypes   []types.TypeID
	CalleeResultType   types.TypeID
	Receiver           types.TypeID
	Args               []types.TypeID
	ExpectedResult     types.TypeID
	StaticReceiver     bool
	Site               source.Span
	SourceKey          string
	Reason             string
}

func (tc *typeChecker) rememberDeferredCallable(
	kind DeferredCallableKind,
	receiver types.TypeID,
	method string,
	args []types.TypeID,
	explicitTypeArgs []types.TypeID,
	expectedResult types.TypeID,
	staticReceiver bool,
	site source.Span,
	useExpr ast.ExprID,
	requirement *DeferredCallableRequirement,
) {
	if tc == nil || tc.result == nil || receiver == types.NoTypeID || method == "" {
		return
	}
	caller := tc.currentFnSym()
	if !tc.isGenericInstantiationCaller(caller) {
		return
	}
	sym := tc.symbolFromID(caller)
	if sym == nil {
		return
	}
	callerArity, err := safecast.Conv[uint32](len(sym.TypeParams))
	if err != nil {
		panic(fmt.Errorf("deferred callable caller arity overflow: %w", err))
	}
	reason := "deferred method call"
	switch kind {
	case DeferredBoolCall:
		reason = "deferred bool call"
	case DeferredCloneCall:
		reason = "deferred clone call"
	case DeferredCloneObligation:
		reason = "deferred clone obligation"
	}
	ordinal := uint32(0)
	edges := tc.result.InstantiationGraph.deferredCallables
	for i := range edges {
		if existing := &edges[i]; existing.Kind == kind && existing.Caller == caller && existing.Witness.Site == site {
			ordinal++
		}
	}
	useID := localDeferredUseID(kind, site, ordinal)
	tc.result.InstantiationGraph.recordDeferredCallable(&DeferredCallableEdge{
		UseID:               useID,
		UseOrdinal:          ordinal,
		Kind:                kind,
		Caller:              caller,
		CallerTemplateArity: callerArity,
		CallerBindings:      tc.instantiationCallerBindings(caller),
		Receiver:            receiver,
		Method:              method,
		Args:                slices.Clone(args),
		ExplicitTypeArgs:    slices.Clone(explicitTypeArgs),
		ExpectedResult:      expectedResult,
		StaticReceiver:      staticReceiver,
		AccessModule:        tc.modulePath,
		Requirement:         cloneDeferredCallableRequirement(requirement),
		Obligation:          tc.pendingCloneObligation,
		Witness: InstantiationWitness{
			Site:   site,
			Caller: caller,
			Reason: reason,
		},
	})
	if useExpr.IsValid() {
		if tc.result.DeferredCallableUses == nil {
			tc.result.DeferredCallableUses = make(map[DeferredUseRef]DeferredUseID)
		}
		tc.result.DeferredCallableUses[DeferredUseRef{Expr: useExpr, Kind: kind}] = useID
	}
}

func cloneResolvedDeferredCall(call *ResolvedDeferredCall) ResolvedDeferredCall {
	cloned := *call
	cloned.CallerTemplateArgs = slices.Clone(call.CallerTemplateArgs)
	cloned.CalleeTemplateArgs = slices.Clone(call.CalleeTemplateArgs)
	cloned.CalleeParamTypes = slices.Clone(call.CalleeParamTypes)
	cloned.Args = slices.Clone(call.Args)
	return cloned
}

// CloneResolvedDeferredCallForConsumer returns a detached copy for downstream
// immutable indexes.
func CloneResolvedDeferredCallForConsumer(call *ResolvedDeferredCall) ResolvedDeferredCall {
	if call == nil {
		return ResolvedDeferredCall{}
	}
	return cloneResolvedDeferredCall(call)
}
