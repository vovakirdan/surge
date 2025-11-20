package driver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func analyzeDependencyModule(
	fs *source.FileSet,
	modulePath string,
	baseDir string,
	opts DiagnoseOptions,
	cache *ModuleCache,
) (*moduleRecord, error) {
	filePath := modulePathToFilePath(baseDir, modulePath)
	fileID, err := fs.Load(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errModuleNotFound
		}
		return nil, err
	}
	file := fs.Get(fileID)
	// try cache by modulePath + content hash
	if cache != nil {
		if m, br, fe, ok := cache.Get(modulePath, file.Hash); ok {
			return &moduleRecord{
				Meta:     m,
				Bag:      diag.NewBag(opts.MaxDiagnostics), // пустой bag для согласованности
				Broken:   br,
				FirstErr: fe,
			}, nil
		}
	}
	bag := diag.NewBag(opts.MaxDiagnostics)
	err = diagnoseTokenize(file, bag)
	if err != nil {
		return nil, err
	}
	var builder *ast.Builder
	var astFile ast.FileID
	builder, astFile = diagnoseParse(fs, file, bag)
	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fs, builder, astFile, baseDir, reporter)
	if !ok {
		meta = fallbackModuleMeta(file, baseDir)
	}
	broken, firstErr := moduleStatus(bag)
	// fill content hash (на случай fallback)
	if meta.ContentHash == ([32]byte{}) {
		meta.ContentHash = file.Hash
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
		FileID:   astFile,
		File:     file,
	}, nil
}

func collectModuleExports(
	records map[string]*moduleRecord,
	idx dag.ModuleIndex,
	topo *dag.Topo,
	baseDir string,
	rootPath string,
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
			if rec == nil {
				continue
			}
			if rec.Exports != nil {
				exports[normPath] = rec.Exports
				continue
			}
			if normPath == normalizedRoot {
				continue
			}
			if rec.Builder == nil || rec.FileID == ast.NoFileID {
				continue
			}
			opts := &symbols.ResolveOptions{
				Reporter:      nil,
				Validate:      false,
				ModulePath:    preferredModulePath(rec, normPath),
				FilePath:      moduleFilePath(rec),
				BaseDir:       baseDir,
				ModuleExports: exports,
			}
			res := symbols.ResolveFile(rec.Builder, rec.FileID, opts)
			rec.Exports = symbols.CollectExports(rec.Builder, res, opts.ModulePath)
			if rec.Exports != nil {
				exports[normPath] = rec.Exports
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

func ensureStdlibModules(
	fs *source.FileSet,
	records map[string]*moduleRecord,
	opts DiagnoseOptions,
	cache *ModuleCache,
	stdlibRoot string,
	typeInterner *types.Interner,
) error {
	if stdlibRoot == "" {
		return nil
	}
	exports := collectedExports(records)
	for _, module := range []string{
		stdModuleCoreResult,
		stdModuleCoreOption,
		stdModuleCoreIntrinsics,
		stdModuleCoreBase,
		stdModuleCoreSaturating,
	} {
		if _, ok := records[module]; ok {
			continue
		}
		rec, err := loadStdModule(fs, module, stdlibRoot, opts, cache, exports, typeInterner)
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

func loadStdModule(
	fs *source.FileSet,
	modulePath string,
	stdlibRoot string,
	opts DiagnoseOptions,
	cache *ModuleCache,
	moduleExports map[string]*symbols.ModuleExports,
	typeInterner *types.Interner,
) (*moduleRecord, error) {
	if stdlibRoot == "" {
		return nil, errStdModuleMissing
	}
	filePath, ok := stdModuleFilePath(stdlibRoot, modulePath)
	if !ok {
		return nil, errStdModuleMissing
	}
	fileID, err := fs.Load(filePath)
	if err != nil {
		return nil, err
	}
	file := fs.Get(fileID)
	bag := diag.NewBag(opts.MaxDiagnostics)
	if errTok := diagnoseTokenize(file, bag); errTok != nil {
		return nil, errTok
	}
	builder, astFile := diagnoseParse(fs, file, bag)
	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fs, builder, astFile, stdlibRoot, reporter)
	if !ok {
		meta = fallbackModuleMeta(file, stdlibRoot)
	}
	if !validateCoreModule(meta, file, stdlibRoot, reporter) {
		return nil, errCoreNamespaceReserved
	}
	broken, firstErr := moduleStatus(bag)
	if cache != nil {
		cache.Put(meta, broken, firstErr)
	}
	res := symbols.ResolveFile(builder, astFile, &symbols.ResolveOptions{
		Reporter:      reporter,
		Validate:      true,
		ModulePath:    modulePath,
		FilePath:      file.Path,
		BaseDir:       stdlibRoot,
		ModuleExports: moduleExports,
	})
	markSymbolsBuiltin(&res)
	semaRes := sema.Check(builder, astFile, sema.Options{
		Reporter: reporter,
		Symbols:  &res,
		Exports:  moduleExports,
		Types:    typeInterner,
	})
	exports := symbols.CollectExports(builder, res, modulePath)
	return &moduleRecord{
		Meta:     meta,
		Bag:      bag,
		Broken:   broken,
		FirstErr: firstErr,
		Builder:  builder,
		FileID:   astFile,
		File:     file,
		Sema:     &semaRes,
		Exports:  exports,
	}, nil
}

func moduleFilePath(rec *moduleRecord) string {
	if rec == nil || rec.File == nil {
		return ""
	}
	return rec.File.Path
}

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

func preferredModulePath(rec *moduleRecord, fallback string) string {
	if rec != nil && rec.Meta != nil && rec.Meta.Path != "" {
		if norm := normalizeExportsKey(rec.Meta.Path); norm != "" {
			return norm
		}
	}
	return fallback
}

func normalizeExportsKey(path string) string {
	if norm, err := project.NormalizeModulePath(path); err == nil {
		return norm
	}
	return strings.Trim(path, "/")
}

func modulePathToFilePath(baseDir, modulePath string) string {
	rel := filepath.FromSlash(modulePath) + ".sg"
	if baseDir == "" {
		return rel
	}
	return filepath.Join(baseDir, rel)
}

func modulePathForFile(fs *source.FileSet, file *source.File) string {
	if fs == nil || file == nil {
		return ""
	}
	path := file.Path
	baseDir := fs.BaseDir()
	if baseDir != "" {
		if rel, err := source.RelativePath(path, baseDir); err == nil {
			path = rel
		}
	}
	if norm, err := project.NormalizeModulePath(path); err == nil {
		return norm
	}
	return ""
}
