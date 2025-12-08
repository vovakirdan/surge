package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"
)

type typeCacheKey struct {
	Type  ast.TypeID
	Scope symbols.ScopeID
	Env   uint32
}

// fieldKey uniquely identifies a struct field for attribute storage
type fieldKey struct {
	TypeID     types.TypeID
	FieldIndex int
}

type typeChecker struct {
	builder  *ast.Builder
	fileID   ast.FileID
	reporter diag.Reporter
	symbols  *symbols.Result
	result   *Result
	types    *types.Interner
	exports  map[string]*symbols.ModuleExports
	magic    map[symbols.TypeKey]map[string][]*symbols.FunctionSignature
	borrow   *BorrowTable

	tracer    trace.Tracer // трассировщик для отладки
	exprDepth int          // глубина рекурсии для typeExpr

	scopeStack                  []symbols.ScopeID
	scopeByItem                 map[ast.ItemID]symbols.ScopeID
	scopeByStmt                 map[ast.StmtID]symbols.ScopeID
	scopeByExtern               map[ast.ExternMemberID]symbols.ScopeID
	stmtSymbols                 map[ast.StmtID]symbols.SymbolID
	externSymbols               map[ast.ExternMemberID]symbols.SymbolID
	bindingBorrow               map[symbols.SymbolID]BorrowID
	bindingTypes                map[symbols.SymbolID]types.TypeID
	constState                  map[symbols.SymbolID]constEvalState
	typeItems                   map[ast.ItemID]types.TypeID
	typeCache                   map[typeCacheKey]types.TypeID
	typeKeys                    map[string]types.TypeID
	typeIDItems                 map[types.TypeID]ast.ItemID
	structBases                 map[types.TypeID]types.TypeID
	externFields                map[symbols.TypeKey]*externFieldSet
	typeAttrs                   map[types.TypeID][]AttrInfo // Type attribute storage
	fieldAttrs                  map[fieldKey][]AttrInfo     // Field attribute storage
	awaitDepth                  int
	returnStack                 []returnContext
	typeParams                  []map[source.StringID]types.TypeID
	typeParamNames              map[types.TypeID]source.StringID
	typeParamEnv                []uint32
	nextParamEnv                uint32
	typeInstantiations          map[string]types.TypeID
	typeInstantiationInProgress map[string]struct{} // tracks cycles during type instantiation
	typeNames                   map[types.TypeID]string
	fnInstantiationSeen         map[string]struct{}
	exportNames                 map[source.StringID]string
	typeParamBounds             map[types.TypeID][]symbols.BoundInstance
	typeParamStack              []types.TypeID
	typeParamMarks              []int
	arrayName                   source.StringID
	arraySymbol                 symbols.SymbolID
	arrayType                   types.TypeID
	arrayFixedName              source.StringID
	arrayFixedSymbol            symbols.SymbolID
	arrayFixedType              types.TypeID
	fnConcurrencySummaries      map[symbols.SymbolID]*FnConcurrencySummary
}

type returnContext struct {
	expected types.TypeID
	span     source.Span
	collect  *[]types.TypeID
}

type returnStatus int

const (
	returnOpen returnStatus = iota
	returnClosed
)

func (tc *typeChecker) run() {
	if tc.builder == nil || tc.result == nil || tc.types == nil {
		return
	}

	// Create root span for sema if tracing is enabled
	var rootSpan *trace.Span
	if tc.tracer != nil && tc.tracer.Enabled() {
		rootSpan = trace.Begin(tc.tracer, trace.ScopePass, "sema_check", 0)
		defer rootSpan.End("")
	}

	// Helper для создания phase spans
	phase := func(name string) func() {
		if tc.tracer == nil || !tc.tracer.Level().ShouldEmit(trace.ScopePass) {
			return func() {}
		}
		var parentID uint64
		if rootSpan != nil {
			parentID = rootSpan.ID()
		}
		span := trace.Begin(tc.tracer, trace.ScopePass, name, parentID)
		return func() { span.End("") }
	}

	done := phase("build_magic_index")
	tc.buildMagicIndex()
	done()

	done = phase("ensure_builtin_magic")
	tc.ensureBuiltinMagic()
	done()

	done = phase("build_scope_index")
	tc.buildScopeIndex()
	done()

	done = phase("build_symbol_index")
	tc.buildSymbolIndex()
	if tc.symbols != nil {
		tc.externSymbols = tc.symbols.ExternSyms
	}
	done()

	done = phase("build_export_indexes")
	tc.buildExportNameIndexes()
	done()

	// Initialize state
	tc.borrow = NewBorrowTable()
	tc.bindingBorrow = make(map[symbols.SymbolID]BorrowID)
	tc.bindingTypes = make(map[symbols.SymbolID]types.TypeID)
	tc.constState = make(map[symbols.SymbolID]constEvalState)
	tc.typeItems = make(map[ast.ItemID]types.TypeID)
	tc.typeCache = make(map[typeCacheKey]types.TypeID)
	tc.typeKeys = make(map[string]types.TypeID)
	tc.typeIDItems = make(map[types.TypeID]ast.ItemID)
	tc.structBases = make(map[types.TypeID]types.TypeID)
	tc.externFields = make(map[symbols.TypeKey]*externFieldSet)
	tc.typeParamNames = make(map[types.TypeID]source.StringID)
	tc.typeParamBounds = make(map[types.TypeID][]symbols.BoundInstance)
	tc.typeParamMarks = tc.typeParamMarks[:0]
	tc.nextParamEnv = 1
	tc.typeInstantiations = make(map[string]types.TypeID)
	tc.typeInstantiationInProgress = make(map[string]struct{})
	tc.fnInstantiationSeen = make(map[string]struct{})
	tc.fnConcurrencySummaries = make(map[symbols.SymbolID]*FnConcurrencySummary)

	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}

	done = phase("register_types")
	tc.ensureBuiltinArrayType()
	files := []*ast.File{file}
	if tc.symbols != nil && len(tc.symbols.ModuleFiles) > 0 {
		for fid := range tc.symbols.ModuleFiles {
			if fid == tc.fileID {
				continue
			}
			if f := tc.builder.Files.Get(fid); f != nil {
				files = append(files, f)
			}
		}
	}
	for _, f := range files {
		tc.registerTypeDecls(f)
	}
	for _, f := range files {
		tc.populateTypeDecls(f)
	}
	for _, f := range files {
		tc.collectExternFields(f)
	}
	done()

	done = phase("walk_items")
	root := tc.fileScope()
	rootPushed := tc.pushScope(root)
	for _, itemID := range file.Items {
		tc.walkItem(itemID)
	}
	if rootPushed {
		tc.leaveScope()
	}
	done()

	done = phase("flush_borrow")
	tc.flushBorrowResults()
	done()
}
