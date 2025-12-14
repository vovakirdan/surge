package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// InstantiationRecorder captures concrete generic instantiations as a first-class artefact.
//
// It is intentionally defined in sema (not in internal/mono) to avoid import cycles;
// later phases can provide implementations.
type InstantiationRecorder interface {
	RecordFnInstantiation(fn symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string)
	RecordTypeInstantiation(typeSym symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string)
	RecordTagInstantiation(tag symbols.SymbolID, typeArgs []types.TypeID, site source.Span, caller symbols.SymbolID, note string)
}
