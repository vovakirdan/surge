package driver

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
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
// owning source identity and canonical local callable vocabulary.
func publishFinalizationDecisions(res *DiagnoseResult) error {
	if res == nil || res.Sema == nil {
		return nil
	}
	if res.finalizationOwner == nil {
		res.finalizationOwner = snapshotFinalizationAuthority(res.Sema)
	}
	authority := res.finalizationOwner
	resolveSource := canonicalInstantiationSourceResolver(res)
	seenResults := make(map[*sema.Result]struct{})
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
			if err := sema.PublishFinalizationDecisions(target, authority, sema.FinalizationPublication{
				SourceKey: sourceKey, RootToLocalSymbols: rootToLocal,
				LocalCallables: res.finalizationIndex[rec],
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
	// A single-file build has no module record to walk, so the authority result
	// is also the owner of its own decisions and must still receive them.
	if _, seen := seenResults[res.Sema]; !seen {
		ownerID, err := finalizationOwnerSource(res)
		if err != nil {
			return err
		}
		sourceKey, err := resolveSource(ownerID)
		if err != nil {
			return fmt.Errorf("finalization publication for source file %d: %w", ownerID, err)
		}
		if err := sema.PublishFinalizationDecisions(res.Sema, authority, sema.FinalizationPublication{
			SourceKey: sourceKey,
		}); err != nil {
			return fmt.Errorf("finalization publication for %s: %w", sourceKey, err)
		}
	}
	return nil
}

// finalizationOwnerSource answers which source file owns res.Sema's decisions.
// The AST file the result was checked against is the authority, exactly as it
// is for the module files publishRecord walks above: its span names the owning
// source file. res.File is only a handle to that same file, kept here as the
// answer for callers that carry no AST builder. Nothing keys off whether that
// handle happens to be present, because an absent handle used to mean the
// result silently received no decisions at all.
func finalizationOwnerSource(res *DiagnoseResult) (source.FileID, error) {
	if res.Builder != nil && res.FileID != ast.NoFileID {
		if node := res.Builder.Files.Get(res.FileID); node != nil {
			return node.Span.File, nil
		}
	}
	if res.File != nil {
		return res.File.ID, nil
	}
	return 0, fmt.Errorf(
		"finalization publication: AST file %d names no owning source file; "+
			"searched the AST builder file table (AST id space) and the diagnosed source file (source id space)",
		res.FileID,
	)
}

func snapshotFinalizationAuthority(src *sema.Result) *sema.Result {
	if src == nil {
		return nil
	}
	return &sema.Result{
		CallableCandidates:         append([]sema.CallableCandidate(nil), src.CallableCandidates...),
		EntrypointCallableBindings: append([]sema.EntrypointCallableBinding(nil), src.EntrypointCallableBindings...),
		DirectCloneBindings:        append([]sema.DirectCloneBinding(nil), src.DirectCloneBindings...),
	}
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
