package layout

import (
	"fortio.org/safecast"

	"surge/internal/types"
)

// TypeLayout is the ABI layout of a type for a specific Target.
type TypeLayout struct {
	Size  int
	Align int

	// Struct-only:
	FieldOffsets []int
	FieldAligns  []int

	// Tag-union (v1) fields, for ABI queries only.
	TagSize       int
	TagAlign      int
	PayloadOffset int
}

// LayoutEngine computes memory layout for types.
type LayoutEngine struct {
	Target Target
	Types  *types.Interner

	cache *cache
}

// New creates a new LayoutEngine for the specified target.
func New(target Target, typesIn *types.Interner) *LayoutEngine {
	return &LayoutEngine{
		Target: target,
		Types:  typesIn,
		cache:  newCache(),
	}
}

type layoutState struct {
	stack []cacheKey
	index map[cacheKey]int
}

func newLayoutState() *layoutState {
	return &layoutState{
		stack: nil,
		index: make(map[cacheKey]int, 32),
	}
}

// LayoutOf computes and caches the layout of a type.
func (e *LayoutEngine) LayoutOf(t types.TypeID) (TypeLayout, error) {
	if e == nil {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	if e.cache == nil {
		e.cache = newCache()
	}
	state := newLayoutState()
	layout, err := e.layoutOf(t, state)
	if err != nil {
		return layout, err
	}
	return layout, nil
}

func (e *LayoutEngine) layoutOf(t types.TypeID, state *layoutState) (TypeLayout, *LayoutError) {
	if e == nil {
		return TypeLayout{Size: 0, Align: 1}, nil
	}
	if state == nil {
		state = newLayoutState()
	}
	canon := canonicalType(e.Types, t)
	key := cacheKey{Type: canon, Attrs: e.attrsFingerprint(canon)}
	if cached, ok := e.cache.get(key); ok {
		return cached.Layout, cached.Err
	}

	if idx, ok := state.index[key]; ok {
		cycleKeys := append([]cacheKey(nil), state.stack[idx:]...)
		cycleKeys = append(cycleKeys, key)
		cycle := make([]types.TypeID, 0, len(cycleKeys))
		for _, k := range cycleKeys {
			cycle = append(cycle, k.Type)
		}
		err := &LayoutError{
			Kind:  LayoutErrRecursiveUnsized,
			Type:  key.Type,
			Cycle: cycle,
		}
		if e.cache != nil {
			e.cache.put(key, &cacheEntry{Layout: TypeLayout{Size: 0, Align: 1}, Err: err})
		}
		return TypeLayout{Size: 0, Align: 1}, err
	}

	state.index[key] = len(state.stack)
	state.stack = append(state.stack, key)
	layout, err := e.computeLayout(canon, state)
	state.stack = state.stack[:len(state.stack)-1]
	delete(state.index, key)

	if e.cache != nil {
		e.cache.put(key, &cacheEntry{Layout: layout, Err: err})
	}
	return layout, err
}

// SizeOf returns the size of a type in bytes.
func (e *LayoutEngine) SizeOf(t types.TypeID) (int, error) {
	l, err := e.LayoutOf(t)
	return l.Size, err
}

// AlignOf returns the alignment requirement of a type in bytes.
func (e *LayoutEngine) AlignOf(t types.TypeID) (int, error) {
	l, err := e.LayoutOf(t)
	return l.Align, err
}

// FieldOffset returns the byte offset of a struct field.
func (e *LayoutEngine) FieldOffset(structT types.TypeID, fieldIdx int) (int, error) {
	l, err := e.LayoutOf(structT)
	if err != nil {
		return 0, err
	}
	if fieldIdx < 0 || fieldIdx >= len(l.FieldOffsets) {
		return 0, nil
	}
	return l.FieldOffsets[fieldIdx], nil
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
	mix := func(x uint64) {
		hash ^= x
		hash *= fnvPrime64
	}

	attrs, ok := e.Types.TypeLayoutAttrs(id)
	if ok {
		if attrs.Packed {
			mix(1)
		} else {
			mix(0)
		}
		if attrs.AlignOverride != nil {
			if n, err := safecast.Conv[uint64](*attrs.AlignOverride); err == nil {
				mix(n)
			} else {
				mix(0)
			}
		} else {
			mix(0)
		}
	}

	tt, ok := e.Types.Lookup(id)
	if !ok {
		return hash
	}
	if tt.Kind != types.KindStruct {
		return hash
	}
	info, ok := e.Types.StructInfo(id)
	if !ok || info == nil || len(info.Fields) == 0 {
		return hash
	}
	mix(uint64(len(info.Fields)))
	for _, f := range info.Fields {
		if f.Layout.AlignOverride != nil {
			if n, err := safecast.Conv[uint64](*f.Layout.AlignOverride); err == nil {
				mix(n)
			} else {
				mix(0)
			}
		} else {
			mix(0)
		}
	}
	return hash
}
