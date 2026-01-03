package driver

import (
	"context"
	"fmt"
	"sort"

	"fortio.org/safecast"

	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// CombineHIRWithModules appends module bodies (including stdlib dependencies) to the root HIR
// module so that cross-module calls can be executed by the VM.
func CombineHIRWithModules(ctx context.Context, res *DiagnoseResult) (*hir.Module, error) {
	if res == nil || res.HIR == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if res.Symbols == nil || res.Symbols.Table == nil || res.Symbols.Table.Symbols == nil {
		return res.HIR, nil
	}

	base := res.HIR
	combined := &hir.Module{
		Name:         base.Name,
		Path:         base.Path,
		SourceAST:    base.SourceAST,
		Funcs:        append([]*hir.Func(nil), base.Funcs...),
		Types:        append([]hir.TypeDecl(nil), base.Types...),
		Consts:       append([]hir.ConstDecl(nil), base.Consts...),
		Globals:      append([]hir.VarDecl(nil), base.Globals...),
		TypeInterner: base.TypeInterner,
		BindingTypes: base.BindingTypes,
		Symbols:      base.Symbols,
	}

	nextFnID := maxFuncID(combined.Funcs) + 1

	if err := appendRootModuleFiles(ctx, res, combined, &nextFnID); err != nil {
		return nil, err
	}
	mergeCopyTypesFromRecord(res.Sema, res.rootRecord)

	rootPath := ""
	if res.rootRecord != nil && res.rootRecord.Meta != nil {
		rootPath = normalizeExportsKey(res.rootRecord.Meta.Path)
	}

	paths := make([]string, 0, len(res.moduleRecords))
	for path, rec := range res.moduleRecords {
		if rec != nil && rec.Meta != nil && rec.Meta.Path != "" {
			paths = append(paths, rec.Meta.Path)
			continue
		}
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		norm := normalizeExportsKey(path)
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		rec := res.moduleRecords[path]
		if rec == nil {
			rec = res.moduleRecords[norm]
		}
		if rec == nil || rec.Meta == nil {
			continue
		}
		if rootPath != "" && normalizeExportsKey(rec.Meta.Path) == rootPath {
			continue
		}
		if err := appendModuleRecordHIR(ctx, res, rec, combined, &nextFnID); err != nil {
			return nil, err
		}
	}

	return combined, nil
}

func appendRootModuleFiles(ctx context.Context, res *DiagnoseResult, combined *hir.Module, nextFnID *hir.FuncID) error {
	if res == nil || combined == nil || nextFnID == nil {
		return nil
	}
	rootRec := res.rootRecord
	if rootRec == nil || rootRec.Builder == nil || len(rootRec.FileIDs) == 0 {
		return nil
	}
	for _, fileID := range rootRec.FileIDs {
		if fileID == res.FileID {
			continue
		}
		semaRes := rootRec.Sema[fileID]
		symRes, ok := rootRec.Symbols[fileID]
		if !ok || semaRes == nil {
			continue
		}
		rootHIR, err := hir.Lower(ctx, rootRec.Builder, fileID, semaRes, &symRes)
		if err != nil {
			return err
		}
		if rootHIR == nil {
			continue
		}
		for _, fn := range rootHIR.Funcs {
			if fn == nil {
				continue
			}
			fn.ID = *nextFnID
			*nextFnID++
		}
		combined.Funcs = append(combined.Funcs, rootHIR.Funcs...)
		combined.Types = append(combined.Types, rootHIR.Types...)
		combined.Consts = append(combined.Consts, rootHIR.Consts...)
		combined.Globals = append(combined.Globals, rootHIR.Globals...)
	}
	return nil
}

func appendModuleRecordHIR(ctx context.Context, res *DiagnoseResult, rec *moduleRecord, combined *hir.Module, nextFnID *hir.FuncID) error {
	if rec == nil || rec.Builder == nil || rec.Table == nil || combined == nil || nextFnID == nil {
		return nil
	}
	mapping := buildModuleSymbolRemap(res.Symbols, rec)
	if len(mapping) > 0 {
		remapTypeParamOwners(res.Sema, mapping)
	}
	mergeCopyTypesFromRecord(res.Sema, rec)
	if err := appendModuleInstantiations(ctx, res, rec, mapping); err != nil {
		return err
	}

	for _, fileID := range rec.FileIDs {
		semaRes := rec.Sema[fileID]
		symRes, ok := rec.Symbols[fileID]
		if !ok || semaRes == nil {
			continue
		}
		modHIR, err := hir.Lower(ctx, rec.Builder, fileID, semaRes, &symRes)
		if err != nil {
			return err
		}
		if modHIR == nil {
			continue
		}
		for _, fn := range modHIR.Funcs {
			if fn == nil {
				continue
			}
			fn.Flags &^= hir.FuncEntrypoint
		}
		remapHIRModule(modHIR, mapping)
		for _, fn := range modHIR.Funcs {
			if fn == nil {
				continue
			}
			fn.ID = *nextFnID
			*nextFnID++
		}
		combined.Funcs = append(combined.Funcs, modHIR.Funcs...)
		combined.Types = append(combined.Types, modHIR.Types...)
		combined.Consts = append(combined.Consts, modHIR.Consts...)
		combined.Globals = append(combined.Globals, modHIR.Globals...)
	}
	return nil
}

func mergeCopyTypesFromRecord(dst *sema.Result, rec *moduleRecord) {
	if dst == nil || rec == nil || rec.Sema == nil {
		return
	}
	for _, semaRes := range rec.Sema {
		if semaRes == nil || len(semaRes.CopyTypes) == 0 {
			continue
		}
		if dst.CopyTypes == nil {
			dst.CopyTypes = make(map[types.TypeID]struct{}, len(semaRes.CopyTypes))
		}
		for ty := range semaRes.CopyTypes {
			dst.CopyTypes[ty] = struct{}{}
		}
	}
}

func appendModuleInstantiations(ctx context.Context, res *DiagnoseResult, rec *moduleRecord, mapping map[symbols.SymbolID]symbols.SymbolID) error {
	if res == nil || res.Instantiations == nil || res.Sema == nil || res.Sema.TypeInterner == nil || rec == nil || rec.Builder == nil {
		return nil
	}
	if len(mapping) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	inst := mono.NewInstantiationMap()
	recorder := mono.NewInstantiationMapRecorder(inst)
	exports := collectedExports(res.moduleRecords)
	if exports == nil {
		exports = make(map[string]*symbols.ModuleExports)
	}

	moduleLabel := "module"
	if rec.Meta != nil && rec.Meta.Path != "" {
		moduleLabel = rec.Meta.Path
	}

	paramStart := res.Sema.TypeInterner.TypeParamCount()
	for _, fileID := range rec.FileIDs {
		symRes, ok := rec.Symbols[fileID]
		if !ok {
			continue
		}
		bag := diag.NewBag(0)
		sema.Check(ctx, rec.Builder, fileID, sema.Options{
			Reporter:       &diag.BagReporter{Bag: bag},
			Symbols:        &symRes,
			Exports:        exports,
			Types:          res.Sema.TypeInterner,
			AlienHints:     false,
			Bag:            bag,
			Instantiations: recorder,
		})
		if bag.HasErrors() {
			items := bag.Items()
			if len(items) > 0 {
				return fmt.Errorf("%s instantiation pass failed: %s", moduleLabel, items[0].Message)
			}
			return fmt.Errorf("%s instantiation pass failed", moduleLabel)
		}
	}

	remapTypeParamOwnersFrom(res.Sema, mapping, paramStart)
	mergeInstantiations(res.Instantiations, inst, mapping)
	return nil
}

func buildModuleSymbolRemap(rootSyms *symbols.Result, rec *moduleRecord) map[symbols.SymbolID]symbols.SymbolID {
	if rootSyms == nil || rootSyms.Table == nil || rootSyms.Table.Symbols == nil || rec == nil || rec.Table == nil || rec.Table.Symbols == nil {
		return nil
	}

	rootTable := rootSyms.Table
	rootMap := make(map[string]symbols.SymbolID)
	rootLen := rootTable.Symbols.Len()
	for i := 1; i <= rootLen; i++ {
		id, err := safecast.Conv[symbols.SymbolID](i)
		if err != nil {
			continue
		}
		sym := rootTable.Symbols.Get(id)
		if sym == nil || sym.Flags&symbols.SymbolFlagImported == 0 {
			continue
		}
		if rootTable.Scopes != nil {
			if scope := rootTable.Scopes.Get(sym.Scope); scope != nil {
				if scope.Kind == symbols.ScopeFunction || scope.Kind == symbols.ScopeBlock {
					continue
				}
			}
		}
		modulePath := normalizeExportsKey(sym.ModulePath)
		if modulePath == "" {
			continue
		}
		key := moduleSymbolKey(modulePath, sym, rootTable.Strings)
		if key != "" {
			rootMap[key] = id
		}
	}

	mapping := make(map[symbols.SymbolID]symbols.SymbolID)
	modTable := rec.Table
	modLen := modTable.Symbols.Len()
	for i := 1; i <= modLen; i++ {
		id, err := safecast.Conv[symbols.SymbolID](i)
		if err != nil {
			continue
		}
		sym := modTable.Symbols.Get(id)
		if sym == nil {
			continue
		}
		if modTable.Scopes != nil {
			if scope := modTable.Scopes.Get(sym.Scope); scope != nil {
				if scope.Kind == symbols.ScopeFunction || scope.Kind == symbols.ScopeBlock {
					continue
				}
			}
		}
		modulePath := normalizeExportsKey(sym.ModulePath)
		if modulePath == "" && rec.Meta != nil {
			modulePath = normalizeExportsKey(rec.Meta.Path)
		}
		key := moduleSymbolKey(modulePath, sym, modTable.Strings)
		if key != "" {
			if rootID, ok := rootMap[key]; ok {
				mapping[id] = rootID
				continue
			}
		}
		newID := synthesizeModuleSymbol(rootTable, modulePath, sym)
		if newID.IsValid() {
			mapping[id] = newID
			if key != "" {
				rootMap[key] = newID
			}
		}
	}

	return mapping
}

func moduleSymbolKey(modulePath string, sym *symbols.Symbol, strs *source.Interner) string {
	if sym == nil {
		return ""
	}
	key := symbolKey(sym, strs)
	if key == "" {
		return ""
	}
	if modulePath == "" {
		return key
	}
	return modulePath + "::" + key
}

func synthesizeModuleSymbol(table *symbols.Table, modulePath string, sym *symbols.Symbol) symbols.SymbolID {
	if table == nil || table.Symbols == nil || sym == nil {
		return symbols.NoSymbolID
	}
	scope := table.ModuleRoot(modulePath, sym.Span)
	importName := sym.ImportName
	if importName == source.NoStringID {
		importName = sym.Name
	}
	clone := symbols.Symbol{
		Name:           sym.Name,
		Kind:           sym.Kind,
		Scope:          scope,
		Span:           sym.Span,
		Flags:          sym.Flags | symbols.SymbolFlagImported,
		Type:           sym.Type,
		Signature:      sym.Signature,
		ModulePath:     modulePath,
		ImportName:     importName,
		Receiver:       sym.Receiver,
		ReceiverKey:    sym.ReceiverKey,
		TypeParams:     append([]source.StringID(nil), sym.TypeParams...),
		TypeParamSpan:  sym.TypeParamSpan,
		EntrypointMode: sym.EntrypointMode,
	}
	id := table.Symbols.New(&clone)
	if scopeEntry := table.Scopes.Get(scope); scopeEntry != nil {
		scopeEntry.Symbols = append(scopeEntry.Symbols, id)
		if scopeEntry.NameIndex == nil {
			scopeEntry.NameIndex = make(map[source.StringID][]symbols.SymbolID)
		}
		scopeEntry.NameIndex[clone.Name] = append(scopeEntry.NameIndex[clone.Name], id)
	}
	return id
}
