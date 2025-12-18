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
	key := cacheKey{Type: canon, Attrs: e.attrsFingerprint(canon)}
	if cached, ok := e.cache.get(key); ok {
		return cached
	}
	layout := e.computeLayout(canon)
	e.cache.put(key, &layout)
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
