package mono

import (
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type InstantiationMapRecorder struct {
	Map *InstantiationMap
}

var _ sema.InstantiationRecorder = (*InstantiationMapRecorder)(nil)

func NewInstantiationMapRecorder(m *InstantiationMap) *InstantiationMapRecorder {
	return &InstantiationMapRecorder{Map: m}
}

func (r *InstantiationMapRecorder) RecordFnInstantiation(fn symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string) {
	if r == nil || r.Map == nil {
		return
	}
	r.Map.Record(InstFn, fn, typeArgs, site, caller, note)
}

func (r *InstantiationMapRecorder) RecordTypeInstantiation(typeSym symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string) {
	if r == nil || r.Map == nil {
		return
	}
	r.Map.Record(InstType, typeSym, typeArgs, site, caller, note)
}

func (r *InstantiationMapRecorder) RecordTagInstantiation(tag symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string) {
	if r == nil || r.Map == nil {
		return
	}
	r.Map.Record(InstTag, tag, typeArgs, site, caller, note)
}
