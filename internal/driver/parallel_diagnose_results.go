package driver

import (
	"path/filepath"

	"surge/internal/ast"
	"surge/internal/source"
)

func publishParallelModuleResults(
	fileSet *source.FileSet,
	baseDir string,
	results []DiagnoseDirResult,
	opts *DiagnoseOptions,
	pathToIndex map[string]int,
	fileIDToIndex map[source.FileID]int,
	records map[string]*moduleRecord,
	normalizePath func(string) string,
) {
	for _, rec := range records {
		if rec == nil || rec.Builder == nil || len(rec.FileIDs) == 0 {
			continue
		}
		diagsByFile := splitDiagnosticsByFile(rec.Bag)
		for i, astFile := range rec.FileIDs {
			file := parallelRecordFile(fileSet, rec, i, astFile)
			if file == nil {
				continue
			}
			resIdx, ok := parallelResultIndex(file, baseDir, pathToIndex, fileIDToIndex, normalizePath)
			if !ok {
				continue
			}
			res := &results[resIdx]
			res.Path, res.FileID, res.ASTFile = file.Path, file.ID, astFile
			res.Builder = rec.Builder
			res.Bag = fileBagFromDiagnostics(diagsByFile[file.ID], opts.MaxDiagnostics)
			res.Symbols = nil
			if sym, ok := rec.Symbols[astFile]; ok {
				symCopy := sym
				res.Symbols = &symCopy
			}
			res.Sema = rec.Sema[astFile]
		}
	}
}

func parallelRecordFile(fileSet *source.FileSet, rec *moduleRecord, index int, astFile ast.FileID) *source.File {
	var file *source.File
	if index < len(rec.Files) {
		file = rec.Files[index]
	}
	if file == nil {
		if node := rec.Builder.Files.Get(astFile); node != nil && fileSet.HasFile(node.Span.File) {
			file = fileSet.Get(node.Span.File)
		}
	}
	return file
}

func parallelResultIndex(
	file *source.File,
	baseDir string,
	pathToIndex map[string]int,
	fileIDToIndex map[source.FileID]int,
	normalizePath func(string) string,
) (int, bool) {
	if index, ok := pathToIndex[normalizePath(file.Path)]; ok {
		return index, true
	}
	if absPath, err := filepath.Abs(file.Path); err == nil {
		if index, ok := pathToIndex[normalizePath(absPath)]; ok {
			return index, true
		}
	}
	if baseDir != "" {
		if relPath, err := source.RelativePath(file.Path, baseDir); err == nil {
			if index, ok := pathToIndex[normalizePath(relPath)]; ok {
				return index, true
			}
		}
	}
	index, ok := fileIDToIndex[file.ID]
	return index, ok
}
