package sema

import (
	"fmt"
	"slices"
	"sort"
	"strings"

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

// CanonicalizeInstantiationGraphSources stamps source-path identities before
// file/module graphs are merged. This keeps witness selection independent of
// FileID registration order.
func CanonicalizeInstantiationGraphSources(result *Result, resolve func(source.FileID) (string, error)) error {
	if result == nil {
		return fmt.Errorf("instantiation graph sources: missing semantic result")
	}
	if resolve == nil {
		return fmt.Errorf("instantiation graph sources: missing source resolver")
	}
	var rebuilt InstantiationGraph
	roots := result.InstantiationGraph.Roots()
	for i := range roots {
		root := &roots[i]
		key, err := resolve(root.Witness.Site.File)
		if err != nil {
			return fmt.Errorf("instantiation root %d source: %w", root.Template, err)
		}
		if key == "" {
			return fmt.Errorf("instantiation root %d source: empty canonical identity", root.Template)
		}
		root.Witness.SourceKey = key
		rebuilt.recordRoot(root)
	}
	edges := result.InstantiationGraph.Edges()
	for i := range edges {
		edge := &edges[i]
		key, err := resolve(edge.Witness.Site.File)
		if err != nil {
			return fmt.Errorf("instantiation edge %d -> %d source: %w", edge.Caller, edge.Callee, err)
		}
		if key == "" {
			return fmt.Errorf("instantiation edge %d -> %d source: empty canonical identity", edge.Caller, edge.Callee)
		}
		edge.Witness.SourceKey = key
		rebuilt.recordEdge(edge)
	}
	deferred := result.InstantiationGraph.DeferredCallables()
	useIDRemap := make(map[DeferredUseID]DeferredUseID, len(deferred))
	for i := range deferred {
		edge := &deferred[i]
		key, err := resolve(edge.Witness.Site.File)
		if err != nil {
			return fmt.Errorf("deferred callable edge %d %s source: %w", edge.Caller, edge.Method, err)
		}
		if key == "" {
			return fmt.Errorf("deferred callable edge %d %s source: empty canonical identity", edge.Caller, edge.Method)
		}
		edge.Witness.SourceKey = key
		canonicalUseID := canonicalDeferredUseID(key, edge.Kind, edge.Witness.Site, edge.UseOrdinal)
		useIDRemap[edge.UseID] = canonicalUseID
		edge.UseID = canonicalUseID
		rebuilt.recordDeferredCallable(edge)
	}
	for ref, id := range result.DeferredCallableUses {
		if canonical, ok := useIDRemap[id]; ok {
			result.DeferredCallableUses[ref] = canonical
		}
	}
	for i := range result.CallableCandidates {
		candidate := &result.CallableCandidates[i]
		if candidate.Builtin {
			candidate.SourceKey = "builtin"
		} else {
			key, err := resolve(candidate.Source.File)
			if err != nil {
				return fmt.Errorf("callable candidate %s source: %w", candidate.Name, err)
			}
			if key == "" {
				return fmt.Errorf("callable candidate %s source: empty canonical identity", candidate.Name)
			}
			candidate.SourceKey = key
		}
		candidate.BodyKey = canonicalCallableBodyKey(candidate)
	}
	sort.SliceStable(result.CallableCandidates, func(i, j int) bool {
		return result.CallableCandidates[i].BodyKey < result.CallableCandidates[j].BodyKey
	})
	if err := canonicalizeFinalizationRequestSources(result, resolve); err != nil {
		return err
	}
	result.InstantiationGraph = rebuilt
	result.InstantiationClosure = nil
	result.rebuildFunctionInstantiations()
	return nil
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

// CopyInstantiationAuthority shares one finalized module-level authority with
// another per-file semantic result while preserving that result's expression
// and borrow analysis maps.
func CopyInstantiationAuthority(dst, src *Result) {
	if dst == nil || src == nil {
		return
	}
	var graph InstantiationGraph
	roots := src.InstantiationGraph.Roots()
	for i := range roots {
		graph.recordRoot(&roots[i])
	}
	edges := src.InstantiationGraph.Edges()
	for i := range edges {
		graph.recordEdge(&edges[i])
	}
	deferred := src.InstantiationGraph.DeferredCallables()
	for i := range deferred {
		graph.recordDeferredCallable(&deferred[i])
	}
	dst.CallableCandidates = make([]CallableCandidate, len(src.CallableCandidates))
	for i := range src.CallableCandidates {
		dst.CallableCandidates[i] = cloneCallableCandidate(&src.CallableCandidates[i])
	}
	copyFinalizationRequests(dst, src)
	dst.InstantiationGraph = graph
	dst.InstantiationIdentity = src.InstantiationIdentity
	dst.InstantiationClosure = nil
	if src.InstantiationClosure != nil {
		closure := InstantiationClosure{
			LiveCallables:         slices.Clone(src.InstantiationClosure.LiveCallables),
			Instances:             make([]InstantiationInstance, len(src.InstantiationClosure.Instances)),
			UseSites:              make([]ConcreteInstantiationUse, len(src.InstantiationClosure.UseSites)),
			ResolvedDeferredCalls: make([]ResolvedDeferredCall, len(src.InstantiationClosure.ResolvedDeferredCalls)),
		}
		for i := range src.InstantiationClosure.Instances {
			closure.Instances[i] = cloneInstantiationInstance(&src.InstantiationClosure.Instances[i])
		}
		for i := range src.InstantiationClosure.UseSites {
			use := src.InstantiationClosure.UseSites[i]
			use.CallerTemplateArgs = slices.Clone(use.CallerTemplateArgs)
			use.TemplateArgs = slices.Clone(use.TemplateArgs)
			closure.UseSites[i] = use
		}
		for i := range src.InstantiationClosure.ResolvedDeferredCalls {
			closure.ResolvedDeferredCalls[i] = cloneResolvedDeferredCall(&src.InstantiationClosure.ResolvedDeferredCalls[i])
		}
		dst.InstantiationClosure = &closure
	}
	dst.FunctionCallEdges = cloneFunctionCallEdges(src.FunctionCallEdges)
	dst.InstantiationCallableSeeds = cloneCallableSet(src.InstantiationCallableSeeds)
	dst.InstantiationTemplateParams = cloneTemplateParams(src.InstantiationTemplateParams)
	dst.rebuildFunctionInstantiations()
}

func cloneTemplateParams(src map[symbols.SymbolID][]types.TypeID) map[symbols.SymbolID][]types.TypeID {
	if src == nil {
		return nil
	}
	out := make(map[symbols.SymbolID][]types.TypeID, len(src))
	for template, params := range src {
		out[template] = slices.Clone(params)
	}
	return out
}

func cloneFunctionCallEdges(src map[symbols.SymbolID]map[symbols.SymbolID]struct{}) map[symbols.SymbolID]map[symbols.SymbolID]struct{} {
	if src == nil {
		return nil
	}
	out := make(map[symbols.SymbolID]map[symbols.SymbolID]struct{}, len(src))
	for caller, callees := range src {
		out[caller] = cloneCallableSet(callees)
	}
	return out
}

func cloneCallableSet(src map[symbols.SymbolID]struct{}) map[symbols.SymbolID]struct{} {
	if src == nil {
		return nil
	}
	out := make(map[symbols.SymbolID]struct{}, len(src))
	for id := range src {
		out[id] = struct{}{}
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
			!deferredRequirementsEqual(existing.Requirement, cloned.Requirement) {
			continue
		}
		if compareInstantiationWitness(&cloned.Witness, &existing.Witness) == 0 {
			return
		}
	}
	g.deferredCallables = append(g.deferredCallables, cloned)
}

// MergeInstantiationGraphs merges one file/module graph into the canonical
// result and remaps every symbol-bearing field. Type-param metadata itself is
// remapped by the driver on the shared interner before closure finalization.
func MergeInstantiationGraphs(dst, src *Result, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if dst == nil || src == nil {
		return
	}
	roots := src.InstantiationGraph.Roots()
	for i := range roots {
		root := &roots[i]
		root.Template = remapInstantiationSymbol(root.Template, mapping)
		root.Witness.Caller = remapInstantiationSymbol(root.Witness.Caller, mapping)
		dst.InstantiationGraph.recordRoot(root)
	}
	edges := src.InstantiationGraph.Edges()
	for i := range edges {
		edge := &edges[i]
		edge.Caller = remapInstantiationSymbol(edge.Caller, mapping)
		edge.Callee = remapInstantiationSymbol(edge.Callee, mapping)
		edge.Witness.Caller = remapInstantiationSymbol(edge.Witness.Caller, mapping)
		for i := range edge.CallerBindings {
			edge.CallerBindings[i].Owner = remapInstantiationSymbol(edge.CallerBindings[i].Owner, mapping)
		}
		sort.Slice(edge.CallerBindings, func(i, j int) bool {
			return compareInstantiationBinding(edge.CallerBindings[i], edge.CallerBindings[j]) < 0
		})
		edge.CallerBindings = slices.Compact(edge.CallerBindings)
		dst.InstantiationGraph.recordEdge(edge)
	}
	deferred := src.InstantiationGraph.DeferredCallables()
	for i := range deferred {
		edge := &deferred[i]
		edge.Caller = remapInstantiationSymbol(edge.Caller, mapping)
		edge.Witness.Caller = remapInstantiationSymbol(edge.Witness.Caller, mapping)
		for contractIndex := range edge.Requirement.Contracts {
			edge.Requirement.Contracts[contractIndex] = remapInstantiationSymbol(edge.Requirement.Contracts[contractIndex], mapping)
		}
		sort.Slice(edge.Requirement.Contracts, func(i, j int) bool {
			return edge.Requirement.Contracts[i] < edge.Requirement.Contracts[j]
		})
		edge.Requirement.Contracts = slices.Compact(edge.Requirement.Contracts)
		for i := range edge.CallerBindings {
			edge.CallerBindings[i].Owner = remapInstantiationSymbol(edge.CallerBindings[i].Owner, mapping)
		}
		sort.Slice(edge.CallerBindings, func(i, j int) bool {
			return compareInstantiationBinding(edge.CallerBindings[i], edge.CallerBindings[j]) < 0
		})
		edge.CallerBindings = slices.Compact(edge.CallerBindings)
		dst.InstantiationGraph.recordDeferredCallable(edge)
	}
	for i := range src.CallableCandidates {
		candidate := cloneCallableCandidate(&src.CallableCandidates[i])
		candidate.Symbol = remapInstantiationSymbol(candidate.Symbol, mapping)
		duplicate := false
		for j := range dst.CallableCandidates {
			existing := &dst.CallableCandidates[j]
			if existing.BodyKey == candidate.BodyKey && existing.Symbol == candidate.Symbol {
				duplicate = true
				break
			}
		}
		if !duplicate {
			dst.CallableCandidates = append(dst.CallableCandidates, candidate)
		}
	}
	sort.SliceStable(dst.CallableCandidates, func(i, j int) bool {
		return dst.CallableCandidates[i].BodyKey < dst.CallableCandidates[j].BodyKey
	})
	mergeFinalizationRequests(dst, src, mapping)
	dst.InstantiationClosure = nil
	dst.rebuildFunctionInstantiations()
}

func (r *Result) rebuildFunctionInstantiations() {
	if r == nil {
		return
	}
	type compatibilityEntry struct {
		template symbols.SymbolID
		args     []types.TypeID
		witness  InstantiationWitness
	}
	entries := make([]compatibilityEntry, 0, len(r.InstantiationGraph.roots))
	finalized := r.InstantiationClosure != nil
	if r.InstantiationClosure != nil {
		entries = make([]compatibilityEntry, 0, len(r.InstantiationClosure.Instances))
		for i := range r.InstantiationClosure.Instances {
			instance := &r.InstantiationClosure.Instances[i]
			entries = append(entries, compatibilityEntry{template: instance.Template, args: instance.TemplateArgs, witness: instance.Witness})
		}
	} else {
		// Before the post-merge closure exists, expose concrete roots only. Raw
		// edge arguments are expressed in a generic caller vocabulary and must
		// never masquerade as concrete compatibility instances.
		for i := range r.InstantiationGraph.roots {
			root := &r.InstantiationGraph.roots[i]
			entries = append(entries, compatibilityEntry{template: root.Template, args: root.TemplateArgs, witness: root.Witness})
		}
	}
	if !finalized {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].template != entries[j].template {
				return entries[i].template < entries[j].template
			}
			if cmp := slices.Compare(entries[i].args, entries[j].args); cmp != 0 {
				return cmp < 0
			}
			return compareInstantiationWitness(&entries[i].witness, &entries[j].witness) < 0
		})
	}
	r.FunctionInstantiations = make(map[symbols.SymbolID][][]types.TypeID)
	r.FunctionInstantiationSites = make(map[symbols.SymbolID][]source.Span)
	for i := range entries {
		entry := &entries[i]
		instances := r.FunctionInstantiations[entry.template]
		if len(instances) > 0 && slices.Equal(instances[len(instances)-1], entry.args) {
			continue
		}
		r.FunctionInstantiations[entry.template] = append(instances, slices.Clone(entry.args))
		r.FunctionInstantiationSites[entry.template] = append(r.FunctionInstantiationSites[entry.template], entry.witness.Site)
	}
}

