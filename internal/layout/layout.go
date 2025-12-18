package layout

import "surge/internal/types"

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

type LayoutEngine struct {
	Target Target
	Types  *types.Interner

	cache *cache
}

func New(target Target, typesIn *types.Interner) *LayoutEngine {
	return &LayoutEngine{
		Target: target,
		Types:  typesIn,
		cache:  newCache(),
	}
}

func (e *LayoutEngine) LayoutOf(t types.TypeID) TypeLayout {
	if e == nil {
		return TypeLayout{Size: 0, Align: 1}
	}
	if e.cache == nil {
		e.cache = newCache()
	}
	canon := canonicalType(e.Types, t)
	if cached, ok := e.cache.get(canon); ok {
		return cached
	}
	layout := e.computeLayout(canon)
	e.cache.put(canon, &layout)
	return layout
}

func (e *LayoutEngine) SizeOf(t types.TypeID) int {
	return e.LayoutOf(t).Size
}

func (e *LayoutEngine) AlignOf(t types.TypeID) int {
	return e.LayoutOf(t).Align
}

func (e *LayoutEngine) FieldOffset(structT types.TypeID, fieldIdx int) int {
	l := e.LayoutOf(structT)
	if fieldIdx < 0 || fieldIdx >= len(l.FieldOffsets) {
		return 0
	}
	return l.FieldOffsets[fieldIdx]
}
