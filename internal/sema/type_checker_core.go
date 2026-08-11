package sema

import (
	"sort"

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

// assignabilityKey uniquely identifies a type assignability check to prevent infinite recursion
type assignabilityKey struct {
	Expected types.TypeID
	Actual   types.TypeID
}

type typeChecker struct {
	builder        *ast.Builder
	fileID         ast.FileID
	reporter       diag.Reporter
	errorCount     int
	symbols        *symbols.Result
	result         *Result
	types          *types.Interner
	exports        map[string]*symbols.ModuleExports
	modulePath     string
	magic          map[symbols.TypeKey]map[string][]*symbols.FunctionSignature
	magicSymbols   map[*symbols.FunctionSignature]symbols.SymbolID
	magicDecls     []magicDeclaration
	borrow         *BorrowTable
	borrowEvents   []BorrowEvent
	borrowBindings map[BorrowID]symbols.SymbolID
	// twoPhaseEligible marks direct `&mut` argument expressions of the call
	// whose argument list is currently being checked; their borrows reserve
	// instead of activating (see two_phase_borrow.go).
	twoPhaseEligible map[ast.ExprID]*twoPhaseFrame
	copyTypes        map[types.TypeID]struct{}
	insts            InstantiationRecorder

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
	rawPointerChecked           map[ast.TypeID]struct{}
	allowRawPointer             bool
	typeKeys                    map[string]types.TypeID
	typeIDItems                 map[types.TypeID]ast.ItemID
	structBases                 map[types.TypeID]types.TypeID
	externFields                map[symbols.TypeKey]*externFieldSet
	externSealedBlocks          map[ast.ItemID]struct{}
	typeAttrs                   map[types.TypeID][]AttrInfo     // Type attribute storage
	fieldAttrs                  map[fieldKey][]AttrInfo         // Field attribute storage
	symbolAttrs                 map[symbols.SymbolID][]AttrInfo // Symbol attribute storage (functions, let, const)
	awaitDepth                  int
	asyncBlockDepth             int // Track nesting level of async blocks for error differentiation
	returnStack                 []returnContext
	fnSymStack                  []symbols.SymbolID // current function (for instantiation use-sites)
	fnParamsStack               [][]symbols.SymbolID
	typeParams                  []map[source.StringID]types.TypeID
	typeParamNames              map[types.TypeID]source.StringID
	typeParamEnv                []uint32
	nextParamEnv                uint32
	typeInstantiations          map[string]types.TypeID
	typeInstantiationInProgress map[string]struct{}           // tracks cycles during type instantiation
	assignabilityInProgress     map[assignabilityKey]struct{} // tracks cycles during type assignability checks
	typeNames                   map[types.TypeID]string
	exportNames                 map[source.StringID]string
	typeParamBounds             map[types.TypeID][]symbols.BoundInstance
	typeParamStack              []types.TypeID
	typeParamMarks              []int
	expectedExpr                ast.ExprID
	expectedType                types.TypeID
	discardedExprs              []ast.ExprID
	compareGuardBindings        [][]symbols.SymbolID
	// Set only while the SCRUTINEE of a compare is being observed. Reading a
	// heap-owning value through a shared reference is refused everywhere else
	// because it makes a second owner; a compare only inspects its subject, so
	// `compare *arg { ... }` takes nothing and is the one position that keeps
	// accepting it. See observeMove and rejectMoveOutOfSharedBorrow.
	observingCompareScrutinee bool
	blockResultExprs          map[ast.ExprID][]ast.ExprID
	arrayName                 source.StringID
	arraySymbol               symbols.SymbolID
	arrayType                 types.TypeID
	arrayFixedName            source.StringID
	arrayFixedSymbol          symbols.SymbolID
	arrayFixedType            types.TypeID
	mapName                   source.StringID
	mapSymbol                 symbols.SymbolID
	mapType                   types.TypeID
	fnConcurrencySummaries    map[symbols.SymbolID]*FnConcurrencySummary
	lockOrderGraph            *LockOrderGraph // Global lock ordering for deadlock detection
	taskTracker               *TaskTracker    // Task tracking for structured concurrency
	localTaskBindings         map[symbols.SymbolID]struct{}
	taskContainers            map[Place]*taskContainerInfo
	taskContainerLoops        []taskContainerLoop
	addressOfOperands         map[ast.ExprID]struct{} // Tracks operands of & expressions (for @atomic validation)
	arrayViewExprs            map[ast.ExprID]struct{}
	arrayViewBindings         map[symbols.SymbolID]struct{}
	assignmentLHSDepth        int
	// placeBaseDepth counts how deep we are inside the BASE CHAIN of a
	// projection being read. `o` in `o.label` is not a value read — only the
	// projection is — so the use-after-move question is asked once, about the
	// whole place, rather than about every binding the path walks through.
	placeBaseDepth int
	// movedPlaces records where each moved PLACE gave its value away. Keyed by
	// place rather than by binding so a field can be tracked apart from its
	// container; at present only whole-binding places (empty path) are
	// reachable, because the partial-move gate rejects the rest.
	movedPlaces            map[Place]source.Span
	dropScopes             []dropScope                     // lexical scopes' droppable bindings (drop obligations)
	loopDropMarks          []int                           // dropScopes depth at each enclosing loop body
	tempFrames             []tempFrame                     // per-statement owned-rvalue candidates (temp drops)
	tempTaken              map[ast.ExprID][][]PlaceSegment // paths moved OUT of a statement-end temporary, so its release can be narrowed to the remainder
	choiceOwnsItsValue     map[ast.ExprID]struct{}         // control-flow expressions every branch of which produced a fresh owned value, so the result is theirs to release
	aliasedBindings        map[symbols.SymbolID]struct{}   // bindings holding container-owned handles (never drop)
	blockingDepth          int                             // nesting depth of `blocking { }` bodies (suspension illegal inside)
	spawnOperand           ast.ExprID                      // the expression `spawn` is currently typing, so an async block can tell whether spawn will scan it
	onCrossingStack        []onAnchorFrame                 // active `on dst { ... }` crossing frames
	directFunctionCrossing map[symbols.SymbolID]struct{}
	functionCrossingEdges  map[symbols.SymbolID]map[symbols.SymbolID]struct{}
	callTargetDepth        int
}

type diagnosticCountingReporter struct {
	inner      diag.Reporter
	errorCount *int
}

// Report forwards diagnostics while counting sema errors for local checkpoints.
func (r *diagnosticCountingReporter) Report(code diag.Code, sev diag.Severity, primary source.Span, msg string, notes []diag.Note, fixes []*diag.Fix) {
	if sev >= diag.SevError && r.errorCount != nil {
		(*r.errorCount)++
	}
	if r.inner != nil {
		r.inner.Report(code, sev, primary, msg, notes, fixes)
	}
}

func (tc *typeChecker) errorCheckpoint() int {
	if tc == nil {
		return 0
	}
	return tc.errorCount
}

func (tc *typeChecker) hasErrorsSince(checkpoint int) bool {
	if tc == nil {
		return false
	}
	return tc.errorCount > checkpoint
}

type returnContext struct {
	kind      returnContextKind
	expected  types.TypeID
	span      source.Span
	collect   *[]collectedResult
	bareRet   *[]source.Span
	discarded bool
}

type collectedResult struct {
	typ    types.TypeID
	span   source.Span
	expr   ast.ExprID
	abrupt bool
}

type returnContextKind uint8

const (
	returnCtxFunction returnContextKind = iota
	returnCtxBlockExpr
	returnCtxTaskPayload
	// returnCtxOnCrossing marks the value-producing body of an `on dst { ... }`
	// placement crossing: `ret` yields the crossed result, while `return` is
	// rejected because it cannot exit through the crossing boundary.
	returnCtxOnCrossing
)

type returnStatus int

const (
	returnOpen returnStatus = iota
	returnClosed
)

func (tc *typeChecker) run() {
	if tc.builder == nil || tc.result == nil || tc.types == nil {
		return
	}
	tc.seedExportedTypeAttrs()

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

	// Prepare type name indexes before consuming exports
	tc.typeKeys = make(map[string]types.TypeID)

	done = phase("build_export_indexes")
	tc.buildExportNameIndexes()
	done()

	// Initialize state
	tc.borrow = NewBorrowTable()
	tc.bindingBorrow = make(map[symbols.SymbolID]BorrowID)
	tc.bindingTypes = make(map[symbols.SymbolID]types.TypeID)
	tc.borrowBindings = make(map[BorrowID]symbols.SymbolID)
	tc.borrowEvents = tc.borrowEvents[:0]
	tc.copyTypes = make(map[types.TypeID]struct{})
	tc.constState = make(map[symbols.SymbolID]constEvalState)
	tc.typeItems = make(map[ast.ItemID]types.TypeID)
	tc.typeCache = make(map[typeCacheKey]types.TypeID)
	tc.rawPointerChecked = make(map[ast.TypeID]struct{})
	tc.typeIDItems = make(map[types.TypeID]ast.ItemID)
	tc.structBases = make(map[types.TypeID]types.TypeID)
	tc.externFields = make(map[symbols.TypeKey]*externFieldSet)
	tc.externSealedBlocks = make(map[ast.ItemID]struct{})
	tc.typeParamNames = make(map[types.TypeID]source.StringID)
	tc.typeParamBounds = make(map[types.TypeID][]symbols.BoundInstance)
	tc.typeParamMarks = tc.typeParamMarks[:0]
	tc.nextParamEnv = 1
	tc.typeInstantiations = make(map[string]types.TypeID)
	tc.typeInstantiationInProgress = make(map[string]struct{})
	tc.assignabilityInProgress = make(map[assignabilityKey]struct{})
	tc.fnConcurrencySummaries = make(map[symbols.SymbolID]*FnConcurrencySummary)
	tc.lockOrderGraph = NewLockOrderGraph()
	tc.taskTracker = NewTaskTracker()
	tc.localTaskBindings = make(map[symbols.SymbolID]struct{})
	tc.blockResultExprs = make(map[ast.ExprID][]ast.ExprID)
	tc.arrayViewExprs = make(map[ast.ExprID]struct{})
	tc.arrayViewBindings = make(map[symbols.SymbolID]struct{})
	tc.movedPlaces = make(map[Place]source.Span)
	tc.taskContainers = make(map[Place]*taskContainerInfo)
	tc.directFunctionCrossing = make(map[symbols.SymbolID]struct{})
	tc.functionCrossingEdges = make(map[symbols.SymbolID]map[symbols.SymbolID]struct{})

	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}

	done = phase("register_types")
	tc.ensureBuiltinArrayType()
	tc.ensureBuiltinMapType()
	files := []*ast.File{file}
	if tc.symbols != nil && len(tc.symbols.ModuleFiles) > 0 {
		ids := make([]ast.FileID, 0, len(tc.symbols.ModuleFiles))
		for fid := range tc.symbols.ModuleFiles {
			if fid != tc.fileID {
				ids = append(ids, fid)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, fid := range ids {
			if f := tc.builder.Files.Get(fid); f != nil {
				files = append(files, f)
			}
		}
	}
	for _, f := range files {
		tc.registerTypeDecls(f)
	}
	// Record type-level attrs early so @copy/@send are visible across files during instantiation.
	for _, f := range files {
		tc.recordTypeDeclAttrs(f)
	}
	for _, f := range files {
		tc.populateTypeDecls(f)
	}
	for _, f := range files {
		tc.collectExternFields(f)
	}
	tc.mergeExternFieldsIntoStructs()
	done()

	done = phase("validate_shard_movable")
	tc.validateShardMovableTypes()
	done()

	// Runs once the alias chains are registered and before any use site is
	// walked, so a hook an alias and its target both answer is refused where it
	// is written rather than where it is silently picked.
	done = phase("check_alias_magic")
	tc.checkAliasMagicDeclarations()
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
	tc.result.rebuildFunctionInstantiations()
	done()
	done = phase("infer_function_effects")
	tc.finalizeFunctionEffects()
	done()

	done = phase("flush_borrow")
	tc.flushBorrowResults()
	done()

	done = phase("check_deadlocks")
	tc.checkForDeadlocks()
	done()

	done = phase("validate_directives")
	tc.validateDirectiveNamespaces()
	done()

	// Export binding types and scopes to result for use by HIR lowering
	tc.result.BindingTypes = tc.bindingTypes
	tc.result.ItemScopes = tc.scopeByItem

	done = phase("validate_layout")
	tc.validateTypeLayouts()
	done()
}