func remapInstantiationSymbol(id symbols.SymbolID, mapping map[symbols.SymbolID]symbols.SymbolID) symbols.SymbolID {
	if mapped, ok := mapping[id]; ok && mapped.IsValid() {
		return mapped
	}
	return id
}

func cloneInstantiationRoot(root *InstantiationRoot) InstantiationRoot {
	cloned := *root
	cloned.TemplateArgs = slices.Clone(root.TemplateArgs)
	cloned.Witness = cloneInstantiationWitness(&root.Witness)
	return cloned
}

func cloneInstantiationEdge(edge *InstantiationEdge) InstantiationEdge {
	cloned := *edge
	cloned.CallerBindings = slices.Clone(edge.CallerBindings)
	cloned.CalleeTemplateArgs = slices.Clone(edge.CalleeTemplateArgs)
	cloned.Witness = cloneInstantiationWitness(&edge.Witness)
	return cloned
}

func cloneDeferredCallableEdge(edge *DeferredCallableEdge) DeferredCallableEdge {
	cloned := *edge
	cloned.CallerBindings = slices.Clone(edge.CallerBindings)
	cloned.Args = slices.Clone(edge.Args)
	cloned.ExplicitTypeArgs = slices.Clone(edge.ExplicitTypeArgs)
	cloned.Requirement = cloneDeferredCallableRequirement(edge.Requirement)
	cloned.Witness = cloneInstantiationWitness(&edge.Witness)
	return cloned
}

