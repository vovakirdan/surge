package mono

import (
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// InstantiationMapRecorder implements sema.InstantiationRecorder.
type InstantiationMapRecorder struct {
	Map *InstantiationMap
}

var _ sema.InstantiationRecorder = (*InstantiationMapRecorder)(nil)

// NewInstantiationMapRecorder creates a new recorder bound to the provided map.
func NewInstantiationMapRecorder(m *InstantiationMap) *InstantiationMapRecorder {
	return &InstantiationMapRecorder{Map: m}
}

// RecordFnInstantiation implements sema.InstantiationRecorder.
func (r *InstantiationMapRecorder) RecordFnInstantiation(fn symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string) {
	if r == nil || r.Map == nil {
		return
	}
	r.Map.Record(InstFn, fn, typeArgs, site, caller, note)
}

// RecordTypeInstantiation implements sema.InstantiationRecorder.
func (r *InstantiationMapRecorder) RecordTypeInstantiation(typeSym symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string) {
	if r == nil || r.Map == nil {
		return
	}
	r.Map.Record(InstType, typeSym, typeArgs, site, caller, note)
}

// RecordTagInstantiation implements sema.InstantiationRecorder.
func (r *InstantiationMapRecorder) RecordTagInstantiation(tag symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string) {
	if r == nil || r.Map == nil {
		return
	}
	r.Map.Record(InstTag, tag, typeArgs, site, caller, note)
}
