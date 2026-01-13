package driver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/project/dag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"
)

// resolveModuleDir, parseModuleDir moved to diagnose_modules_resolution.go and diagnose_modules_parsing.go

func analyzeDependencyModule(
	ctx context.Context,
	fs *source.FileSet,
	modulePath string,
	baseDir string,
	stdlibRoot string,
	opts *DiagnoseOptions,
	cache *ModuleCache,
	strs *source.Interner,
) (retRec *moduleRecord, retErr error) {
	tracer := trace.FromContext(ctx)
	span := trace.Begin(tracer, trace.ScopeModule, "analyze_dependency", 0)
	span.WithExtra("module", modulePath)
	defer func() {
		status := "ok"
		if retErr != nil {
			status = "error"
		} else if retRec != nil && retRec.Broken {
			status = "broken"
		}
		span.End(status)
	}()

	dirPath, err := resolveModuleDir(modulePath, baseDir, stdlibRoot, opts.ReadFile)
	if err != nil {
		if errors.Is(err, errModuleNotFound) {
			return nil, errModuleNotFound
		}
		return nil, err
	}
	bag := diag.NewBag(opts.MaxDiagnostics)
	builder, fileIDs, files, err := parseModuleDir(ctx, fs, dirPath, bag, strs, nil, nil, parser.DirectiveModeOff)
	if err != nil {
		if errors.Is(err, errModuleNotFound) {
			return nil, errModuleNotFound
		}
		return nil, err
	}
	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fs, builder, fileIDs, baseDir, reporter)
	if !ok {
		fallbackFile := files[0]
		if fallbackFile == nil {
			return nil, errModuleNotFound
		}
		meta = fallbackModuleMeta(fallbackFile, baseDir)
	}
	if meta != nil && !meta.HasModulePragma && len(fileIDs) > 1 {
		targetPath := modulePathToFilePath(baseDir, modulePath)
		idx := 0
		normTarget := filepath.ToSlash(targetPath)
		for i, f := range files {
			if f == nil {
				continue
			}
			if filepath.ToSlash(f.Path) == normTarget {
				idx = i
				break
			}
		}
		fileIDs = []ast.FileID{fileIDs[idx]}
		files = []*source.File{files[idx]}
		meta, ok = buildModuleMeta(fs, builder, fileIDs, baseDir, reporter)
		if !ok {
			meta = fallbackModuleMeta(files[0], baseDir)
		}
		if bag != nil && len(files) > 0 && files[0] != nil {
			targetID := files[0].ID
			bag.Filter(func(d *diag.Diagnostic) bool {
				return d.Primary.File == targetID
			})
		}
	}
	broken, firstErr := moduleStatus(bag)
	if meta.ContentHash == ([32]byte{}) && len(files) > 0 && files[0] != nil {
		meta.ContentHash = files[0].Hash
	}
	if cache != nil {
		cache.Put(meta, broken, firstErr)
	}
	return &moduleRecord{
		Meta:     meta,
		Bag:      bag,
		Broken:   broken,
		FirstErr: firstErr,
		Builder:  builder,
		FileIDs:  fileIDs,
		Files:    files,
	}, nil
}

