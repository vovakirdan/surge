package sema

import (
	"fmt"
	"slices"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// InstantiationTemplateKind identifies the generic declaration represented by
// an always-on instantiation graph record.
type InstantiationTemplateKind uint8

const (
	// InstantiationFunction identifies a generic function template.
	InstantiationFunction InstantiationTemplateKind = iota + 1
	// InstantiationTag identifies a generic tag constructor template.
	InstantiationTag
)

func (k InstantiationTemplateKind) String() string {
	switch k {
	case InstantiationFunction:
		return "function"
	case InstantiationTag:
		return "tag"
	default:
		return fmt.Sprintf("instantiation-kind(%d)", k)
	}
}

// InstanceKey is the canonical post-merge identity of a concrete generic
// instance. It deliberately contains no allocation-order-dependent IDs.
type InstanceKey struct {
	TemplateKey string
	ArgsKey     string
}

// NewInstanceKey constructs the shared post-merge instance identity.
func NewInstanceKey(identity InstantiationIdentity, template symbols.SymbolID, args []types.TypeID) (InstanceKey, error) {
	if !template.IsValid() {
		return InstanceKey{}, fmt.Errorf("instantiation key: missing template symbol")
	}
	if len(args) == 0 {
		return InstanceKey{}, fmt.Errorf("instantiation key: symbol %d has no template arguments", template)
	}
	if identity.ResolveTemplate == nil {
		return InstanceKey{}, fmt.Errorf("instantiation key: missing canonical template resolver")
	}
	templateKey, err := identity.ResolveTemplate(template)
	if err != nil {
		return InstanceKey{}, fmt.Errorf("instantiation key for symbol %d: %w", template, err)
	}
	if templateKey == "" {
		return InstanceKey{}, fmt.Errorf("instantiation key for symbol %d: empty canonical template identity", template)
	}
	argsKey, err := identity.Types.TypeArgsKey(args)
	if err != nil {
		return InstanceKey{}, fmt.Errorf("instantiation key for symbol %d: %w", template, err)
	}
	return InstanceKey{TemplateKey: templateKey, ArgsKey: argsKey}, nil
}

// InstantiationStep is one concrete caller-to-callee hop in a closure witness.
type InstantiationStep struct {
	Caller    InstanceKey
	Callee    InstanceKey
	Site      source.Span
	SourceKey string
	Reason    string
}

// InstantiationWitness preserves one deterministic source path from a root to
// a concrete instance. Duplicate sites select the stable minimum witness.
type InstantiationWitness struct {
	Root      InstanceKey
	Site      source.Span
	SourceKey string
	Caller    symbols.SymbolID
	CallerKey string
	Reason    string
	Steps     []InstantiationStep
}

// InstantiationRoot is a concrete request made outside a generic template.
type InstantiationRoot struct {
	Kind         InstantiationTemplateKind
	Template     symbols.SymbolID
	TemplateArgs []types.TypeID
	Witness      InstantiationWitness
}

// InstantiationParamBinding maps one type parameter in a generic caller's
// declaration vocabulary to its exact position in that caller's instance
// argument vector. Receiver and method parameters may have different owners
// and overlapping parameter indices, so an owner set is insufficient.
type InstantiationParamBinding struct {
	Param      types.TypeID
	Owner      symbols.SymbolID
	ParamIndex uint32
	ArgIndex   uint32
}

// InstantiationEdge records a request inside a generic caller in the caller's
// template-argument vocabulary. CallerBindings is the complete substitution
// environment for that caller instance.
type InstantiationEdge struct {
	Kind                InstantiationTemplateKind
	Caller              symbols.SymbolID
	CallerTemplateArity uint32
	CallerBindings      []InstantiationParamBinding
	Callee              symbols.SymbolID
	CalleeTemplateArgs  []types.TypeID
	Witness             InstantiationWitness
}

// DeferredCallableKind identifies a call whose implementation cannot be
// selected until a generic caller has concrete type arguments.
type DeferredCallableKind uint8

// Call shapes whose implementation is chosen only after substitution.
const (
	DeferredMethodCall DeferredCallableKind = iota + 1
	DeferredBoolCall
	DeferredCloneCall
)

// DeferredCallableEdge keeps the complete sema-approved call shape in the
// caller template's type vocabulary. Resolution happens once Receiver, Args,
// ExplicitTypeArgs, and ExpectedResult have all been concretely substituted.
type DeferredCallableEdge struct {
	UseID               DeferredUseID
	UseOrdinal          uint32
	Kind                DeferredCallableKind
	Caller              symbols.SymbolID
	CallerTemplateArity uint32
	CallerBindings      []InstantiationParamBinding
	Receiver            types.TypeID
	Method              string
	Args                []types.TypeID
	ExplicitTypeArgs    []types.TypeID
	ExpectedResult      types.TypeID
	StaticReceiver      bool
	AccessModule        string
	Requirement         DeferredCallableRequirement
	Witness             InstantiationWitness
}

// InstantiationGraph is the mandatory sema artefact. It is populated whether
// or not the optional InstantiationRecorder is installed.
type InstantiationGraph struct {
	roots             []InstantiationRoot
	edges             []InstantiationEdge
	deferredCallables []DeferredCallableEdge
}

// IsEmpty reports whether no generic callable roots or edges were recorded.
func (g *InstantiationGraph) IsEmpty() bool {
	return g == nil || (len(g.roots) == 0 && len(g.edges) == 0 && len(g.deferredCallables) == 0)
}

// Roots returns a detached snapshot of recorded roots.
func (g *InstantiationGraph) Roots() []InstantiationRoot {
	if g == nil || len(g.roots) == 0 {
		return nil
	}
	out := make([]InstantiationRoot, len(g.roots))
	for i := range g.roots {
		out[i] = cloneInstantiationRoot(&g.roots[i])
	}
	return out
}

// Edges returns a detached snapshot of recorded template edges.
func (g *InstantiationGraph) Edges() []InstantiationEdge {
	if g == nil || len(g.edges) == 0 {
		return nil
	}
	out := make([]InstantiationEdge, len(g.edges))
	for i := range g.edges {
		out[i] = cloneInstantiationEdge(&g.edges[i])
	}
	return out
}

// DeferredCallables returns a detached snapshot of sema-approved calls whose
// exact implementation requires concrete caller arguments.
func (g *InstantiationGraph) DeferredCallables() []DeferredCallableEdge {
	if g == nil || len(g.deferredCallables) == 0 {
		return nil
	}
	out := make([]DeferredCallableEdge, len(g.deferredCallables))
	for i := range g.deferredCallables {
		out[i] = cloneDeferredCallableEdge(&g.deferredCallables[i])
	}
	return out
}

func (g *InstantiationGraph) recordRoot(root *InstantiationRoot) {
	if g == nil || root == nil || !root.Template.IsValid() || len(root.TemplateArgs) == 0 {
		return
	}
	cloned := cloneInstantiationRoot(root)
	for i := range g.roots {
		existing := &g.roots[i]
		if existing.Kind != cloned.Kind || existing.Template != cloned.Template || !slices.Equal(existing.TemplateArgs, cloned.TemplateArgs) {
			continue
		}
		if compareInstantiationWitness(&cloned.Witness, &existing.Witness) == 0 {
			return
		}
	}
	g.roots = append(g.roots, cloned)
}

func (g *InstantiationGraph) recordEdge(edge *InstantiationEdge) {
	if g == nil || edge == nil || !edge.Caller.IsValid() || !edge.Callee.IsValid() || len(edge.CalleeTemplateArgs) == 0 {
		return
	}
	cloned := cloneInstantiationEdge(edge)
	for i := range g.edges {
		existing := &g.edges[i]
		if existing.Kind != cloned.Kind || existing.Caller != cloned.Caller || existing.Callee != cloned.Callee ||
			existing.CallerTemplateArity != cloned.CallerTemplateArity ||
			!slices.Equal(existing.CallerBindings, cloned.CallerBindings) ||
			!slices.Equal(existing.CalleeTemplateArgs, cloned.CalleeTemplateArgs) {
			continue
		}
		if compareInstantiationWitness(&cloned.Witness, &existing.Witness) == 0 {
			return
		}
	}
	g.edges = append(g.edges, cloned)
}

func (g *InstantiationGraph) recordDeferredCallable(edge *DeferredCallableEdge) {
	if g == nil || edge == nil || edge.UseID == "" || !edge.Caller.IsValid() || edge.Receiver == types.NoTypeID || edge.Method == "" {
		return
	}
	cloned := cloneDeferredCallableEdge(edge)
	for i := range g.deferredCallables {
		existing := &g.deferredCallables[i]
		if existing.UseID != cloned.UseID || existing.Kind != cloned.Kind || existing.Caller != cloned.Caller ||
			existing.CallerTemplateArity != cloned.CallerTemplateArity ||
			!slices.Equal(existing.CallerBindings, cloned.CallerBindings) ||
			existing.Receiver != cloned.Receiver || existing.Method != cloned.Method ||
			!slices.Equal(existing.Args, cloned.Args) ||
			!slices.Equal(existing.ExplicitTypeArgs, cloned.ExplicitTypeArgs) ||
			existing.ExpectedResult != cloned.ExpectedResult ||
			existing.StaticReceiver != cloned.StaticReceiver || existing.AccessModule != cloned.AccessModule ||
			!deferredRequirementsEqual(&existing.Requirement, &cloned.Requirement) {
			continue
		}
		if compareInstantiationWitness(&cloned.Witness, &existing.Witness) == 0 {
			return
		}
	}
	g.deferredCallables = append(g.deferredCallables, cloned)
}
