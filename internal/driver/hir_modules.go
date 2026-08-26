package driver

import (
	"context"
	"sort"

	"fortio.org/safecast"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// HIRCombineOptions configures cross-file and cross-module HIR merging.
type HIRCombineOptions struct {
	CrossingForms map[sema.CrossingLoweringKind]bool
}

// CombineHIRWithModules appends module bodies (including stdlib dependencies) to the root HIR
// module so that cross-module calls can be executed by the VM.
func CombineHIRWithModules(ctx context.Context, res *DiagnoseResult) (*hir.Module, error) {
	return CombineHIRWithModulesWithOptions(ctx, res, HIRCombineOptions{})
}

// CombineHIRWithModulesWithOptions appends module bodies using the same HIR
// feature flags as the root lowering path.
func CombineHIRWithModulesWithOptions(ctx context.Context, res *DiagnoseResult, opts HIRCombineOptions) (*hir.Module, error) {
	if res == nil || res.HIR == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if res.Symbols == nil || res.Symbols.Table == nil || res.Symbols.Table.Symbols == nil {
		return res.HIR, nil
	}
	if err := mergeTypeAttrFactsFromRecords(res); err != nil {
		return nil, err
	}
	res.wholeProgramAuthority = true
	if err := classifyCapabilities(res); err != nil {
		return nil, err
	}
	if err := FinalizeInstantiationClosure(ctx, res, 64); err != nil {
		return nil, err
	}
	if res.Bag != nil && res.Bag.HasErrors() {
		return nil, ErrDiagnosticsReported
	}
	if err := reachRequiredValueOperations(res); err != nil {
		return nil, err
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

	if err := appendRootModuleFiles(ctx, res, combined, &nextFnID, opts); err != nil {
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
		if err := appendModuleRecordHIR(ctx, res, rec, combined, &nextFnID, opts); err != nil {
			return nil, err
		}
	}

	return combined, nil
}

func appendRootModuleFiles(ctx context.Context, res *DiagnoseResult, combined *hir.Module, nextFnID *hir.FuncID, opts HIRCombineOptions) error {
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
		rootHIR, err := hir.LowerWithOptions(ctx, rootRec.Builder, fileID, semaRes, &symRes, hir.LowerOptions{
			CrossingForms: opts.CrossingForms,
		})
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

func appendModuleRecordHIR(ctx context.Context, res *DiagnoseResult, rec *moduleRecord, combined *hir.Module, nextFnID *hir.FuncID, opts HIRCombineOptions) error {
	if rec == nil || rec.Builder == nil || rec.Table == nil || combined == nil || nextFnID == nil {
		return nil
	}
	mapping, cachedMapping := cachedInstantiationSymbolRemap(rec, res.Symbols.Table)
	if !cachedMapping {
		mapping = buildModuleSymbolRemap(res.Symbols, rec)
		if rec.Meta != nil && isCoreModulePath(rec.Meta.Path) {
			if coreMapping := buildCoreSymbolRemap(res.Symbols, rec); len(coreMapping) > 0 {
				mapping = coreMapping
			}
		}
	}
	mergeCopyTypesFromRecord(res.Sema, rec)
	cacheInstantiationSymbolRemap(rec, res.Symbols.Table, mapping)

	for _, fileID := range rec.FileIDs {
		semaRes := rec.Sema[fileID]
		symRes, ok := rec.Symbols[fileID]
		if !ok || semaRes == nil {
			continue
		}
		modHIR, err := hir.LowerWithOptions(ctx, rec.Builder, fileID, semaRes, &symRes, hir.LowerOptions{
			CrossingForms: opts.CrossingForms,
		})
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

// mergeTypeAttrFactsFromRecords folds the detached type attribute facts of
// every record backing this result into res.Sema, and refuses a merged table
// that contradicts itself.
//
// This runs as a pre-pass rather than alongside the Copy merge because Copy is
// not comparable here: it is written into the shared interner, so every
// consumer reads the whole-program answer no matter when the record merge
// happens. The capability attributes exist only in each record's semantic
// result, so anything that runs before this fold — the instantiation closure
// first among them — would see the root file's facts and call them the program's.
func mergeTypeAttrFactsFromRecords(res *DiagnoseResult) error {
	if res == nil || res.Sema == nil {
		return nil
	}
	merge := sema.NewTypeAttrFactMerge()
	rootPath := recordModulePath(res.rootRecord, "")
	// The result's own facts are part of the merged table whether or not a
	// record holds it, so they are attributed before the records are folded in.
	merge.Fold(res.Sema, res.Sema, rootPath)
	foldRecordTypeAttrFacts(merge, res.Sema, res.rootRecord, rootPath)

	paths := make([]string, 0, len(res.moduleRecords))
	for path := range res.moduleRecords {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		rec := res.moduleRecords[path]
		if rec == nil || rec == res.rootRecord {
			continue
		}
		foldRecordTypeAttrFacts(merge, res.Sema, rec, recordModulePath(rec, path))
	}
	return merge.Validate(res.Sema)
}

func foldRecordTypeAttrFacts(merge *sema.TypeAttrFactMerge, dst *sema.Result, rec *moduleRecord, modulePath string) {
	if rec == nil || rec.Sema == nil {
		return
	}
	for _, fileID := range rec.FileIDs {
		if semaRes := rec.Sema[fileID]; semaRes != nil {
			merge.Fold(dst, semaRes, modulePath)
		}
	}
}

func recordModulePath(rec *moduleRecord, fallback string) string {
	if rec != nil && rec.Meta != nil && rec.Meta.Path != "" {
		return rec.Meta.Path
	}
	return fallback
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
		if sym == nil || sym.Flags&(symbols.SymbolFlagImported|symbols.SymbolFlagBuiltin) == 0 {
			continue
		}
		if isLocalSymbol(sym, rootTable) {
			continue
		}
		modulePath := normalizeExportsKey(sym.ModulePath)
		if modulePath == "" && !isPreludeSymbol(sym) {
			continue
		}
		key := moduleSymbolKey(modulePath, sym, rootTable.Strings)
		if key != "" {
			if sym.Kind == symbols.SymbolType && modulePath == "" && sym.Flags&symbols.SymbolFlagBuiltin != 0 {
				if _, exists := rootMap[key]; !exists {
					rootMap[key] = id
				}
				continue
			}
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
		isLocal := isLocalSymbol(sym, modTable)
		modulePath := normalizeExportsKey(sym.ModulePath)
		if modulePath == "" && rec.Meta != nil && !isPreludeSymbol(sym) {
			modulePath = normalizeExportsKey(rec.Meta.Path)
		}
		key := ""
		if !isLocal {
			key = moduleSymbolKey(modulePath, sym, modTable.Strings)
			if key != "" {
				if rootID, ok := rootMap[key]; ok {
					mapping[id] = rootID
					continue
				}
			}
		}
		newID := synthesizeModuleSymbol(rootTable, modulePath, sym)
		if newID.IsValid() {
			mapping[id] = newID
			if key != "" && !isLocal {
				rootMap[key] = newID
			}
		}
	}

	return mapping
}

func isLocalSymbol(sym *symbols.Symbol, table *symbols.Table) bool {
	if sym == nil {
		return false
	}
	if sym.Kind == symbols.SymbolParam {
		return true
	}
	if table == nil || table.Scopes == nil {
		return false
	}
	if scope := table.Scopes.Get(sym.Scope); scope != nil {
		return scope.Kind == symbols.ScopeFunction || scope.Kind == symbols.ScopeBlock
	}
	return false
}

func isPreludeSymbol(sym *symbols.Symbol) bool {
	if sym == nil {
		return false
	}
	return sym.ModulePath == "" &&
		sym.Flags&symbols.SymbolFlagBuiltin != 0 &&
		(sym.Kind == symbols.SymbolType || sym.Flags&symbols.SymbolFlagImported != 0)
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