func compareInstantiationBinding(left, right InstantiationParamBinding) int {
	if left.ArgIndex != right.ArgIndex {
		if left.ArgIndex < right.ArgIndex {
			return -1
		}
		return 1
	}
	if left.Owner != right.Owner {
		if left.Owner < right.Owner {
			return -1
		}
		return 1
	}
	if left.Param != right.Param {
		if left.Param < right.Param {
			return -1
		}
		return 1
	}
	if left.ParamIndex < right.ParamIndex {
		return -1
	}
	if left.ParamIndex > right.ParamIndex {
		return 1
	}
	return 0
}

func cloneInstantiationWitness(witness *InstantiationWitness) InstantiationWitness {
	cloned := *witness
	cloned.Steps = slices.Clone(witness.Steps)
	return cloned
}

func compareInstantiationWitness(left, right *InstantiationWitness) int {
	if cmp := compareWitnessSite(left, right); cmp != 0 {
		return cmp
	}
	if left.CallerKey != right.CallerKey {
		return strings.Compare(left.CallerKey, right.CallerKey)
	}
	if left.CallerKey == "" && left.Caller != right.Caller {
		if left.Caller < right.Caller {
			return -1
		}
		return 1
	}
	if left.Reason < right.Reason {
		return -1
	}
	if left.Reason > right.Reason {
		return 1
	}
	return 0
}

func compareWitnessSite(left, right *InstantiationWitness) int {
	if left.SourceKey != right.SourceKey {
		return strings.Compare(left.SourceKey, right.SourceKey)
	}
	if left.SourceKey != "" {
		return compareSpanOffsets(left.Site, right.Site)
	}
	return compareSpan(left.Site, right.Site)
}

func compareSpanOffsets(left, right source.Span) int {
	if left.Start != right.Start {
		if left.Start < right.Start {
			return -1
		}
		return 1
	}
	if left.End < right.End {
		return -1
	}
	if left.End > right.End {
		return 1
	}
	return 0
}

func compareSpan(left, right source.Span) int {
	if left.File != right.File {
		if left.File < right.File {
			return -1
		}
		return 1
	}
	if left.Start != right.Start {
		if left.Start < right.Start {
			return -1
		}
		return 1
	}
	if left.End < right.End {
		return -1
	}
	if left.End > right.End {
		return 1
	}
	return 0
}
