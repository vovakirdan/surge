package driver

import (
	"context"
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
)

// CombineHIRWithCore appends core module bodies to the root HIR module so that
// stdlib functions implemented in Surge can be executed by the VM.
func CombineHIRWithCore(ctx context.Context, res *DiagnoseResult) (*hir.Module, error) {
	if res == nil || res.HIR == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if res.rootRecord != nil && res.rootRecord.Meta != nil {
		if isCoreModulePath(res.rootRecord.Meta.Path) {
			return res.HIR, nil
		}
	}
	coreRec := findCoreRecord(res.moduleRecords)
	if coreRec == nil || coreRec.Builder == nil || coreRec.Table == nil {
		return res.HIR, nil
	}
	if res.Symbols == nil || res.Symbols.Table == nil {
		return res.HIR, nil
	}
	if err := FinalizeInstantiationClosure(ctx, res, 64); err != nil {
		return nil, err
	}
	if res.Bag != nil && res.Bag.HasErrors() {
		return nil, ErrDiagnosticsReported
	}

	mapping, cachedMapping := cachedInstantiationSymbolRemap(coreRec, res.Symbols.Table)
	if !cachedMapping {
		mapping = buildCoreSymbolRemap(res.Symbols, coreRec)
	}
	if len(mapping) == 0 {
		return res.HIR, nil
	}

	cacheInstantiationSymbolRemap(coreRec, res.Symbols.Table, mapping)

	combined := &hir.Module{
		Name:         res.HIR.Name,
		Path:         res.HIR.Path,
		SourceAST:    res.HIR.SourceAST,
		Funcs:        append([]*hir.Func(nil), res.HIR.Funcs...),
		Types:        append([]hir.TypeDecl(nil), res.HIR.Types...),
		Consts:       append([]hir.ConstDecl(nil), res.HIR.Consts...),
		Globals:      append([]hir.VarDecl(nil), res.HIR.Globals...),
		TypeInterner: res.HIR.TypeInterner,
		BindingTypes: res.HIR.BindingTypes,
		Symbols:      res.HIR.Symbols,
	}

	nextFnID := maxFuncID(combined.Funcs) + 1

	for _, fileID := range coreRec.FileIDs {
		semaRes := coreRec.Sema[fileID]
		symRes, ok := coreRec.Symbols[fileID]
		if !ok || semaRes == nil {
			continue
		}
		coreHIR, err := hir.Lower(ctx, coreRec.Builder, fileID, semaRes, &symRes)
		if err != nil {
			return nil, err
		}
		if coreHIR == nil {
			continue
		}
		remapHIRModule(coreHIR, mapping)
		for _, fn := range coreHIR.Funcs {
			if fn == nil {
				continue
			}
			fn.ID = nextFnID
			nextFnID++
		}
		combined.Funcs = append(combined.Funcs, coreHIR.Funcs...)
		combined.Types = append(combined.Types, coreHIR.Types...)
		combined.Consts = append(combined.Consts, coreHIR.Consts...)
		combined.Globals = append(combined.Globals, coreHIR.Globals...)
	}

	return combined, nil
}

func findCoreRecord(records map[string]*moduleRecord) *moduleRecord {
	if len(records) == 0 {
		return nil
	}
	if rec := records["core"]; rec != nil {
		return rec
	}
	for _, rec := range records {
		if rec == nil || rec.Meta == nil {
			continue
		}
		if isCoreModulePath(rec.Meta.Path) {
			return rec
		}
	}
	return nil
}

func isCoreModulePath(path string) bool {
	return path == "core" || strings.HasPrefix(path, "core/")
}

