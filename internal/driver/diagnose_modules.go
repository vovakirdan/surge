package driver

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
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

func resolveModuleDir(modulePath, baseDir string) (string, error) {
	filePath := modulePathToFilePath(baseDir, modulePath)
	if st, err := os.Stat(filePath); err == nil && !st.IsDir() {
		return filepath.Dir(filePath), nil
	}
	dirCandidate := filepath.FromSlash(modulePath)
	if baseDir != "" {
		dirCandidate = filepath.Join(baseDir, dirCandidate)
	}
	if st, err := os.Stat(dirCandidate); err == nil && st.IsDir() {
		return dirCandidate, nil
	}
	if strings.Contains(modulePath, "/") {
		parent := filepath.Dir(filepath.FromSlash(modulePath))
		if baseDir != "" {
			parent = filepath.Join(baseDir, parent)
		}
		if st, err := os.Stat(parent); err == nil && st.IsDir() {
			return parent, nil
		}
	}
	return "", errModuleNotFound
}

func parseModuleDir(
	fs *source.FileSet,
	dir string,
	bag *diag.Bag,
	strs *source.Interner,
	builder *ast.Builder,
	preloaded map[string]ast.FileID,
) (*ast.Builder, []ast.FileID, []*source.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	paths := make([]string, 0, len(entries))
	dirNorm := filepath.ToSlash(filepath.Clean(dir))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		if filepath.Ext(ent.Name()) != ".sg" {
			continue
		}
		paths = append(paths, filepath.Join(dir, ent.Name()))
	}
	existing := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		existing[filepath.ToSlash(p)] = struct{}{}
	}
	for key := range preloaded {
		normKey := filepath.ToSlash(key)
		keyDir := filepath.ToSlash(filepath.Dir(normKey))
		if keyDir == "." {
			keyDir = dirNorm
		}
		if keyDir == dirNorm {
			if _, ok := existing[normKey]; !ok {
				paths = append(paths, normKey)
			}
		}
	}
	if len(paths) == 0 {
		return nil, nil, nil, errModuleNotFound
	}
	sort.Strings(paths)
	if builder == nil {
		builder = ast.NewBuilder(ast.Hints{}, strs)
	}
	fileIDs := make([]ast.FileID, 0, len(paths))
	files := make([]*source.File, 0, len(paths))
	for _, p := range paths {
		normPath := filepath.ToSlash(p)
		if id, ok := preloaded[normPath]; ok && builder != nil {
			if existingID, okFile := fs.GetLatest(normPath); okFile {
				if file := fs.Get(existingID); file != nil {
					fileIDs = append(fileIDs, id)
					files = append(files, file)
					continue
				}
			}
		}
		fileID, err := fs.Load(p)
		if err != nil {
			return nil, nil, nil, err
		}
		file := fs.Get(fileID)
		if bag != nil {
			if errTok := diagnoseTokenize(file, bag); errTok != nil {
				return nil, nil, nil, errTok
			}
		}
		var parsed ast.FileID
		builder, parsed = diagnoseParseWithBuilder(fs, file, bag, builder)
		fileIDs = append(fileIDs, parsed)
		files = append(files, file)
	}
	return builder, fileIDs, files, nil
}

