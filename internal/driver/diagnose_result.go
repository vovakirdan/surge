package driver

import (
	"sort"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/project"
	"surge/internal/sema"
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

// ModuleFiles returns file paths for all modules resolved during diagnostics.
func (r *DiagnoseResult) ModuleFiles() []string {
	if r == nil || r.moduleRecords == nil {
		return nil
	}
	seen := make(map[string]struct{})
	files := make([]string, 0, len(r.moduleRecords))
	for _, rec := range r.moduleRecords {
		if rec == nil {
			continue
		}
		for _, file := range rec.Files {
			if file == nil || file.Path == "" {
				continue
			}
			if _, ok := seen[file.Path]; ok {
				continue
			}
			seen[file.Path] = struct{}{}
			files = append(files, file.Path)
		}
	}
	sort.Strings(files)
	return files
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

// ModuleAnalysis exposes one dependency module's parsed AST and per-file sema
// results for pipeline passes that must inspect every compiled module, such as
// the crossing backend guards.
type ModuleAnalysis struct {
	Builder *ast.Builder
	Sema    []*sema.Result
}

// DependencyAnalyses returns AST/sema analysis for every resolved module other
// than the root module, in deterministic module-path order.
func (r *DiagnoseResult) DependencyAnalyses() []ModuleAnalysis {
	if r == nil || r.moduleRecords == nil {
		return nil
	}
	paths := make([]string, 0, len(r.moduleRecords))
	seen := make(map[*moduleRecord]struct{}, len(r.moduleRecords))
	for path, rec := range r.moduleRecords {
		if rec == nil || rec == r.rootRecord || rec.Builder == nil {
			continue
		}
		if _, dup := seen[rec]; dup {
			continue
		}
		seen[rec] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	analyses := make([]ModuleAnalysis, 0, len(paths))
	for _, path := range paths {
		rec := r.moduleRecords[path]
		semaResults := make([]*sema.Result, 0, len(rec.Sema))
		for _, fileID := range rec.FileIDs {
			if sr := rec.Sema[fileID]; sr != nil {
				semaResults = append(semaResults, sr)
			}
		}
		analyses = append(analyses, ModuleAnalysis{Builder: rec.Builder, Sema: semaResults})
	}
	return analyses
}

// MergeModuleDiagnostics merges diagnostics from dependency modules into the root bag.
// This is useful for build pipelines that must fail on dependency errors.
func (r *DiagnoseResult) MergeModuleDiagnostics() {
	if r == nil || r.Bag == nil || r.moduleRecords == nil {
		return
	}
	for _, rec := range r.moduleRecords {
		if rec == nil || rec.Bag == nil || rec.Bag == r.Bag {
			continue
		}
		r.Bag.Merge(rec.Bag)
	}
	r.Bag.Dedup()
	r.Bag.Sort()
}