func buildCoreSymbolRemap(rootSyms *symbols.Result, coreRec *moduleRecord) map[symbols.SymbolID]symbols.SymbolID {
	if rootSyms == nil || rootSyms.Table == nil || rootSyms.Table.Symbols == nil || rootSyms.Table.Strings == nil {
		return nil
	}
	if coreRec == nil || coreRec.Table == nil || coreRec.Table.Symbols == nil || coreRec.Table.Strings == nil {
		return nil
	}

	rootMap := make(map[string]symbols.SymbolID)
	rootBuiltins := make(map[string]symbols.SymbolID)
	rootSymsLen := rootSyms.Table.Symbols.Len()
	for i := 1; i <= rootSymsLen; i++ {
		id, err := safecast.Conv[symbols.SymbolID](i)
		if err != nil {
			panic(fmt.Errorf("symbol id overflow: %w", err))
		}
		sym := rootSyms.Table.Symbols.Get(id)
		if sym == nil {
			continue
		}
		if isLocalSymbol(sym, rootSyms.Table) {
			continue
		}
		if sym.Flags&symbols.SymbolFlagImported == 0 && sym.Flags&symbols.SymbolFlagBuiltin == 0 {
			continue
		}
		key := symbolKey(sym, rootSyms.Table.Strings)
		if key == "" {
			continue
		}
		if sym.ModulePath != "" && isCoreModulePath(sym.ModulePath) {
			rootMap[key] = id
			continue
		}
		if sym.Kind == symbols.SymbolType && sym.ModulePath == "" && sym.Flags&symbols.SymbolFlagBuiltin != 0 {
			if _, exists := rootBuiltins[key]; !exists {
				rootBuiltins[key] = id
			}
			if _, exists := rootMap[key]; !exists {
				rootMap[key] = id
			}
		}
	}

	mapping := make(map[symbols.SymbolID]symbols.SymbolID)
	coreSymsLen := coreRec.Table.Symbols.Len()
	modulePath := "core"
	if coreRec.Meta != nil && coreRec.Meta.Path != "" {
		modulePath = normalizeExportsKey(coreRec.Meta.Path)
	}
	for i := 1; i <= coreSymsLen; i++ {
		id, err := safecast.Conv[symbols.SymbolID](i)
		if err != nil {
			panic(fmt.Errorf("symbol id overflow: %w", err))
		}
		sym := coreRec.Table.Symbols.Get(id)
		if sym == nil {
			continue
		}
		key := ""
		if !isLocalSymbol(sym, coreRec.Table) && (sym.Flags&symbols.SymbolFlagPublic != 0 || sym.Flags&symbols.SymbolFlagBuiltin != 0) {
			key = symbolKey(sym, coreRec.Table.Strings)
			if key != "" {
				if sym.Kind == symbols.SymbolType && sym.ModulePath == "" && sym.Flags&symbols.SymbolFlagBuiltin != 0 {
					if rootID, ok := rootBuiltins[key]; ok {
						mapping[id] = rootID
						continue
					}
				}
				if rootID, ok := rootMap[key]; ok {
					mapping[id] = rootID
					continue
				}
			}
		}
		newID := synthesizeModuleSymbol(rootSyms.Table, modulePath, sym)
		if newID.IsValid() {
			mapping[id] = newID
			if key != "" {
				rootMap[key] = newID
			}
		}
	}

	return mapping
}

func symbolKey(sym *symbols.Symbol, strs *source.Interner) string {
	if sym == nil || strs == nil {
		return ""
	}
	name := ""
	if sym.Name != source.NoStringID {
		if s, ok := strs.Lookup(sym.Name); ok {
			name = s
		}
	}
	sig := signatureKey(sym.Signature)
	return fmt.Sprintf("%d|%s|%s|%s|%d", sym.Kind, name, sym.ReceiverKey, sig, len(sym.TypeParams))
}

func signatureKey(sig *symbols.FunctionSignature) string {
	if sig == nil {
		return "nosig"
	}
	var b strings.Builder
	for _, p := range sig.Params {
		b.WriteString(string(p))
		b.WriteByte(',')
	}
	b.WriteString("->")
	b.WriteString(string(sig.Result))
	return b.String()
}

func maxFuncID(funcs []*hir.Func) (maxFID hir.FuncID) {
	for _, fn := range funcs {
		if fn != nil && fn.ID > maxFID {
			maxFID = fn.ID
		}
	}
	return maxFID
}

func remapSymbol(id symbols.SymbolID, mapping map[symbols.SymbolID]symbols.SymbolID) symbols.SymbolID {
	if id.IsValid() {
		if mapped, ok := mapping[id]; ok {
			return mapped
		}
	}
	return id
}
