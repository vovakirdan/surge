package driver

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/project"
	"surge/internal/project/dag"
	"surge/internal/source"
	"surge/internal/types"
)

func resolveDirModuleGraph(ctx context.Context, fileSet *source.FileSet, results []DiagnoseDirResult, opts *DiagnoseOptions) error {
	if fileSet == nil || len(results) == 0 {
		return nil
	}
	if opts == nil {
		opts = &DiagnoseOptions{}
	}
	baseDir := fileSet.BaseDir()
	stdlibRoot := detectStdlibRootFrom(baseDir)

	sharedStrings := source.NewInterner()
	typeInterner := types.NewInterner()
	cache := NewModuleCache(len(results) * 2)

	pathToIndex := make(map[string]int, len(results))
	for i := range results {
		if resPath := resultPath(fileSet, &results[i]); resPath != "" {
			pathToIndex[filepath.ToSlash(resPath)] = i
		}
	}

	moduleDirs := make(map[string]struct{})
	for i := range results {
		res := &results[i]
		if res.Builder == nil || !res.ASTFile.IsValid() {
			continue
		}
		if !hasModulePragma(res.Builder, res.ASTFile) {
			continue
		}
		resPath := resultPath(fileSet, res)
		if resPath == "" {
			continue
		}
		dirKey := filepath.ToSlash(filepath.Dir(resPath))
		moduleDirs[dirKey] = struct{}{}
	}

	handledFiles := make(map[string]struct{})
	records := make(map[string]*moduleRecord, len(results))

	dirKeys := make([]string, 0, len(moduleDirs))
	for dirKey := range moduleDirs {
		dirKeys = append(dirKeys, dirKey)
	}
	sort.Strings(dirKeys)

	for _, dirKey := range dirKeys {
		bag := diag.NewBag(opts.MaxDiagnostics)
		builder, fileIDs, files, err := parseModuleDir(ctx, fileSet, dirKey, bag, sharedStrings, nil, nil, opts.DirectiveMode)
		if err != nil {
			if errors.Is(err, errModuleNotFound) {
				continue
			}
			return err
		}
		reporter := &diag.BagReporter{Bag: bag}
		meta, ok := buildModuleMeta(fileSet, builder, fileIDs, baseDir, reporter)
		if !ok && len(files) > 0 && files[0] != nil {
			meta = fallbackModuleMeta(files[0], baseDir)
		}
		if meta == nil || !meta.HasModulePragma {
			continue
		}
		rec := &moduleRecord{
			Meta:    meta,
			Bag:     bag,
			Builder: builder,
			FileIDs: fileIDs,
			Files:   files,
		}
		rec.Broken, rec.FirstErr = moduleStatus(bag)
		records[meta.Path] = rec
		cache.Put(meta, rec.Broken, rec.FirstErr)
		for _, file := range files {
			if file == nil {
				continue
			}
			handledFiles[filepath.ToSlash(file.Path)] = struct{}{}
		}
	}

	for i := range results {
		res := &results[i]
		resPath := resultPath(fileSet, res)
		if resPath == "" {
			continue
		}
		pathKey := filepath.ToSlash(resPath)
		if _, ok := handledFiles[pathKey]; ok {
			continue
		}
		file := resultFile(fileSet, res, resPath)
		if file == nil {
			continue
		}
		bag := diag.NewBag(opts.MaxDiagnostics)
		diagnoseTokenize(file, bag)
		builder, astFile := diagnoseParseWithStrings(ctx, fileSet, file, bag, sharedStrings, opts.DirectiveMode)
		reporter := &diag.BagReporter{Bag: bag}
		meta, ok := buildModuleMeta(fileSet, builder, []ast.FileID{astFile}, baseDir, reporter)
		if !ok {
			meta = fallbackModuleMeta(file, baseDir)
		}
		if meta == nil {
			continue
		}
		rec := &moduleRecord{
			Meta:    meta,
			Bag:     bag,
			Builder: builder,
			FileIDs: []ast.FileID{astFile},
			Files:   []*source.File{file},
		}
		rec.Broken, rec.FirstErr = moduleStatus(bag)
		if _, exists := records[meta.Path]; !exists {
			records[meta.Path] = rec
		}
		cache.Put(meta, rec.Broken, rec.FirstErr)
	}

	if len(records) == 0 {
		return nil
	}

	missing := make(map[string]struct{})
	processedImports := make(map[string]struct{})
	aliasExports := make(map[string]string)
	queue := make([]string, 0, len(records))
	seen := make(map[string]struct{})
	for modulePath := range records {
		queue = append(queue, modulePath)
	}
	sort.Strings(queue)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		seen[cur] = struct{}{}
		rec := records[cur]
		if rec == nil || rec.Meta == nil {
			continue
		}
		for i := range rec.Meta.Imports {
			imp := rec.Meta.Imports[i]
			if imp.Path == "" {
				continue
			}
			if _, ok := processedImports[imp.Path]; ok {
				continue
			}
			if _, ok := records[imp.Path]; ok {
				continue
			}
			if _, miss := missing[imp.Path]; miss {
				continue
			}
			depRec, err := analyzeDependencyModule(ctx, fileSet, imp.Path, baseDir, stdlibRoot, opts, cache, sharedStrings)
			if err != nil {
				if errors.Is(err, errModuleNotFound) {
					missing[imp.Path] = struct{}{}
					continue
				}
				return err
			}
			importedPath := normalizeExportsKey(imp.Path)
			actualPath := normalizeExportsKey(depRec.Meta.Path)
			if importedPath != "" && actualPath != "" && importedPath != actualPath {
				if moduleHasExplicitName(depRec.Meta) && rec.Bag != nil && path.Base(importedPath) != depRec.Meta.Name {
					reporter := &diag.BagReporter{Bag: rec.Bag}
					msg := fmt.Sprintf("module is named %q, not %q", depRec.Meta.Path, imp.Path)
					if b := diag.ReportError(reporter, diag.ProjWrongModuleNameInImport, imp.Span, msg); b != nil {
						fixID := fix.MakeFixID(diag.ProjWrongModuleNameInImport, imp.Span)
						b.WithFixSuggestion(fix.ReplaceSpan(
							"update import path",
							imp.Span,
							depRec.Meta.Path,
							imp.Path,
							fix.WithID(fixID),
							fix.WithKind(diag.FixKindQuickFix),
							fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
						))
						b.Emit()
					}
				}
				aliasExports[importedPath] = depRec.Meta.Path
				rec.Meta.Imports[i].Path = depRec.Meta.Path
			}
			processedImports[imp.Path] = struct{}{}
			records[depRec.Meta.Path] = depRec
			if _, ok := seen[depRec.Meta.Path]; !ok {
				queue = append(queue, depRec.Meta.Path)
			}
		}
	}

	if err := ensureStdlibModules(ctx, fileSet, records, opts, cache, stdlibRoot, typeInterner, sharedStrings); err != nil {
		return err
	}

	paths := make([]string, 0, len(records))
	for p := range records {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	metas := make([]*project.ModuleMeta, 0, len(paths))
	nodes := make([]*dag.ModuleNode, 0, len(paths))
	for _, p := range paths {
		rec := records[p]
		if rec == nil || rec.Meta == nil {
			continue
		}
		metas = append(metas, rec.Meta)
		nodes = append(nodes, &dag.ModuleNode{
			Meta:     rec.Meta,
			Reporter: &diag.BagReporter{Bag: rec.Bag},
			Broken:   rec.Broken,
			FirstErr: rec.FirstErr,
		})
	}
	idx := dag.BuildIndex(metas)
	graph, slots := dag.BuildGraph(idx, nodes)
	topo := dag.ToposortKahn(graph)
	dag.ReportCycles(idx, slots, topo)
	ComputeModuleHashes(idx, graph, slots, topo)
	for i := range slots {
		reporter, ok := slots[i].Reporter.(*diag.BagReporter)
		if !ok || reporter.Bag == nil {
			continue
		}
		brokenNow, firstErrNow := moduleStatus(reporter.Bag)
		if brokenNow {
			slots[i].Broken = true
			if slots[i].FirstErr == nil && firstErrNow != nil {
				slots[i].FirstErr = firstErrNow
			}
		}
	}
	dag.ReportBrokenDeps(idx, slots)

	exports := collectModuleExports(ctx, records, idx, topo, baseDir, "", typeInterner, opts)
	for alias, target := range aliasExports {
		if exp, ok := exports[normalizeExportsKey(target)]; ok {
			exports[normalizeExportsKey(alias)] = exp
		}
	}
	if opts.ExportsOut != nil {
		*opts.ExportsOut = exports
	}

	for _, rec := range records {
		if rec == nil || rec.Builder == nil || len(rec.FileIDs) == 0 {
			continue
		}
		diagsByFile := splitDiagnosticsByFile(rec.Bag)
		for i, astFile := range rec.FileIDs {
			var file *source.File
			if i < len(rec.Files) {
				file = rec.Files[i]
			}
			if file == nil && rec.Builder != nil {
				if node := rec.Builder.Files.Get(astFile); node != nil && fileSet.HasFile(node.Span.File) {
					file = fileSet.Get(node.Span.File)
				}
			}
			if file == nil {
				continue
			}
			resIdx, ok := pathToIndex[filepath.ToSlash(file.Path)]
			if !ok {
				continue
			}
			res := &results[resIdx]
			res.Path = file.Path
			res.FileID = file.ID
			res.ASTFile = astFile
			res.Builder = rec.Builder
			res.Bag = fileBagFromDiagnostics(diagsByFile[file.ID], opts.MaxDiagnostics)
			res.Symbols = nil
			if rec.Symbols != nil {
				if sym, ok := rec.Symbols[astFile]; ok {
					symCopy := sym
					res.Symbols = &symCopy
				}
			}
			res.Sema = nil
			if rec.Sema != nil {
				res.Sema = rec.Sema[astFile]
			}
		}
	}

	return nil
}

