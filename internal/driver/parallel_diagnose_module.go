package driver

import (
	"context"
	"errors"
	"path/filepath"
	"sort"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/parser"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// enrichModuleResults reruns symbol/semantic resolution for directories that
// declare a multi-file module (pragma module/binary). This ensures all files in
// the module share one scope when diagnosing a directory.
func enrichModuleResults(ctx context.Context, _ string, fileSet *source.FileSet, results []DiagnoseDirResult, opts DiagnoseOptions) error {
	if fileSet == nil {
		return nil
	}

	moduleDirs := make(map[string]struct{})
	pathToIndex := make(map[string]int, len(results))

	for i := range results {
		res := &results[i]

		path := res.Path
		if path == "" && res.FileID != 0 {
			if file := fileSet.Get(res.FileID); file != nil {
				path = file.Path
			}
		}
		if path == "" {
			continue
		}

		pathKey := filepath.ToSlash(path)
		pathToIndex[pathKey] = i
		dirKey := filepath.ToSlash(filepath.Dir(pathKey))

		if res.Builder != nil && hasModulePragma(res.Builder, res.ASTFile) {
			moduleDirs[dirKey] = struct{}{}
		}
	}

	if len(moduleDirs) == 0 {
		return nil
	}

	typeInterner := types.NewInterner()
	moduleExports := make(map[string]*symbols.ModuleExports)

	dirKeys := make([]string, 0, len(moduleDirs))
	for dirKey := range moduleDirs {
		dirKeys = append(dirKeys, dirKey)
	}
	sort.Strings(dirKeys)

	for _, dirKey := range dirKeys {
		moduleRes, err := diagnoseModuleDirectory(ctx, dirKey, fileSet, opts, typeInterner, moduleExports)
		if err != nil {
			return err
		}
		for pathKey, res := range moduleRes {
			idx, ok := pathToIndex[pathKey]
			if !ok {
				continue
			}
			results[idx].Bag = res.Bag
			results[idx].FileID = res.FileID
			results[idx].ASTFile = res.ASTFile
			results[idx].Builder = res.Builder
			results[idx].Symbols = res.Symbols
			results[idx].Sema = res.Sema
		}
	}

	return nil
}

type moduleFileResult struct {
	Bag     *diag.Bag
	FileID  source.FileID
	ASTFile ast.FileID
	Symbols *symbols.Result
	Sema    *sema.Result
	Builder *ast.Builder
}

// diagnoseModuleDirectory parses and resolves a single directory that declares
// a multi-file module. It returns per-file diagnostics and semantic results so
// that directory-level diagnosis can honor shared module scope.
func diagnoseModuleDirectory(
	ctx context.Context,
	dir string,
	fileSet *source.FileSet,
	opts DiagnoseOptions,
	typeInterner *types.Interner,
	moduleExports map[string]*symbols.ModuleExports,
) (map[string]moduleFileResult, error) {
	if fileSet == nil {
		return nil, nil
	}

	bag := diag.NewBag(opts.MaxDiagnostics)
	builder, fileIDs, files, err := parseModuleDir(ctx, fileSet, dir, bag, source.NewInterner(), nil, nil, parser.DirectiveModeOff)
	if err != nil {
		if errors.Is(err, errModuleNotFound) {
			return nil, nil
		}
		return nil, err
	}

	reporter := &diag.BagReporter{Bag: bag}
	meta, ok := buildModuleMeta(fileSet, builder, fileIDs, fileSet.BaseDir(), reporter)
	if !ok && len(files) > 0 && files[0] != nil {
		meta = fallbackModuleMeta(files[0], fileSet.BaseDir())
	}
	if meta == nil || !meta.HasModulePragma {
		return nil, nil
	}

	rec := &moduleRecord{
		Meta:    meta,
		Bag:     bag,
		Builder: builder,
		FileIDs: fileIDs,
		Files:   files,
	}

	if typeInterner == nil {
		typeInterner = types.NewInterner()
	}
	if moduleExports == nil {
		moduleExports = make(map[string]*symbols.ModuleExports)
	}
	if exp := resolveModuleRecord(ctx, rec, fileSet.BaseDir(), moduleExports, typeInterner, opts); exp != nil {
		moduleExports[normalizeExportsKey(meta.Path)] = exp
	}

	diagsByFile := make(map[source.FileID][]*diag.Diagnostic)
	for _, d := range bag.Items() {
		diagsByFile[d.Primary.File] = append(diagsByFile[d.Primary.File], d)
	}

	out := make(map[string]moduleFileResult, len(fileIDs))
	for i, fileID := range fileIDs {
		file := files[i]
		fileBag := diag.NewBag(opts.MaxDiagnostics)
		if ds := diagsByFile[file.ID]; len(ds) > 0 {
			for _, d := range ds {
				fileBag.Add(d)
			}
		}

		var symRes *symbols.Result
		if rec.Symbols != nil {
			if sym, ok := rec.Symbols[fileID]; ok {
				symCopy := sym
				symRes = &symCopy
			}
		}
		var semRes *sema.Result
		if rec.Sema != nil {
			semRes = rec.Sema[fileID]
		}

		out[filepath.ToSlash(file.Path)] = moduleFileResult{
			Bag:     fileBag,
			FileID:  file.ID,
			ASTFile: fileID,
			Symbols: symRes,
			Sema:    semRes,
			Builder: builder,
		}
	}

	return out, nil
}

func hasModulePragma(builder *ast.Builder, fileID ast.FileID) bool {
	if builder == nil || fileID == ast.NoFileID {
		return false
	}
	node := builder.Files.Get(fileID)
	if node == nil || len(node.Pragma.Entries) == 0 {
		return false
	}
	interner := builder.StringsInterner
	for _, entry := range node.Pragma.Entries {
		name, ok := interner.Lookup(entry.Name)
		if !ok {
			continue
		}
		if name == "module" || name == "binary" {
			return true
		}
	}
	return false
}
