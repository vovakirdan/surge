package driver

import (
	"context"
	"fmt"

	"surge/internal/ast"
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
	pass, err := enrichModuleResults(ctx, baseDir, fileSet, results, opts)
	if err != nil {
		return err
	}
	return finalizeParallelFileResults(ctx, fileSet, results, pass)
}

func finalizeParallelFileResults(ctx context.Context, fileSet *source.FileSet, results []DiagnoseDirResult, passes ...returnOriginPass) error {
	var pass returnOriginPass
	if len(passes) > 0 {
		pass = passes[0]
	}
	for i := range results {
		result := &results[i]
		if result.Bag == nil {
			return fmt.Errorf("%s return origins: missing diagnostic bag", result.Path)
		}
		if result.Sema == nil || result.Symbols == nil {
			if result.Bag.HasErrors() {
				return returnOriginBagRefusal(result.Path, result.Bag)
			}
			return fmt.Errorf("%s return origins: missing original typed source artifacts", result.Path)
		}
		if outcome, carried := pass[result.Sema]; carried {
			if !outcome.matches(result.Sema) {
				return fmt.Errorf("%s return-origin authority changed after module finalization", result.Path)
			}
			if err := requireReturnOriginPublication(outcome, result.Bag); err != nil {
				return err
			}
			continue
		}
		if result.Bag.HasErrors() {
			return returnOriginBagRefusal(result.Path, result.Bag)
		}
		if pass != nil {
			return fmt.Errorf("%s has no return-origin outcome from its module pass", result.Path)
		}
		file, err := parallelResultOwner(fileSet, result)
		if err != nil {
			return fmt.Errorf("%s: %w", result.Path, err)
		}
		diagnosed := &DiagnoseResult{
			FileSet: fileSet,
			File:    file,
			FileID:  result.ASTFile,
			Builder: result.Builder,
			Bag:     result.Bag,
			Symbols: result.Symbols,
			Sema:    result.Sema,
		}
		if _, err := finalizeDiagnoseResult(ctx, diagnosed); err != nil {
			return fmt.Errorf("%s finalization: %w", result.Path, err)
		}
	}
	return nil
}

// parallelResultOwner answers which source file owns one retained result's
// finalization decisions.
//
// DiagnoseDirResult names its file twice, in two different id spaces: ASTFile
// in the AST space and FileID in the source space. Only ASTFile is tied to the
// semantic result being finalized, so its span settles the owning source
// identity here — the same span the module-record publication path reads — and
// both DiagnoseResult.File and DiagnoseResult.FileID derive from it. Deriving
// the two fields separately let them name different files, and a result whose
// source-space field had drifted then reached publication with no owner at all.
func parallelResultOwner(fileSet *source.FileSet, result *DiagnoseDirResult) (*source.File, error) {
	if result.Builder == nil || result.ASTFile == ast.NoFileID {
		return nil, fmt.Errorf("retained semantic result names no AST file to identify its source")
	}
	node := result.Builder.Files.Get(result.ASTFile)
	if node == nil {
		return nil, fmt.Errorf("AST file %d is absent from its own builder", result.ASTFile)
	}
	if fileSet == nil || !fileSet.HasFile(node.Span.File) {
		return nil, fmt.Errorf(
			"AST file %d names source file %d, which this file set does not hold",
			result.ASTFile, node.Span.File,
		)
	}
	return fileSet.Get(node.Span.File), nil
}

// finalizeParallelModuleRecords finalizes one module-level authority per root
// table and attaches it to every retained per-file semantic result.
func finalizeParallelModuleRecords(
	ctx context.Context,
	fileSet *source.FileSet,
	paths []string,
	records map[string]*moduleRecord,
) (returnOriginPass, error) {
	pass := make(returnOriginPass)
	reporters := make(returnOriginReporters)
	for _, modulePath := range paths {
		rec := records[modulePath]
		if rec == nil || rec.Bag == nil {
			return nil, fmt.Errorf("%s return origins: missing module or diagnostic bag", modulePath)
		}
		if rec.Bag.HasErrors() {
			return nil, returnOriginBagRefusal(modulePath, rec.Bag)
		}
		aggregate, aggregateSymbols, _ := parallelModuleAuthority(rec)
		if aggregate == nil || aggregateSymbols == nil {
			return nil, fmt.Errorf("%s return origins: missing module semantic authority", modulePath)
		}
	}
	publicationSeed := &DiagnoseResult{FileSet: fileSet, moduleRecords: records}
	publicationIndex, err := buildFinalizationPublicationIndex(publicationSeed)
	if err != nil {
		return nil, err
	}
	// One authority for every module below, built before the loop for the same
	// reason the publication index is: what a type can do is a property of the
	// program, not of whichever module happens to be finalizing when the
	// question is asked.
	capabilities, err := wholeProgramCapabilityAuthority(records)
	if err != nil {
		return nil, fmt.Errorf("capability classification: %w", err)
	}
	finalized := make(map[*moduleRecord]struct{}, len(records))
	for _, modulePath := range paths {
		rec := records[modulePath]
		if _, done := finalized[rec]; done {
			continue
		}
		finalized[rec] = struct{}{}
		aggregate, aggregateSymbols, aggregateFile := parallelModuleAuthority(rec)
		if aggregate == nil || aggregateSymbols == nil {
			return nil, fmt.Errorf("%s return origins: missing module semantic authority", modulePath)
		}
		diagnosed := &DiagnoseResult{
			FileSet: fileSet, File: aggregateFile, Bag: rec.Bag,
			Symbols: aggregateSymbols, Sema: aggregate,
			rootRecord: rec, moduleRecords: records, finalizationIndex: publicationIndex,
			wholeProgramAuthority: true,
		}
		if err := mergeTypeAttrFactsFromRecords(diagnosed); err != nil {
			return nil, fmt.Errorf("%s type attribute facts: %w", modulePath, err)
		}
		aggregate.Capabilities = capabilities
		if err := FinalizeInstantiationClosure(ctx, diagnosed, 64); err != nil {
			return nil, fmt.Errorf("%s instantiation closure: %w", modulePath, err)
		}
		if err := reachRequiredValueOperations(diagnosed); err != nil {
			return nil, fmt.Errorf("%s required value operations: %w", modulePath, err)
		}
		for _, astFile := range rec.FileIDs {
			if fileSema := rec.Sema[astFile]; fileSema != nil && fileSema != aggregate {
				sema.CopyInstantiationAuthority(fileSema, aggregate)
			}
		}
		if err := publishFinalizationDecisions(diagnosed); err != nil {
			return nil, fmt.Errorf("%s finalization publication: %w", modulePath, err)
		}
		outcome, err := analyzeReturnOriginResult(ctx, diagnosed, nil, reporters)
		if err != nil {
			return nil, err
		}
		for _, astFile := range rec.FileIDs {
			if checked := rec.Sema[astFile]; checked != nil {
				pass[checked] = returnOriginOutcome{analysis: outcome.analysis, identity: checked.InstantiationIdentity, closure: checked.InstantiationClosure}
			}
		}
	}
	return pass, nil
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
