package driver

import (
	"context"
	"errors"
	"path/filepath"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"
)

// ensureStdlibModules ensures that required standard library modules are loaded.
func ensureStdlibModules(
	ctx context.Context,
	fs *source.FileSet,
	records map[string]*moduleRecord,
	opts *DiagnoseOptions,
	cache *ModuleCache,
	stdlibRoot string,
	typeInterner *types.Interner,
	strs *source.Interner,
) error {
	if stdlibRoot == "" {
		return nil
	}
	exports := collectedExports(records)
	for _, module := range []string{
		stdModuleCore,
	} {
		if _, ok := records[module]; ok {
			continue
		}
		rec, err := loadStdModule(ctx, fs, module, stdlibRoot, opts, cache, exports, typeInterner, strs)
		if err != nil {
			if errors.Is(err, errStdModuleMissing) {
				continue
			}
			return err
		}
		if rec.Exports != nil {
			exports[normalizeExportsKey(rec.Meta.Path)] = rec.Exports
		}
		records[rec.Meta.Path] = rec
	}
	return nil
}

// loadStdModule loads a standard library module from the stdlib root directory.
func loadStdModule(
	ctx context.Context,
	fs *source.FileSet,
	modulePath string,
	stdlibRoot string,
	opts *DiagnoseOptions,
	cache *ModuleCache,
	moduleExports map[string]*symbols.ModuleExports,
	typeInterner *types.Interner,
	strs *source.Interner,
) (retRec *moduleRecord, retErr error) {
	tracer := trace.FromContext(ctx)
	span := trace.Begin(tracer, trace.ScopeModule, "load_std_module", 0)
	span.WithExtra("module", modulePath)
	defer func() {
		if retRec != nil && !retRec.Broken {
			span.End("ok")
		} else {
			span.End("broken")
		}
	}()
	if stdlibRoot == "" {
		return nil, errStdModuleMissing
	}
	filePath, ok := stdModuleFilePath(stdlibRoot, modulePath)
	if !ok {
		return nil, errStdModuleMissing
	}
	dirPath := filepath.Dir(filePath)
	bag := diag.NewBag(opts.MaxDiagnostics)
	builder, fileIDs, files, err := parseModuleDir(ctx, fs, dirPath, bag, strs, nil, nil, parser.DirectiveModeOff)
	if err != nil {
		return nil, err
	}
	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fs, builder, fileIDs, stdlibRoot, nil, reporter)
	if !ok && len(files) > 0 && files[0] != nil {
		meta = fallbackModuleMeta(files[0], stdlibRoot, nil)
	}
	if meta != nil && !meta.HasModulePragma && len(fileIDs) > 1 {
		normTarget := filepath.ToSlash(filePath)
		idx := 0
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
		meta, ok = buildModuleMeta(fs, builder, fileIDs, stdlibRoot, nil, reporter)
		if !ok && len(files) > 0 && files[0] != nil {
			meta = fallbackModuleMeta(files[0], stdlibRoot, nil)
		}
		if bag != nil && len(files) > 0 && files[0] != nil {
			targetID := files[0].ID
			bag.Filter(func(d *diag.Diagnostic) bool {
				return d.Primary.File == targetID
			})
		}
	}
	if len(files) > 0 && !validateCoreModule(meta, files[0], stdlibRoot, reporter) {
		return nil, errCoreNamespaceReserved
	}
	broken, firstErr := moduleStatus(bag)
	if cache != nil {
		cache.Put(meta, broken, firstErr)
	}
	rec := &moduleRecord{
		Meta:     meta,
		Bag:      bag,
		Broken:   broken,
		FirstErr: firstErr,
		Builder:  builder,
		FileIDs:  fileIDs,
		Files:    files,
	}
	exports := resolveModuleRecord(ctx, rec, stdlibRoot, moduleExports, typeInterner, opts, nil)
	if exports != nil {
		rec.Exports = exports
		if rec.Symbols != nil {
			for i := range rec.Symbols {
				res := rec.Symbols[i]
				markSymbolsBuiltin(&res)
				rec.Symbols[i] = res
			}
		}
	}
	return rec, nil
}