func resultPath(fileSet *source.FileSet, res *DiagnoseDirResult) string {
	if res == nil {
		return ""
	}
	if res.Path != "" {
		return res.Path
	}
	if fileSet == nil || res.FileID == 0 {
		return ""
	}
	if !fileSet.HasFile(res.FileID) {
		return ""
	}
	file := fileSet.Get(res.FileID)
	if file == nil {
		return ""
	}
	return file.Path
}

func resultFile(fileSet *source.FileSet, res *DiagnoseDirResult, filePath string) *source.File {
	if fileSet == nil {
		return nil
	}
	if res != nil && res.FileID != 0 && fileSet.HasFile(res.FileID) {
		return fileSet.Get(res.FileID)
	}
	if filePath == "" {
		return nil
	}
	if id, ok := fileSet.GetLatest(filePath); ok {
		return fileSet.Get(id)
	}
	return nil
}

func splitDiagnosticsByFile(bag *diag.Bag) map[source.FileID][]*diag.Diagnostic {
	if bag == nil {
		return nil
	}
	out := make(map[source.FileID][]*diag.Diagnostic)
	for _, d := range bag.Items() {
		if d == nil {
			continue
		}
		out[d.Primary.File] = append(out[d.Primary.File], d)
	}
	return out
}

func fileBagFromDiagnostics(diags []*diag.Diagnostic, maxDiagnostics int) *diag.Bag {
	bag := diag.NewBag(maxDiagnostics)
	for _, d := range diags {
		bag.Add(d)
	}
	return bag
}