func resolveModuleRecord(
	ctx context.Context,
	rec *moduleRecord,
	baseDir string,
	moduleExports map[string]*symbols.ModuleExports,
	typeInterner *types.Interner,
	opts *DiagnoseOptions,
	insts sema.InstantiationRecorder,
) *symbols.ModuleExports {
	tracer := trace.FromContext(ctx)
	span := trace.Begin(tracer, trace.ScopeModule, "resolve_module_record", 0)
	if rec != nil && rec.Meta != nil {
		span.WithExtra("module", rec.Meta.Path)
		span.WithExtra("files", fmt.Sprintf("%d", len(rec.FileIDs)))
	}
	defer span.End("")

	if rec == nil || rec.Builder == nil || len(rec.FileIDs) == 0 {
		return nil
	}
	if rec.Exports != nil && rec.Sema != nil && !exportsIncomplete(rec.Exports) {
		return rec.Exports
	}
	bag := rec.Bag
	if bag == nil {
		bag = diag.NewBag(opts.MaxDiagnostics)
	}
	reporter := diag.NewDedupReporter(&diag.BagReporter{Bag: bag})
	table := symbols.NewTable(symbols.Hints{}, rec.Builder.StringsInterner)
	rec.Table = table
	moduleScope := table.ModuleRoot(rec.Meta.Path, rec.Meta.Span)
	moduleFiles := make(map[ast.FileID]struct{}, len(rec.FileIDs))
	fileByID := make(map[ast.FileID]*source.File, len(rec.FileIDs))
	for i, id := range rec.FileIDs {
		moduleFiles[id] = struct{}{}
		if i < len(rec.Files) {
			fileByID[id] = rec.Files[i]
		}
	}

	resolveModulePath := strings.Trim(rec.Meta.Name, "/")
	if rec.Meta.Dir != "" {
		resolveModulePath = strings.Trim(strings.Trim(rec.Meta.Dir, "/")+"/"+rec.Meta.Name, "/")
	}
	noStd := rec.Meta != nil && rec.Meta.NoStd

	// pass 1: declarations
	for _, fileID := range rec.FileIDs {
		filePath := ""
		if f := fileByID[fileID]; f != nil {
			filePath = f.Path
		}
		symbols.ResolveFile(rec.Builder, fileID, &symbols.ResolveOptions{
			Table:         table,
			Reporter:      reporter,
			Validate:      false,
			ModulePath:    resolveModulePath,
			FilePath:      filePath,
			BaseDir:       baseDir,
			ModuleExports: moduleExports,
			ModuleScope:   moduleScope,
			NoStd:         noStd,
			DeclareOnly:   true,
		})
	}

	rec.Sema = make(map[ast.FileID]*sema.Result, len(rec.FileIDs))
	for _, fileID := range rec.FileIDs {
		filePath := ""
		if f := fileByID[fileID]; f != nil {
			filePath = f.Path
		}
		res := symbols.ResolveFile(rec.Builder, fileID, &symbols.ResolveOptions{
			Table:         table,
			Reporter:      reporter,
			Validate:      false,
			ModulePath:    resolveModulePath,
			FilePath:      filePath,
			BaseDir:       baseDir,
			ModuleExports: moduleExports,
			ModuleScope:   moduleScope,
			NoStd:         noStd,
			ReuseDecls:    true,
		})
		res.ModuleFiles = moduleFiles
		if rec.Symbols == nil {
			rec.Symbols = make(map[ast.FileID]symbols.Result)
		}
		rec.Symbols[fileID] = res
		semaRes := sema.Check(ctx, rec.Builder, fileID, sema.Options{
			Reporter:       reporter,
			Symbols:        &res,
			Exports:        moduleExports,
			Types:          typeInterner,
			AlienHints:     !opts.NoAlienHints,
			Bag:            bag,
			Instantiations: insts,
		})
		rec.Sema[fileID] = &semaRes
	}

	enforceEntrypoints(rec, moduleScope)

	rec.Exports = symbols.CollectExports(rec.Builder, symbols.Result{
		Table:       table,
		FileScope:   moduleScope,
		ModuleFiles: moduleFiles,
		File:        rec.FileIDs[0],
	}, rec.Meta.Path)
	return rec.Exports
}

func collectModuleExports(
	ctx context.Context,
	records map[string]*moduleRecord,
	idx dag.ModuleIndex,
	topo *dag.Topo,
	baseDir string,
	rootPath string,
	typeInterner *types.Interner,
	opts *DiagnoseOptions,
) map[string]*symbols.ModuleExports {
	exports := collectedExports(records)
	if exports == nil {
		exports = make(map[string]*symbols.ModuleExports, len(records))
	}
	normalizedRoot := normalizeExportsKey(rootPath)
	if topo != nil && len(topo.Order) > 0 {
		for i := len(topo.Order) - 1; i >= 0; i-- {
			id := topo.Order[i]
			path := idx.IDToName[int(id)]
			normPath := normalizeExportsKey(path)
			rec := records[path]
			if rec == nil && normPath != path {
				rec = records[normPath]
			}
			if rec == nil || normPath == normalizedRoot {
				continue
			}
			if exp := resolveModuleRecord(ctx, rec, baseDir, exports, typeInterner, opts, nil); exp != nil {
				exports[normPath] = exp
			}
		}
	}
	// include any preloaded records that are outside the graph (e.g., core modules)
	for path, rec := range records {
		normPath := normalizeExportsKey(path)
		if _, seen := exports[normPath]; seen {
			continue
		}
		if rec != nil && rec.Exports != nil {
			exports[normPath] = rec.Exports
		}
	}
	return exports
}

// enforceEntrypoints, ensureStdlibModules, and other helper functions moved to separate files

// Utility functions

func moduleStatus(bag *diag.Bag) (bool, *diag.Diagnostic) {
	if bag == nil {
		return false, nil
	}
	items := bag.Items()
	for i := range items {
		if items[i].Severity >= diag.SevError {
			first := items[i]
			copyFirst := first
			return true, copyFirst
		}
	}
	return false, nil
}

func collectedExports(records map[string]*moduleRecord) map[string]*symbols.ModuleExports {
	exports := make(map[string]*symbols.ModuleExports, len(records))
	for path, rec := range records {
		if rec == nil || rec.Exports == nil {
			continue
		}
		exports[normalizeExportsKey(path)] = rec.Exports
	}
	return exports
}

func exportsIncomplete(exp *symbols.ModuleExports) bool {
	if exp == nil {
		return true
	}
	for _, set := range exp.Symbols {
		for i := range set {
			sym := set[i]
			if sym.Kind == symbols.SymbolType && sym.Type == types.NoTypeID {
				return true
			}
			if sym.Kind == symbols.SymbolContract && sym.Contract == nil {
				return true
			}
		}
	}
	return false
}
