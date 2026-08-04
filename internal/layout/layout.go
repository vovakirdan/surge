package layout

import "surge/internal/types"

// State distinguishes usable physical facts from deferred and failed layouts.
type State uint8

const (
	// StateConcrete contains usable non-zero-sized physical facts.
	StateConcrete State = iota + 1
	// StateZST contains usable zero-sized physical facts.
	StateZST
	// StateDeferred still depends on generic information.
	StateDeferred
	// StateError has no usable physical facts.
	StateError
)

func (s State) String() string {
	switch s {
	case StateConcrete:
		return "Concrete"
	case StateZST:
		return "ZST"
	case StateDeferred:
		return "Deferred"
	case StateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// PhysicalLayout is a fail-closed discriminated result. Physical facts are
// accessible only for Concrete and ZST through Physical.
type PhysicalLayout struct {
	state State
	facts PhysicalFacts
}

// PhysicalFacts is returned only for Concrete and ZST results. Slice storage
// remains private and is copied on every public return.
type PhysicalFacts struct {
	Size     uint64
	Align    uint64
	Stride   uint64
	TagSize  uint64
	TagAlign uint64

	fieldOffsets []uint64
	fieldAligns  []uint64
	unionCases   []UnionCaseLayout
}

// UnionCaseLayout contains every canonical physical fact for one direct union
// member. Field offsets are relative to PayloadOffset and correspond to tag
// arguments in declaration order (one field for a plain type, none for nothing).
type UnionCaseLayout struct {
	PayloadOffset uint64
	PayloadSize   uint64
	PayloadAlign  uint64

	fieldOffsets []uint64
}

func (c UnionCaseLayout) clone() UnionCaseLayout {
	c.fieldOffsets = append([]uint64(nil), c.fieldOffsets...)
	return c
}

// FieldOffsets returns an owned copy of payload field offsets.
func (c UnionCaseLayout) FieldOffsets() []uint64 {
	return append([]uint64(nil), c.fieldOffsets...)
}

// FieldOffset returns one payload field offset when present.
func (c UnionCaseLayout) FieldOffset(index int) (uint64, bool) {
	if index < 0 || index >= len(c.fieldOffsets) {
		return 0, false
	}
	return c.fieldOffsets[index], true
}

func (l *PhysicalLayout) clone() PhysicalLayout {
	if l == nil {
		return PhysicalLayout{}
	}
	out := *l
	out.facts = l.facts.clone()
	return out
}

func (f *PhysicalFacts) clone() PhysicalFacts {
	if f == nil {
		return PhysicalFacts{}
	}
	out := *f
	out.fieldOffsets = append([]uint64(nil), f.fieldOffsets...)
	out.fieldAligns = append([]uint64(nil), f.fieldAligns...)
	out.unionCases = cloneUnionCases(f.unionCases)
	return out
}

// State reports the discriminant without exposing fabricated facts.
func (l *PhysicalLayout) State() State {
	if l == nil {
		return StateError
	}
	return l.state
}

// Physical returns owned facts only for Concrete and ZST.
func (l *PhysicalLayout) Physical() (PhysicalFacts, bool) {
	if l == nil || (l.state != StateConcrete && l.state != StateZST) {
		return PhysicalFacts{}, false
	}
	return l.facts.clone(), true
}

func cloneUnionCases(cases []UnionCaseLayout) []UnionCaseLayout {
	if len(cases) == 0 {
		return nil
	}
	out := make([]UnionCaseLayout, len(cases))
	for i := range cases {
		out[i] = cases[i].clone()
	}
	return out
}

// FieldOffsets returns an owned copy in declaration order.
func (f *PhysicalFacts) FieldOffsets() []uint64 {
	if f == nil {
		return nil
	}
	return append([]uint64(nil), f.fieldOffsets...)
}

// FieldAligns returns an owned copy in declaration order.
func (f *PhysicalFacts) FieldAligns() []uint64 {
	if f == nil {
		return nil
	}
	return append([]uint64(nil), f.fieldAligns...)
}

// UnionCases returns deep owned copies in declaration order.
func (f *PhysicalFacts) UnionCases() []UnionCaseLayout {
	if f == nil {
		return nil
	}
	return cloneUnionCases(f.unionCases)
}

// FieldOffset returns one aggregate field offset when present.
func (f *PhysicalFacts) FieldOffset(index int) (uint64, bool) {
	if f == nil || index < 0 || index >= len(f.fieldOffsets) {
		return 0, false
	}
	return f.fieldOffsets[index], true
}

// UnionCase returns an owned copy of one union case layout.
func (f *PhysicalFacts) UnionCase(index int) (UnionCaseLayout, bool) {
	if f == nil || index < 0 || index >= len(f.unionCases) {
		return UnionCaseLayout{}, false
	}
	return f.unionCases[index].clone(), true
}

func deferredLayout() PhysicalLayout { return PhysicalLayout{state: StateDeferred} }
func errorLayout() PhysicalLayout    { return PhysicalLayout{state: StateError} }

// LayoutEngine is the mutable, single-threaded physical-layout builder. It may
// consult the type interner; production backends consume only a frozen Registry.
type LayoutEngine struct { //nolint:revive
	Target Target
	Types  *types.Interner

	cache         *cache
	substitutions map[types.TypeID]types.TypeID
}

// New creates a mutable builder for the specified target.
func New(target Target, typesIn *types.Interner) *LayoutEngine {
	return &LayoutEngine{Target: target, Types: typesIn, cache: newCache()}
}

// NewWithSubstitutions creates a non-mutating proof engine for one generic
// instantiation. Generic parameters are resolved while walking the original
// type graph, so validation never allocates concrete types in the interner.
func NewWithSubstitutions(
	target Target,
	typesIn *types.Interner,
	substitutions map[types.TypeID]types.TypeID,
) *LayoutEngine {
	owned := make(map[types.TypeID]types.TypeID, len(substitutions))
	for from, to := range substitutions {
		if from != types.NoTypeID && to != types.NoTypeID && from != to {
			owned[from] = to
		}
	}
	return &LayoutEngine{Target: target, Types: typesIn, cache: newCache(), substitutions: owned}
}

type layoutState struct {
	stack []cacheKey
	index map[cacheKey]int
}

func newLayoutState() *layoutState {
	return &layoutState{index: make(map[cacheKey]int, 32)}
}

// LayoutOf computes and caches one physical layout. Deferred is a successful
// state; all invalid or unsupported inputs return StateError and a LayoutError.
func (e *LayoutEngine) LayoutOf(t types.TypeID) (PhysicalLayout, error) {
	if e == nil {
		err := &LayoutError{Kind: ErrInvalidTarget, Type: t, Operation: "nil builder"}
		return errorLayout(), err
	}
	if err := validateTarget(e.Target, t); err != nil {
		return errorLayout(), err
	}
	if e.cache == nil {
		e.cache = newCache()
	}
	l, err := e.layoutOf(t, newLayoutState())
	if err != nil {
		return errorLayout(), err
	}
	return l.clone(), nil
}

func (e *LayoutEngine) layoutOf(t types.TypeID, state *layoutState) (PhysicalLayout, *LayoutError) {
	canon, prefix, canonErr := e.canonicalTypePath(t)
	if canonErr != nil {
		return errorLayout(), canonErr.prependPath(t, prefix)
	}
	key := cacheKey{Type: canon, Attrs: e.attrsFingerprint(canon)}
	if cached, ok := e.cache.get(key); ok {
		return cached.Layout, cached.Err.prependPath(t, prefix)
	}
	if idx, ok := state.index[key]; ok {
		cycle := make([]types.TypeID, 0, len(state.stack)-idx+1)
		for _, item := range state.stack[idx:] {
			cycle = append(cycle, item.Type)
		}
		cycle = append(cycle, key.Type)
		err := &LayoutError{Kind: ErrRecursiveUnsized, Type: key.Type, cycle: cycle}
		return errorLayout(), err.prependPath(t, prefix)
	}

	state.index[key] = len(state.stack)
	state.stack = append(state.stack, key)
	l, err := e.computeLayout(canon, state)
	state.stack = state.stack[:len(state.stack)-1]
	delete(state.index, key)
	if err != nil {
		err = err.withRoot(canon)
	}
	e.cache.put(key, &cacheEntry{Layout: l, Err: err})
	return l.clone(), err.prependPath(t, prefix)
}

// CanonicalType returns the builder's physical-layout identity for a type.
func (e *LayoutEngine) CanonicalType(id types.TypeID) (types.TypeID, error) {
	canon, err := e.canonicalType(id)
	if err != nil {
		return types.NoTypeID, err
	}
	return canon, nil
}

func (e *LayoutEngine) canonicalType(id types.TypeID) (types.TypeID, *LayoutError) {
	canonical, _, err := e.canonicalTypePath(id)
	return canonical, err
}

func (e *LayoutEngine) canonicalTypePath(id types.TypeID) (types.TypeID, []PathElement, *LayoutError) {
	if id == types.NoTypeID || e == nil || e.Types == nil {
		return types.NoTypeID, nil, &LayoutError{Kind: ErrUnknownType, Type: id}
	}
	seen := make(map[types.TypeID]struct{}, 8)
	path := make([]PathElement, 0, 4)
	for id != types.NoTypeID {
		id = e.substitutedType(id)
		if _, ok := seen[id]; ok {
			return types.NoTypeID, path, &LayoutError{Kind: ErrUnsupportedKind, Type: id, Operation: "alias cycle"}
		}
		seen[id] = struct{}{}
		t, ok := e.Types.Lookup(id)
		if !ok {
			return types.NoTypeID, path, &LayoutError{Kind: ErrUnknownType, Type: id}
		}
		switch t.Kind {
		case types.KindAlias:
			target, ok := e.Types.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return types.NoTypeID, path, &LayoutError{Kind: ErrUnknownType, Type: id, Operation: "alias target"}
			}
			path = append(path, PathElement{Kind: PathAliasTarget})
			id = target
		case types.KindOwn:
			if t.Elem == types.NoTypeID {
				return types.NoTypeID, path, &LayoutError{Kind: ErrUnknownType, Type: id, Operation: "owned value"}
			}
			path = append(path, PathElement{Kind: PathOwnValue})
			id = t.Elem
		default:
			return id, path, nil
		}
	}
	return types.NoTypeID, path, &LayoutError{Kind: ErrUnknownType, Type: id}
}

func (e *LayoutEngine) substitutedType(id types.TypeID) types.TypeID {
	if e == nil || len(e.substitutions) == 0 {
		return id
	}
	seen := make(map[types.TypeID]struct{}, 4)
	for id != types.NoTypeID {
		if _, cycle := seen[id]; cycle {
			return id
		}
		seen[id] = struct{}{}
		next, ok := e.substitutions[id]
		if !ok || next == types.NoTypeID || next == id {
			return id
		}
		id = next
	}
	return id
}

// SizeOf returns the checked physical size of a concrete type.
func (e *LayoutEngine) SizeOf(t types.TypeID) (uint64, error) {
	l, err := e.LayoutOf(t)
	if err != nil {
		return 0, err
	}
	if l.State() == StateDeferred {
		return 0, &LayoutError{Kind: ErrDeferred, Type: t}
	}
	facts, ok := l.Physical()
	if !ok {
		return 0, &LayoutError{Kind: ErrInvalidQuery, Type: t, Operation: "size"}
	}
	return facts.Size, nil
}

// AlignOf returns the checked physical alignment of a concrete type.
func (e *LayoutEngine) AlignOf(t types.TypeID) (uint64, error) {
	l, err := e.LayoutOf(t)
	if err != nil {
		return 0, err
	}
	if l.State() == StateDeferred {
		return 0, &LayoutError{Kind: ErrDeferred, Type: t}
	}
	facts, ok := l.Physical()
	if !ok {
		return 0, &LayoutError{Kind: ErrInvalidQuery, Type: t, Operation: "alignment"}
	}
	return facts.Align, nil
}

// FieldOffset returns one checked aggregate field offset.
func (e *LayoutEngine) FieldOffset(structT types.TypeID, fieldIdx int) (uint64, error) {
	l, err := e.LayoutOf(structT)
	if err != nil {
		return 0, err
	}
	if l.State() == StateDeferred {
		return 0, &LayoutError{Kind: ErrDeferred, Type: structT}
	}
	facts, physical := l.Physical()
	if !physical {
		return 0, &LayoutError{Kind: ErrInvalidQuery, Type: structT, Operation: "field offset"}
	}
	if offset, ok := facts.FieldOffset(fieldIdx); ok {
		return offset, nil
	}
	return 0, &LayoutError{Kind: ErrInvalidQuery, Type: structT, Operation: "field offset"}
}

func (e *LayoutEngine) attrsFingerprint(id types.TypeID) uint64 {
	if e == nil || e.Types == nil || id == types.NoTypeID {
		return 0
	}
	const (
		fnvOffset64 = 1469598103934665603
		fnvPrime64  = 1099511628211
	)
	hash := uint64(fnvOffset64)
	mix := func(x uint64) { hash ^= x; hash *= fnvPrime64 }
	attrs, ok := e.Types.TypeLayoutAttrs(id)
	if ok {
		if attrs.Packed {
			mix(1)
		} else {
			mix(0)
		}
		if attrs.AlignOverride != nil {
			mix(*attrs.AlignOverride)
		} else {
			mix(0)
		}
	}
	t, ok := e.Types.Lookup(id)
	if !ok || t.Kind != types.KindStruct {
		return hash
	}
	info, ok := e.Types.StructInfo(id)
	if !ok || info == nil {
		return hash
	}
	mix(uint64(len(info.Fields)))
	for _, field := range info.Fields {
		if field.Layout.AlignOverride != nil {
			mix(*field.Layout.AlignOverride)
		} else {
			mix(0)
		}
	}
	return hash
}