func analyzeDependencyModule(
	fs *source.FileSet,
	modulePath string,
	baseDir string,
	opts DiagnoseOptions,
	cache *ModuleCache,
	strs *source.Interner,
) (*moduleRecord, error) {
	dirPath, err := resolveModuleDir(modulePath, baseDir)
	if err != nil {
		if errors.Is(err, errModuleNotFound) {
			return nil, errModuleNotFound
		}
		return nil, err
	}
	bag := diag.NewBag(opts.MaxDiagnostics)
	builder, fileIDs, files, err := parseModuleDir(fs, dirPath, bag, strs, nil, nil)
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
	rec *moduleRecord,
	baseDir string,
	moduleExports map[string]*symbols.ModuleExports,
	typeInterner *types.Interner,
	opts DiagnoseOptions,
) *symbols.ModuleExports {
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
			ReuseDecls:    true,
		})
		res.ModuleFiles = moduleFiles
		if rec.Symbols == nil {
			rec.Symbols = make(map[ast.FileID]symbols.Result)
		}
		rec.Symbols[fileID] = res
		semaRes := sema.Check(rec.Builder, fileID, sema.Options{
			Reporter: reporter,
			Symbols:  &res,
			Exports:  moduleExports,
			Types:    typeInterner,
		})
		rec.Sema[fileID] = &semaRes
	}

	rec.Exports = symbols.CollectExports(rec.Builder, symbols.Result{
		Table:       table,
		FileScope:   moduleScope,
		ModuleFiles: moduleFiles,
		File:        rec.FileIDs[0],
	}, rec.Meta.Path)
	return rec.Exports
}

func collectModuleExports(
	records map[string]*moduleRecord,
	idx dag.ModuleIndex,
	topo *dag.Topo,
	baseDir string,
	rootPath string,
	typeInterner *types.Interner,
	opts DiagnoseOptions,
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
			if exp := resolveModuleRecord(rec, baseDir, exports, typeInterner, opts); exp != nil {
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

func ensureStdlibModules(
	fs *source.FileSet,
	records map[string]*moduleRecord,
	opts DiagnoseOptions,
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
		stdModuleCoreIntrinsics,
		stdModuleCoreBase,
		stdModuleCoreOption,
		stdModuleCoreResult,
	} {
		if _, ok := records[module]; ok {
			continue
		}
		rec, err := loadStdModule(fs, module, stdlibRoot, opts, cache, exports, typeInterner, strs)
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
	strs *source.Interner,
) (*moduleRecord, error) {
	if stdlibRoot == "" {
		return nil, errStdModuleMissing
	}
	filePath, ok := stdModuleFilePath(stdlibRoot, modulePath)
	if !ok {
		return nil, errStdModuleMissing
	}
	dirPath := filepath.Dir(filePath)
	bag := diag.NewBag(opts.MaxDiagnostics)
	builder, fileIDs, files, err := parseModuleDir(fs, dirPath, bag, strs, nil, nil)
	if err != nil {
		return nil, err
	}
	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fs, builder, fileIDs, stdlibRoot, reporter)
	if !ok && len(files) > 0 && files[0] != nil {
		meta = fallbackModuleMeta(files[0], stdlibRoot)
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
		meta, ok = buildModuleMeta(fs, builder, fileIDs, stdlibRoot, reporter)
		if !ok && len(files) > 0 && files[0] != nil {
			meta = fallbackModuleMeta(files[0], stdlibRoot)
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
	res := symbols.ResolveFile(builder, fileIDs[0], &symbols.ResolveOptions{
		Reporter:      reporter,
		Validate:      true,
		ModulePath:    meta.Path,
		FilePath:      files[0].Path,
		BaseDir:       stdlibRoot,
		ModuleExports: moduleExports,
	})
	markSymbolsBuiltin(&res)
	semaRes := sema.Check(builder, fileIDs[0], sema.Options{
		Reporter: reporter,
		Symbols:  &res,
		Exports:  moduleExports,
		Types:    typeInterner,
	})
	exports := symbols.CollectExports(builder, res, meta.Path)
	rec := &moduleRecord{
		Meta:     meta,
		Bag:      bag,
		Broken:   broken,
		FirstErr: firstErr,
		Builder:  builder,
		FileIDs:  fileIDs,
		Files:    files,
		Table:    res.Table,
		Sema: map[ast.FileID]*sema.Result{
			fileIDs[0]: &semaRes,
		},
		Symbols: map[ast.FileID]symbols.Result{
			fileIDs[0]: res,
		},
		Exports: exports,
	}
	return rec, nil
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
