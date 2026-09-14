package driver

import (
	"context"
	"fmt"
	"sort"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// A pass travels only from directory module finalization to its file wrappers.
// The copied identity and closure must still be the ones the pass answered.
type returnOriginPass map[*sema.Result]returnOriginOutcome

type returnOriginOutcome struct {
	analysis *sema.ReturnOriginAnalysis
	identity *sema.InstantiationIdentity
	closure  *sema.InstantiationClosure
}

func (o returnOriginOutcome) matches(result *sema.Result) bool {
	return result != nil && o.analysis != nil && o.identity != nil && o.closure != nil &&
		o.identity == result.InstantiationIdentity && o.closure == result.InstantiationClosure
}

type returnOriginUnfinishedError struct {
	Pending     []sema.ReturnOriginPending
	Diagnostics []diag.Diagnostic
}

func (e *returnOriginUnfinishedError) Error() string {
	return fmt.Sprintf("return-origin analysis unfinished: %+v", e.Pending)
}

type returnOriginUnpublishedError struct {
	Diagnostics []diag.Diagnostic
}

func (e *returnOriginUnpublishedError) Error() string {
	return fmt.Sprintf("return-origin refusal was not represented in the returned diagnostics: %+v", e.Diagnostics)
}

func (e *returnOriginUnpublishedError) Unwrap() error { return ErrDiagnosticsReported }

// Directory APIs return no result bags on error. Keep the actual prerequisite
// diagnostics in the existing evidence-carrying error instead of a bare status.
func returnOriginBagRefusal(path string, bag *diag.Bag) error {
	unpublished := &returnOriginUnpublishedError{}
	for _, diagnostic := range bag.Items() {
		if diagnostic != nil {
			unpublished.Diagnostics = append(unpublished.Diagnostics, *diagnostic)
		}
	}
	return fmt.Errorf("%s: invalid semantic prerequisites: %w", path, unpublished)
}

func returnOriginVerdict(analysis *sema.ReturnOriginAnalysis) (bool, error) {
	if analysis == nil {
		return true, fmt.Errorf("return-origin analysis did not run")
	}
	if !analysis.Complete() {
		return true, &returnOriginUnfinishedError{Pending: append([]sema.ReturnOriginPending(nil), analysis.Pending...),
			Diagnostics: append([]diag.Diagnostic(nil), analysis.Diagnostics...)}
	}
	for _, diagnostic := range analysis.Diagnostics {
		if diagnostic.Severity >= diag.SevError {
			return true, nil
		}
	}
	return false, nil
}

func requireReturnOriginPublication(outcome returnOriginOutcome, bag *diag.Bag) error {
	blocked, err := returnOriginVerdict(outcome.analysis)
	if err != nil {
		return err
	}
	if blocked && (bag == nil || !bag.HasErrors()) {
		return &returnOriginUnpublishedError{Diagnostics: append([]diag.Diagnostic(nil), outcome.analysis.Diagnostics...)}
	}
	if bag == nil {
		return fmt.Errorf("return origins: missing diagnostic publication bag")
	}
	if !blocked && bag.HasErrors() {
		return returnOriginBagRefusal("finalized source", bag)
	}
	return nil
}

func requireParallelReturnOriginPublication(pass returnOriginPass, results []DiagnoseDirResult) error {
	for _, result := range results {
		outcome, ok := pass[result.Sema]
		if !ok {
			if result.Bag != nil && result.Bag.HasErrors() {
				return returnOriginBagRefusal(result.Path, result.Bag)
			}
			return fmt.Errorf("%s has no return-origin outcome from its module pass", result.Path)
		}
		if !outcome.matches(result.Sema) {
			return fmt.Errorf("%s return-origin authority changed after module finalization", result.Path)
		}
		if err := requireReturnOriginPublication(outcome, result.Bag); err != nil {
			return err
		}
	}
	return nil
}

func hasReturnOriginRefusal(bag *diag.Bag) bool {
	if bag != nil {
		for _, diagnostic := range bag.Items() {
			if diagnostic != nil && diagnostic.Severity >= diag.SevError && diagnostic.Code == diag.SemaBorrowEscapesReturn {
				return true
			}
		}
	}
	return false
}

type returnOriginInputs struct {
	units []sema.ReturnOriginUnit
	bags  map[source.FileID]*diag.Bag
}

func collectReturnOriginUnits(res *DiagnoseResult) (returnOriginInputs, error) {
	inputs := returnOriginInputs{bags: make(map[source.FileID]*diag.Bag)}
	if res == nil || res.Sema == nil || res.Sema.TypeInterner == nil || res.FileSet == nil {
		return inputs, fmt.Errorf("return origins: missing diagnosed source authority")
	}
	resolveSource := canonicalInstantiationSourceResolver(res)
	seen := make(map[string]struct{})
	add := func(rec *moduleRecord, builder *ast.Builder, fileID ast.FileID, checked *sema.Result, resolved *symbols.Result, bag *diag.Bag) error {
		if builder == nil || !fileID.IsValid() || checked == nil || checked.TypeInterner != res.Sema.TypeInterner || resolved == nil || bag == nil {
			return fmt.Errorf("return origins: incomplete original per-file artifacts for AST file %d", fileID)
		}
		if uint32(fileID) > builder.Files.Arena.Len() {
			return fmt.Errorf("return origins: missing owning AST file %d", fileID)
		}
		file := builder.Files.Get(fileID)
		if file == nil {
			return fmt.Errorf("return origins: missing owning AST file %d", fileID)
		}
		key, err := resolveSource(file.Span.File)
		if err != nil {
			return err
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("return origins: conflicting owning units for %q", key)
		}
		publication, err := returnOriginPublication(res, rec, key)
		if err != nil {
			return err
		}
		seen[key] = struct{}{}
		inputs.bags[file.Span.File] = bag
		inputs.units = append(inputs.units, sema.ReturnOriginUnit{
			Builder: builder, FileID: fileID, Sema: checked, Symbols: resolved,
			SourceKey: key, Publication: publication,
		})
		return nil
	}
	for _, rec := range finalizationPublicationRecords(res) {
		for _, fileID := range rec.FileIDs {
			resolved, exists := rec.Symbols[fileID]
			if !exists {
				return inputs, fmt.Errorf("return origins: missing original symbols for AST file %d", fileID)
			}
			if err := add(rec, rec.Builder, fileID, rec.Sema[fileID], &resolved, rec.Bag); err != nil {
				return inputs, err
			}
		}
	}
	if res.rootRecord == nil {
		if err := add(nil, res.Builder, res.FileID, res.Sema, res.Symbols, res.Bag); err != nil {
			return inputs, err
		}
	}
	if len(inputs.units) == 0 {
		return inputs, fmt.Errorf("return origins: diagnosed program has no owning source units")
	}
	sort.Slice(inputs.units, func(i, j int) bool { return inputs.units[i].SourceKey < inputs.units[j].SourceKey })
	return inputs, nil
}

func returnOriginPublication(res *DiagnoseResult, rec *moduleRecord, key string) (sema.FinalizationPublication, error) {
	callables, captured := res.finalizationIndex[rec]
	if !captured {
		return sema.FinalizationPublication{}, fmt.Errorf("return origins: no original callable identity snapshot for %q", key)
	}
	publication := sema.FinalizationPublication{
		SourceKey: key, LocalCallables: append([]sema.FinalizationCallableIdentity(nil), callables...),
	}
	if rec == nil || rec == res.rootRecord {
		return publication, nil
	}
	if res.Symbols == nil || res.Symbols.Table == nil {
		return publication, fmt.Errorf("return origins: imported unit %q has no root symbol authority", key)
	}
	mapping, ok := cachedInstantiationSymbolRemap(rec, res.Symbols.Table)
	if !ok {
		mapping = buildModuleSymbolRemap(res.Symbols, rec)
		if rec.Meta != nil && isCoreModulePath(rec.Meta.Path) {
			if core := buildCoreSymbolRemap(res.Symbols, rec); len(core) > 0 {
				mapping = core
			}
		}
	}
	publication.RootToLocalSymbols = invertFinalizationSymbolRemap(mapping)
	return publication, nil
}

// Reporters are shared only within one publication pass. Each diagnostic goes
// to the source's module bag; file publication already splits those bags.
type returnOriginReporters map[*diag.Bag]*diag.DedupReporter

func publishReturnOriginDiagnostics(analysis *sema.ReturnOriginAnalysis, inputs returnOriginInputs, rootBag *diag.Bag, reporters returnOriginReporters) error {
	for i := range analysis.Diagnostics {
		diagnostic := &analysis.Diagnostics[i]
		bag := inputs.bags[diagnostic.Primary.File]
		if bag == nil {
			return fmt.Errorf("return origins: diagnostic names no owning source unit: %v", diagnostic.Primary)
		}
		// A single-file API exposes one aggregate bag, including imported
		// refusals. Directory publication instead uses each owning module bag.
		if rootBag != nil {
			bag = rootBag
		}
		if bag.Len() >= int(bag.Cap()) {
			duplicate := false
			for _, old := range bag.Items() {
				if old != nil && old.Code == diagnostic.Code && old.Severity == diagnostic.Severity && old.Primary == diagnostic.Primary && old.Message == diagnostic.Message {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			return &returnOriginUnpublishedError{Diagnostics: append([]diag.Diagnostic(nil), analysis.Diagnostics...)}
		}
		if reporters[bag] == nil {
			reporters[bag] = diag.NewDedupReporter(&diag.BagReporter{Bag: bag})
		}
		reporters[bag].ReportDiagnostic(diagnostic)
		bag.Sort()
	}
	return nil
}

func analyzeReturnOriginResult(ctx context.Context, res *DiagnoseResult, rootBag *diag.Bag, reporters returnOriginReporters) (returnOriginOutcome, error) {
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		return returnOriginOutcome{}, err
	}
	// Module finalization checks its input bags before any pass publishes errors.
	// The single-file route must also retain errors in imported owning bags.
	if rootBag != nil {
		for _, unit := range inputs.units {
			file := unit.Builder.Files.Get(unit.FileID)
			if bag := inputs.bags[file.Span.File]; bag.HasErrors() {
				return returnOriginOutcome{}, returnOriginBagRefusal(unit.SourceKey, bag)
			}
		}
	}
	analysis, err := sema.AnalyzeReturnOrigins(ctx, res.Sema, inputs.units)
	if err != nil {
		return returnOriginOutcome{}, err
	}
	if analysis == nil {
		return returnOriginOutcome{}, fmt.Errorf("return-origin analysis did not run")
	}
	outcome := returnOriginOutcome{analysis: analysis, identity: res.Sema.InstantiationIdentity, closure: res.Sema.InstantiationClosure}
	if err := publishReturnOriginDiagnostics(analysis, inputs, rootBag, reporters); err != nil {
		return outcome, err
	}
	_, err = returnOriginVerdict(analysis)
	return outcome, err
}
