package driver

import (
	"context"
	"fmt"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

func prepareParallelFileResults(
	ctx context.Context,
	baseDir string,
	fileSet *source.FileSet,
	results []DiagnoseDirResult,
	opts *DiagnoseOptions,
) error {
	if err := enrichModuleResults(ctx, baseDir, fileSet, results, opts); err != nil {
		return err
	}
	return finalizeParallelFileResults(ctx, fileSet, results)
}

func finalizeParallelFileResults(ctx context.Context, fileSet *source.FileSet, results []DiagnoseDirResult) error {
	for i := range results {
		result := &results[i]
		if result.Sema == nil || result.Symbols == nil || result.Bag == nil || result.Bag.HasErrors() {
			continue
		}
		var file *source.File
		if fileSet.HasFile(result.FileID) {
			file = fileSet.Get(result.FileID)
		}
		diagnosed := &DiagnoseResult{
			FileSet: fileSet,
			File:    file,
			FileID:  result.ASTFile,
			Bag:     result.Bag,
			Symbols: result.Symbols,
			Sema:    result.Sema,
		}
		if err := FinalizeInstantiationClosure(ctx, diagnosed, 64); err != nil {
			return fmt.Errorf("%s instantiation closure: %w", result.Path, err)
		}
	}
	return nil
}

// finalizeParallelModuleRecords finalizes one module-level authority per root
// table and attaches it to every retained per-file semantic result.
func finalizeParallelModuleRecords(
	ctx context.Context,
	fileSet *source.FileSet,
	paths []string,
	records map[string]*moduleRecord,
) error {
	publicationSeed := &DiagnoseResult{FileSet: fileSet, moduleRecords: records}
	publicationIndex, err := buildFinalizationPublicationIndex(publicationSeed)
	if err != nil {
		return err
	}
	finalized := make(map[*moduleRecord]struct{}, len(records))
	for _, modulePath := range paths {
		rec := records[modulePath]
		if rec == nil || rec.Bag == nil || rec.Bag.HasErrors() {
			continue
		}
		if _, done := finalized[rec]; done {
			continue
		}
		finalized[rec] = struct{}{}
		aggregate, aggregateSymbols, aggregateFile := parallelModuleAuthority(rec)
		if aggregate == nil || aggregateSymbols == nil {
			continue
		}
		diagnosed := &DiagnoseResult{
			FileSet: fileSet, File: aggregateFile, Bag: rec.Bag,
			Symbols: aggregateSymbols, Sema: aggregate,
			rootRecord: rec, moduleRecords: records, finalizationIndex: publicationIndex,
		}
		if err := FinalizeInstantiationClosure(ctx, diagnosed, 64); err != nil {
			return fmt.Errorf("%s instantiation closure: %w", modulePath, err)
		}
		for _, astFile := range rec.FileIDs {
			if fileSema := rec.Sema[astFile]; fileSema != nil && fileSema != aggregate {
				sema.CopyInstantiationAuthority(fileSema, aggregate)
			}
		}
		if err := publishFinalizationDecisions(diagnosed); err != nil {
			return fmt.Errorf("%s finalization publication: %w", modulePath, err)
		}
	}
	return nil
}

func parallelModuleAuthority(rec *moduleRecord) (semaResult *sema.Result, symbolsResult *symbols.Result, sourceFile *source.File) {
	for i, astFile := range rec.FileIDs {
		aggregate := rec.Sema[astFile]
		sym, ok := rec.Symbols[astFile]
		if aggregate == nil || !ok {
			continue
		}
		var file *source.File
		if i < len(rec.Files) {
			file = rec.Files[i]
		}
		return aggregate, &sym, file
	}
	return nil, nil, nil
}
