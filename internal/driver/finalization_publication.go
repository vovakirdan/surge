package driver

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/symbols"
)

func finalizeDiagnoseResult(ctx context.Context, res *DiagnoseResult) (bool, error) {
	if res == nil || res.Sema == nil || res.Symbols == nil || res.Bag == nil || res.Bag.HasErrors() {
		return false, nil
	}
	if err := FinalizeInstantiationClosure(ctx, res, 64); err != nil {
		return false, fmt.Errorf("instantiation closure: %w", err)
	}
	return res.Bag.HasErrors(), nil
}

type finalizationDiagnosticError interface {
	error
	Diagnostic() *diag.Diagnostic
}

func consumeFinalizationDiagnostic(res *DiagnoseResult, err error) bool {
	if res == nil || res.Bag == nil || err == nil {
		return false
	}
	var sourceError finalizationDiagnosticError
	if !errors.As(err, &sourceError) {
		return false
	}
	diagnostic := sourceError.Diagnostic()
	if diagnostic == nil {
		return false
	}
	res.Bag.Add(diagnostic)
	res.Bag.Sort()
	return true
}

// publishFinalizationDecisions is the single post-merge publication point for
// decisions that per-file HIR lowering consumes. Feature-specific projection
// belongs in sema.PublishFinalizationDecisions; the driver only supplies the
// owning source identity and root-to-local symbol vocabulary.
func publishFinalizationDecisions(res *DiagnoseResult) error {
	if res == nil || res.Sema == nil {
		return nil
	}
	resolveSource := canonicalInstantiationSourceResolver(res)
	seenResults := map[*sema.Result]struct{}{res.Sema: {}}
	seenRecords := make(map[*moduleRecord]struct{})

	publishRecord := func(rec *moduleRecord, rootToLocal map[symbols.SymbolID][]symbols.SymbolID) error {
		if rec == nil || rec.Builder == nil {
			return nil
		}
		for _, fileID := range rec.FileIDs {
			target := rec.Sema[fileID]
			if target == nil {
				continue
			}
			if _, seen := seenResults[target]; seen {
				continue
			}
			file := rec.Builder.Files.Get(fileID)
			if file == nil {
				return fmt.Errorf("finalization publication: missing AST file %d", fileID)
			}
			sourceKey, err := resolveSource(file.Span.File)
			if err != nil {
				return fmt.Errorf("finalization publication for file %d: %w", fileID, err)
			}
			if err := sema.PublishFinalizationDecisions(target, res.Sema, sema.FinalizationPublication{
				SourceKey: sourceKey, RootToLocalSymbols: rootToLocal,
			}); err != nil {
				return fmt.Errorf("finalization publication for %s: %w", sourceKey, err)
			}
			seenResults[target] = struct{}{}
		}
		return nil
	}

	if res.rootRecord != nil {
		seenRecords[res.rootRecord] = struct{}{}
		if err := publishRecord(res.rootRecord, nil); err != nil {
			return err
		}
	}
	paths := make([]string, 0, len(res.moduleRecords))
	for path := range res.moduleRecords {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		rec := res.moduleRecords[path]
		if rec == nil {
			continue
		}
		if _, seen := seenRecords[rec]; seen {
			continue
		}
		seenRecords[rec] = struct{}{}
		localToRoot, ok := cachedInstantiationSymbolRemap(rec, res.Symbols.Table)
		if !ok {
			localToRoot = buildModuleSymbolRemap(res.Symbols, rec)
			if isCoreModulePath(path) {
				if core := buildCoreSymbolRemap(res.Symbols, rec); len(core) > 0 {
					localToRoot = core
				}
			}
		}
		rootToLocal := invertFinalizationSymbolRemap(localToRoot)
		if err := publishRecord(rec, rootToLocal); err != nil {
			return err
		}
	}
	return nil
}

func invertFinalizationSymbolRemap(localToRoot map[symbols.SymbolID]symbols.SymbolID) map[symbols.SymbolID][]symbols.SymbolID {
	if len(localToRoot) == 0 {
		return nil
	}
	rootToLocal := make(map[symbols.SymbolID][]symbols.SymbolID, len(localToRoot))
	for local, root := range localToRoot {
		rootToLocal[root] = append(rootToLocal[root], local)
	}
	for root := range rootToLocal {
		sort.Slice(rootToLocal[root], func(i, j int) bool { return rootToLocal[root][i] < rootToLocal[root][j] })
	}
	return rootToLocal
}
