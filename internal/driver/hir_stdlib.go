package driver

import (
	"context"
	"fmt"
	"strings"

	"fortio.org/safecast"

	"surge/internal/diag"
	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/sema"
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

	mapping := buildCoreSymbolRemap(res.Symbols, coreRec)
	if len(mapping) == 0 {
		return res.HIR, nil
	}

	if err := appendCoreInstantiations(ctx, res, coreRec, mapping); err != nil {
		return nil, err
	}
	remapTypeParamOwners(res.Sema, mapping)

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

func remapTypeParamOwners(semaRes *sema.Result, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if semaRes == nil || semaRes.TypeInterner == nil || len(mapping) == 0 {
		return
	}
	owners := make(map[uint32]uint32, len(mapping))
	for from, to := range mapping {
		owners[uint32(from)] = uint32(to)
	}
	semaRes.TypeInterner.RemapTypeParamOwners(owners)
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
	rootSymsLen := rootSyms.Table.Symbols.Len()
	for i := 1; i <= rootSymsLen; i++ {
		id, err := safecast.Conv[symbols.SymbolID](i)
		if err != nil {
			panic(fmt.Errorf("symbol id overflow: %w", err))
		}
		sym := rootSyms.Table.Symbols.Get(id)
		if sym == nil || sym.Flags&symbols.SymbolFlagImported == 0 {
			continue
		}
		if !isCoreModulePath(sym.ModulePath) {
			continue
		}
		key := symbolKey(sym, rootSyms.Table.Strings)
		if key == "" {
			continue
		}
		rootMap[key] = id
	}

	mapping := make(map[symbols.SymbolID]symbols.SymbolID)
	coreSymsLen := coreRec.Table.Symbols.Len()
	for i := 1; i <= coreSymsLen; i++ {
		id, err := safecast.Conv[symbols.SymbolID](i)
		if err != nil {
			panic(fmt.Errorf("symbol id overflow: %w", err))
		}
		sym := coreRec.Table.Symbols.Get(id)
		if sym == nil {
			continue
		}
		if sym.Flags&symbols.SymbolFlagPublic == 0 && sym.Flags&symbols.SymbolFlagBuiltin == 0 {
			continue
		}
		key := symbolKey(sym, coreRec.Table.Strings)
		if key == "" {
			continue
		}
		if rootID, ok := rootMap[key]; ok {
			mapping[id] = rootID
		}
	}

	return mapping
}

func appendCoreInstantiations(ctx context.Context, res *DiagnoseResult, coreRec *moduleRecord, mapping map[symbols.SymbolID]symbols.SymbolID) error {
	if res == nil || res.Instantiations == nil || res.Sema == nil || res.Sema.TypeInterner == nil || coreRec == nil || coreRec.Builder == nil {
		return nil
	}
	if len(mapping) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	coreInst := mono.NewInstantiationMap()
	recorder := mono.NewInstantiationMapRecorder(coreInst)
	exports := collectedExports(res.moduleRecords)
	if exports == nil {
		exports = make(map[string]*symbols.ModuleExports)
	}

	for _, fileID := range coreRec.FileIDs {
		symRes, ok := coreRec.Symbols[fileID]
		if !ok {
			continue
		}
		bag := diag.NewBag(0)
		sema.Check(ctx, coreRec.Builder, fileID, sema.Options{
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
				return fmt.Errorf("core instantiation pass failed: %s", items[0].Message)
			}
			return fmt.Errorf("core instantiation pass failed")
		}
	}

	mergeInstantiations(res.Instantiations, coreInst, mapping)
	return nil
}

func mergeInstantiations(dst, src *mono.InstantiationMap, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if dst == nil || src == nil || len(src.Entries) == 0 || len(mapping) == 0 {
		return
	}
	for _, entry := range src.Entries {
		if entry == nil {
			continue
		}
		mappedSym, ok := mapping[entry.Key.Sym]
		if !ok {
			continue
		}
		for _, site := range entry.UseSites {
			mappedCaller, ok := mapping[site.Caller]
			if !ok {
				continue
			}
			dst.Record(entry.Kind, mappedSym, entry.TypeArgs, site.Span, mappedCaller, site.Note)
		}
	}
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

func remapHIRModule(mod *hir.Module, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if mod == nil || len(mapping) == 0 {
		return
	}
	for i := range mod.Types {
		mod.Types[i].SymbolID = remapSymbol(mod.Types[i].SymbolID, mapping)
	}
	for i := range mod.Consts {
		mod.Consts[i].SymbolID = remapSymbol(mod.Consts[i].SymbolID, mapping)
		remapExpr(mod.Consts[i].Value, mapping)
	}
	for i := range mod.Globals {
		mod.Globals[i].SymbolID = remapSymbol(mod.Globals[i].SymbolID, mapping)
		remapExpr(mod.Globals[i].Value, mapping)
	}
	for _, fn := range mod.Funcs {
		remapFunc(fn, mapping)
	}
}

func remapFunc(fn *hir.Func, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if fn == nil {
		return
	}
	fn.SymbolID = remapSymbol(fn.SymbolID, mapping)
	for i := range fn.Params {
		fn.Params[i].SymbolID = remapSymbol(fn.Params[i].SymbolID, mapping)
	}
	if fn.Body != nil {
		remapBlock(fn.Body, mapping)
	}
}

func remapBlock(block *hir.Block, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if block == nil {
		return
	}
	for i := range block.Stmts {
		remapStmt(&block.Stmts[i], mapping)
	}
}

func remapStmt(st *hir.Stmt, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if st == nil {
		return
	}
	switch st.Kind {
	case hir.StmtLet:
		data, ok := st.Data.(hir.LetData)
		if !ok {
			return
		}
		data.SymbolID = remapSymbol(data.SymbolID, mapping)
		remapExpr(data.Value, mapping)
		remapExpr(data.Pattern, mapping)
		st.Data = data
	case hir.StmtExpr:
		data, ok := st.Data.(hir.ExprStmtData)
		if !ok {
			return
		}
		remapExpr(data.Expr, mapping)
		st.Data = data
	case hir.StmtAssign:
		data, ok := st.Data.(hir.AssignData)
		if !ok {
			return
		}
		remapExpr(data.Target, mapping)
		remapExpr(data.Value, mapping)
		st.Data = data
	case hir.StmtReturn:
		data, ok := st.Data.(hir.ReturnData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping)
		st.Data = data
	case hir.StmtIf:
		data, ok := st.Data.(hir.IfStmtData)
		if !ok {
			return
		}
		remapExpr(data.Cond, mapping)
		remapBlock(data.Then, mapping)
		remapBlock(data.Else, mapping)
		st.Data = data
	case hir.StmtWhile:
		data, ok := st.Data.(hir.WhileData)
		if !ok {
			return
		}
		remapExpr(data.Cond, mapping)
		remapBlock(data.Body, mapping)
		st.Data = data
	case hir.StmtFor:
		data, ok := st.Data.(hir.ForData)
		if !ok {
			return
		}
		data.VarSym = remapSymbol(data.VarSym, mapping)
		if data.Init != nil {
			remapStmt(data.Init, mapping)
		}
		remapExpr(data.Cond, mapping)
		remapExpr(data.Post, mapping)
		remapExpr(data.Iterable, mapping)
		remapBlock(data.Body, mapping)
		st.Data = data
	case hir.StmtBlock:
		data, ok := st.Data.(hir.BlockStmtData)
		if !ok {
			return
		}
		remapBlock(data.Block, mapping)
		st.Data = data
	case hir.StmtDrop:
		data, ok := st.Data.(hir.DropData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping)
		st.Data = data
	default:
	}
}

func remapExpr(expr *hir.Expr, mapping map[symbols.SymbolID]symbols.SymbolID) {
	if expr == nil {
		return
	}
	switch expr.Kind {
	case hir.ExprVarRef:
		data, ok := expr.Data.(hir.VarRefData)
		if !ok {
			return
		}
		data.SymbolID = remapSymbol(data.SymbolID, mapping)
		expr.Data = data
	case hir.ExprUnaryOp:
		data, ok := expr.Data.(hir.UnaryOpData)
		if !ok {
			return
		}
		remapExpr(data.Operand, mapping)
		expr.Data = data
	case hir.ExprBinaryOp:
		data, ok := expr.Data.(hir.BinaryOpData)
		if !ok {
			return
		}
		remapExpr(data.Left, mapping)
		remapExpr(data.Right, mapping)
		expr.Data = data
	case hir.ExprCall:
		data, ok := expr.Data.(hir.CallData)
		if !ok {
			return
		}
		data.SymbolID = remapSymbol(data.SymbolID, mapping)
		remapExpr(data.Callee, mapping)
		for _, arg := range data.Args {
			remapExpr(arg, mapping)
		}
		expr.Data = data
	case hir.ExprFieldAccess:
		data, ok := expr.Data.(hir.FieldAccessData)
		if !ok {
			return
		}
		remapExpr(data.Object, mapping)
		expr.Data = data
	case hir.ExprIndex:
		data, ok := expr.Data.(hir.IndexData)
		if !ok {
			return
		}
		remapExpr(data.Object, mapping)
		remapExpr(data.Index, mapping)
		expr.Data = data
	case hir.ExprStructLit:
		data, ok := expr.Data.(hir.StructLitData)
		if !ok {
			return
		}
		for i := range data.Fields {
			remapExpr(data.Fields[i].Value, mapping)
		}
		expr.Data = data
	case hir.ExprArrayLit:
		data, ok := expr.Data.(hir.ArrayLitData)
		if !ok {
			return
		}
		for _, el := range data.Elements {
			remapExpr(el, mapping)
		}
		expr.Data = data
	case hir.ExprTupleLit:
		data, ok := expr.Data.(hir.TupleLitData)
		if !ok {
			return
		}
		for _, el := range data.Elements {
			remapExpr(el, mapping)
		}
		expr.Data = data
	case hir.ExprCompare:
		data, ok := expr.Data.(hir.CompareData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping)
		for i := range data.Arms {
			remapExpr(data.Arms[i].Pattern, mapping)
			remapExpr(data.Arms[i].Guard, mapping)
			remapExpr(data.Arms[i].Result, mapping)
		}
		expr.Data = data
	case hir.ExprTagTest:
		data, ok := expr.Data.(hir.TagTestData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping)
		expr.Data = data
	case hir.ExprTagPayload:
		data, ok := expr.Data.(hir.TagPayloadData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping)
		expr.Data = data
	case hir.ExprIterInit:
		data, ok := expr.Data.(hir.IterInitData)
		if !ok {
			return
		}
		remapExpr(data.Iterable, mapping)
		expr.Data = data
	case hir.ExprIterNext:
		data, ok := expr.Data.(hir.IterNextData)
		if !ok {
			return
		}
		remapExpr(data.Iter, mapping)
		expr.Data = data
	case hir.ExprIf:
		data, ok := expr.Data.(hir.IfData)
		if !ok {
			return
		}
		remapExpr(data.Cond, mapping)
		remapExpr(data.Then, mapping)
		remapExpr(data.Else, mapping)
		expr.Data = data
	case hir.ExprAwait:
		data, ok := expr.Data.(hir.AwaitData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping)
		expr.Data = data
	case hir.ExprSpawn:
		data, ok := expr.Data.(hir.SpawnData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping)
		expr.Data = data
	case hir.ExprAsync:
		data, ok := expr.Data.(hir.AsyncData)
		if !ok {
			return
		}
		remapBlock(data.Body, mapping)
		expr.Data = data
	case hir.ExprCast:
		data, ok := expr.Data.(hir.CastData)
		if !ok {
			return
		}
		remapExpr(data.Value, mapping)
		expr.Data = data
	case hir.ExprBlock:
		data, ok := expr.Data.(hir.BlockExprData)
		if !ok {
			return
		}
		remapBlock(data.Block, mapping)
		expr.Data = data
	default:
	}
}
