package driver

import (
	"slices"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// importedCallableIndex answers which declaration an imported callable copy
// stands for. The resolver's copy of an export keeps the export's declaration
// span, declared name, receiver and signature spelling; the declaration's own
// catalog record keeps the same facts beside its canonical body identity.
// Captured before any graph merge, so neither side has been renumbered.
type importedCallableIndex struct {
	bySource      map[source.Span][]*sema.CallableCandidate
	resolveSource func(source.FileID) (string, error)
}

func newImportedCallableIndex(results []*sema.Result, resolveSource func(source.FileID) (string, error)) importedCallableIndex {
	index := importedCallableIndex{bySource: make(map[source.Span][]*sema.CallableCandidate), resolveSource: resolveSource}
	for _, result := range results {
		if result == nil {
			continue
		}
		for i := range result.CallableCandidates {
			candidate := &result.CallableCandidates[i]
			if candidate.BodyKey != "" && candidate.SourceKey != "" {
				index.bySource[candidate.Source] = append(index.bySource[candidate.Source], candidate)
			}
		}
	}
	return index
}

// finalizationRecordTables lists the symbol vocabularies a record's owning
// units select in: each file's resolved table, or the program's own table for
// a result diagnosed without records.
func finalizationRecordTables(res *DiagnoseResult, rec *moduleRecord) []*symbols.Table {
	var tables []*symbols.Table
	if rec == nil {
		if res.Symbols != nil && res.Symbols.Table != nil {
			tables = append(tables, res.Symbols.Table)
		}
		return tables
	}
	for _, fileID := range rec.FileIDs {
		if resolved, ok := rec.Symbols[fileID]; ok && resolved.Table != nil && !slices.Contains(tables, resolved.Table) {
			tables = append(tables, resolved.Table)
		}
	}
	return tables
}

// capture publishes one identity per imported callable copy that names
// exactly one declaration body. A copy whose facts meet no declaration
// publishes nothing; one whose facts meet more than one body, or a symbol
// that two vocabularies publish differently, maps to the empty identity, and
// a selection of it stays refused where it is made.
func (index importedCallableIndex) capture(tables []*symbols.Table) map[symbols.SymbolID]sema.FinalizationCallableIdentity {
	out := make(map[symbols.SymbolID]sema.FinalizationCallableIdentity)
	for _, table := range tables {
		if table == nil || table.Symbols == nil || table.Strings == nil {
			continue
		}
		for i := 1; i <= table.Symbols.Len(); i++ {
			id := symbols.SymbolID(i) //nolint:gosec // bounded by the table length
			identity, listed := index.identity(table, id)
			if !listed {
				continue
			}
			if previous, twice := out[id]; twice && previous != identity {
				identity = sema.FinalizationCallableIdentity{Symbol: id}
			}
			out[id] = identity
		}
	}
	return out
}

func (index importedCallableIndex) identity(table *symbols.Table, id symbols.SymbolID) (sema.FinalizationCallableIdentity, bool) {
	none := sema.FinalizationCallableIdentity{}
	sym := table.Symbols.Get(id)
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Flags&symbols.SymbolFlagImported == 0 || sym.Signature == nil {
		return none, false
	}
	declared := sym.ImportName
	if declared == source.NoStringID {
		declared = sym.Name
	}
	name, named := table.Strings.Lookup(declared)
	sourceKey, err := index.resolveSource(sym.Span.File)
	if !named || err != nil || sourceKey == "" {
		return none, false
	}
	found := none
	for _, candidate := range index.bySource[sym.Span] {
		// A builtin declaration's record names its source "builtin"; the span,
		// whose file is part of it, still pins the one declaration.
		builtin := candidate.Builtin && candidate.SourceKey == "builtin" && sym.Flags&symbols.SymbolFlagBuiltin != 0
		if (candidate.SourceKey != sourceKey && !builtin) || candidate.Name != name || candidate.ReceiverKey != sym.ReceiverKey ||
			candidate.Result != sym.Signature.Result || !slices.Equal(candidate.Params, sym.Signature.Params) {
			continue
		}
		if found.BodyKey != "" && found.BodyKey != candidate.BodyKey {
			return sema.FinalizationCallableIdentity{Symbol: id}, true
		}
		found = sema.FinalizationCallableIdentity{Symbol: id, BodyKey: candidate.BodyKey, SourceKey: candidate.SourceKey}
	}
	return found, found.BodyKey != ""
}
