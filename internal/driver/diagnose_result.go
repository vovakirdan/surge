package driver

import (
	"sort"

	"fortio.org/safecast"

	"surge/internal/project"
	"surge/internal/source"
	"surge/internal/symbols"
)

// EntrypointInfo describes an @entrypoint function discovered in the module graph.
type EntrypointInfo struct {
	ModulePath string
	FilePath   string
	Name       string
	Span       source.Span
}

// RootModuleMeta returns metadata for the root module when available.
func (r *DiagnoseResult) RootModuleMeta() *project.ModuleMeta {
	if r == nil || r.rootRecord == nil {
		return nil
	}
	return r.rootRecord.Meta
}

// Entrypoints returns entrypoint metadata across all resolved modules.
func (r *DiagnoseResult) Entrypoints() []EntrypointInfo {
	if r == nil || r.moduleRecords == nil {
		return nil
	}
	entries := make([]EntrypointInfo, 0, 2)
	for modulePath, rec := range r.moduleRecords {
		if rec == nil || rec.Table == nil || rec.Table.Symbols == nil {
			continue
		}
		count := rec.Table.Symbols.Len()
		for i := 1; i <= count; i++ {
			symID, convErr := safecast.Conv[symbols.SymbolID](i)
			if convErr != nil {
				continue
			}
			sym := rec.Table.Symbols.Get(symID)
			if sym == nil || sym.Kind != symbols.SymbolFunction {
				continue
			}
			if sym.Flags&symbols.SymbolFlagEntrypoint == 0 || sym.Flags&symbols.SymbolFlagImported != 0 {
				continue
			}
			name := ""
			if rec.Table.Strings != nil {
				if n, ok := rec.Table.Strings.Lookup(sym.Name); ok {
					name = n
				}
			}
			filePath := ""
			if r.FileSet != nil {
				file := r.FileSet.Get(sym.Span.File)
				if file != nil {
					filePath = file.Path
				}
			}
			entries = append(entries, EntrypointInfo{
				ModulePath: modulePath,
				FilePath:   filePath,
				Name:       name,
				Span:       sym.Span,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ModulePath != entries[j].ModulePath {
			return entries[i].ModulePath < entries[j].ModulePath
		}
		if entries[i].FilePath != entries[j].FilePath {
			return entries[i].FilePath < entries[j].FilePath
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}
