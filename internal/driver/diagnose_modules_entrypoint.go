package driver

import (
	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
	"surge/internal/symbols"
)

// enforceEntrypoints checks that binary modules have exactly one entrypoint function.
func enforceEntrypoints(rec *moduleRecord, moduleScope symbols.ScopeID) {
	if rec == nil || rec.Meta == nil || rec.Table == nil || rec.checkedEntrypoints {
		return
	}
	rec.checkedEntrypoints = true
	scope := rec.Table.Scopes.Get(moduleScope)
	if scope == nil {
		return
	}
	entryIDs := make([]symbols.SymbolID, 0, 2)
	for _, id := range scope.Symbols {
		sym := rec.Table.Symbols.Get(id)
		if sym == nil || sym.Kind != symbols.SymbolFunction {
			continue
		}
		if sym.Flags&symbols.SymbolFlagEntrypoint == 0 || sym.Flags&symbols.SymbolFlagImported != 0 {
			continue
		}
		entryIDs = append(entryIDs, id)
	}
	reporter := diag.NewDedupReporter(&diag.BagReporter{Bag: rec.Bag})
	if len(entryIDs) == 0 {
		if rec.Meta.Kind == project.ModuleKindBinary {
			errSpan := rec.Meta.Span
			// Try to find the pragma binary span
			if rec.Builder != nil {
				for _, fileID := range rec.FileIDs {
					if f := rec.Builder.Files.Get(fileID); f != nil {
						if !f.Pragma.IsEmpty() {
							for _, entry := range f.Pragma.Entries {
								name, _ := rec.Builder.StringsInterner.Lookup(entry.Name)
								if name == "binary" {
									errSpan = entry.Span
									goto Found
								}
							}
						}
					}
				}
			}
		Found:
			b := diag.ReportError(reporter, diag.SemaEntrypointNotFound, errSpan, "binary module must have exactly one @entrypoint")
			if b != nil {
				b.Emit()
			}
		}
		return
	}
	if len(entryIDs) > 1 {
		var firstSpan source.Span
		if first := rec.Table.Symbols.Get(entryIDs[0]); first != nil {
			firstSpan = first.Span
		}
		for _, id := range entryIDs[1:] {
			sym := rec.Table.Symbols.Get(id)
			if sym == nil {
				continue
			}
			b := diag.ReportError(reporter, diag.SemaMultipleEntrypoints, sym.Span, "multiple @entrypoint functions in module")
			if b != nil && firstSpan != (source.Span{}) {
				b.WithNote(firstSpan, "first @entrypoint is here")
			}
			if b != nil {
				b.Emit()
			}
		}
	}
}
