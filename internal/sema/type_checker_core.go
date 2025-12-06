package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
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

func (tc *typeChecker) walkItem(id ast.ItemID) {
	item := tc.builder.Items.Get(id)
	if item == nil {
		return
	}

	var span *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDetail {
		span = trace.Begin(tc.tracer, trace.ScopeModule, "walk_item", 0)
		span.WithExtra("kind", fmt.Sprintf("%d", item.Kind))
		defer span.End("")
	}

	switch item.Kind {
	case ast.ItemLet:
		letItem, ok := tc.builder.Items.Let(id)
		if !ok || letItem == nil {
			return
		}
		scope := tc.scopeForItem(id)
		symID := tc.typeSymbolForItem(id)
		declaredType := tc.resolveTypeExprWithScope(letItem.Type, scope)
		if declaredType != types.NoTypeID {
			tc.setBindingType(symID, declaredType)
		}
		if !letItem.Value.IsValid() {
			return
		}
		valueType := tc.typeExpr(letItem.Value)
		tc.observeMove(letItem.Value, tc.exprSpan(letItem.Value))
		tc.ensureBindingTypeMatch(letItem.Type, declaredType, valueType, letItem.Value)
		if declaredType == types.NoTypeID {
			tc.setBindingType(symID, valueType)
		}
		tc.updateItemBinding(id, letItem.Value)
	case ast.ItemConst:
		symID := tc.typeSymbolForItem(id)
		if symID.IsValid() {
			tc.ensureConstEvaluated(symID)
		} else if constItem, ok := tc.builder.Items.Const(id); ok && constItem != nil && constItem.Value.IsValid() {
			tc.typeExpr(constItem.Value)
		}
	case ast.ItemFn:
		fnItem, ok := tc.builder.Items.Fn(id)
		if !ok || fnItem == nil {
			return
		}
		scope := tc.scopeForItem(id)
		symID := tc.typeSymbolForItem(id)
		paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetFnTypeParamIDs(fnItem), scope)
		if len(paramSpecs) == 0 && len(fnItem.Generics) > 0 {
			paramSpecs = specsFromNames(fnItem.Generics)
		}
		typeParamsPushed := tc.pushTypeParams(symID, paramSpecs, nil)
		if paramIDs := tc.builder.Items.GetFnTypeParamIDs(fnItem); len(paramIDs) > 0 {
			bounds := tc.resolveTypeParamBounds(paramIDs, scope, nil)
			tc.attachTypeParamSymbols(symID, bounds)
			tc.applyTypeParamBounds(symID)
		}
		returnType := tc.functionReturnType(fnItem, scope)
		returnSpan := fnItem.ReturnSpan
		if returnSpan == (source.Span{}) {
			returnSpan = fnItem.Span
		}
		tc.registerFnParamTypes(id, fnItem)
		if fnItem.Body.IsValid() {
			tc.pushReturnContext(returnType, returnSpan, nil)
			if fnItem.Flags&ast.FnModifierAsync != 0 {
				tc.awaitDepth++
			}
			pushed := tc.pushScope(scope)
			tc.walkStmt(fnItem.Body)
			if returnType != tc.types.Builtins().Nothing && tc.returnStatus(fnItem.Body) != returnClosed {
				tc.report(diag.SemaMissingReturn, returnSpan, "function returning %s is missing a return", tc.typeLabel(returnType))
			}
			if pushed {
				tc.leaveScope()
			}
			tc.popReturnContext()
			if fnItem.Flags&ast.FnModifierAsync != 0 {
				tc.awaitDepth--
			}
		}
		// Validate function attributes
		tc.validateFunctionAttrs(fnItem, types.NoTypeID)
		if typeParamsPushed {
			tc.popTypeParams()
		}
	case ast.ItemExtern:
		if block, ok := tc.builder.Items.Extern(id); ok && block != nil {
			tc.checkExternFns(id, block)
		}
	case ast.ItemContract:
		if contract, ok := tc.builder.Items.Contract(id); ok && contract != nil {
			tc.checkContract(id, contract)
		}
	case ast.ItemTag:
		if tag, ok := tc.builder.Items.Tag(id); ok && tag != nil {
			tc.checkTag(id, tag)
		}
	default:
		// Other item kinds are currently ignored.
	}
}

func (tc *typeChecker) walkStmt(id ast.StmtID) {
	stmt := tc.builder.Stmts.Get(id)
	if stmt == nil {
		return
	}

	var span *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDebug {
		span = trace.Begin(tc.tracer, trace.ScopeNode, "walk_stmt", 0)
		span.WithExtra("kind", fmt.Sprintf("%d", stmt.Kind))
		defer span.End("")
	}

	switch stmt.Kind {
	case ast.StmtBlock:
		if block := tc.builder.Stmts.Block(id); block != nil {
			scope := tc.scopeForStmt(id)
			pushed := tc.pushScope(scope)
			for _, child := range block.Stmts {
				tc.walkStmt(child)
			}
			if pushed {
				tc.leaveScope()
			}
		}
	case ast.StmtLet:
		if letStmt := tc.builder.Stmts.Let(id); letStmt != nil {
			scope := tc.scopeForStmt(id)

			// Check if this is a tuple pattern or simple binding
			if letStmt.Pattern.IsValid() {
				// Tuple destructuring: let (x, y) = value
				valueType := tc.typeExpr(letStmt.Value)
				tc.observeMove(letStmt.Value, tc.exprSpan(letStmt.Value))
				tc.bindTuplePattern(letStmt.Pattern, valueType, scope)
			} else {
				// Simple binding: let x = value
				symID := tc.symbolForStmt(id)
				declaredType := tc.resolveTypeExprWithScope(letStmt.Type, scope)
				if declaredType != types.NoTypeID {
					tc.setBindingType(symID, declaredType)
				}
				if letStmt.Value.IsValid() {
					valueType := tc.typeExpr(letStmt.Value)
					tc.observeMove(letStmt.Value, tc.exprSpan(letStmt.Value))
					tc.ensureBindingTypeMatch(letStmt.Type, declaredType, valueType, letStmt.Value)
					if declaredType == types.NoTypeID {
						tc.setBindingType(symID, valueType)
					}
					tc.updateStmtBinding(id, letStmt.Value)
				}
			}
		}
	case ast.StmtConst:
		symID := tc.symbolForStmt(id)
		if symID.IsValid() {
			tc.ensureConstEvaluated(symID)
		} else if constStmt := tc.builder.Stmts.Const(id); constStmt != nil && constStmt.Value.IsValid() {
			tc.typeExpr(constStmt.Value)
		}
	case ast.StmtExpr:
		if exprStmt := tc.builder.Stmts.Expr(id); exprStmt != nil {
			tc.typeExpr(exprStmt.Expr)
		}
	case ast.StmtReturn:
		if ret := tc.builder.Stmts.Return(id); ret != nil {
			var valueType types.TypeID
			if ret.Expr.IsValid() {
				valueType = tc.typeExpr(ret.Expr)
				tc.observeMove(ret.Expr, tc.exprSpan(ret.Expr))
			}
			tc.validateReturn(stmt.Span, ret.Expr, valueType)
		}
	case ast.StmtIf:
		if ifStmt := tc.builder.Stmts.If(id); ifStmt != nil {
			tc.ensureBoolContext(ifStmt.Cond, tc.exprSpan(ifStmt.Cond))
			tc.walkStmt(ifStmt.Then)
			if ifStmt.Else.IsValid() {
				tc.walkStmt(ifStmt.Else)
			}
		}
	case ast.StmtWhile:
		if whileStmt := tc.builder.Stmts.While(id); whileStmt != nil {
			tc.ensureBoolContext(whileStmt.Cond, tc.exprSpan(whileStmt.Cond))
			tc.walkStmt(whileStmt.Body)
		}
	case ast.StmtForClassic:
		if forStmt := tc.builder.Stmts.ForClassic(id); forStmt != nil {
			scope := tc.scopeForStmt(id)
			pushed := tc.pushScope(scope)
			if forStmt.Init.IsValid() {
				tc.walkStmt(forStmt.Init)
			}
			tc.ensureBoolContext(forStmt.Cond, tc.exprSpan(forStmt.Cond))
			tc.typeExpr(forStmt.Post)
			tc.walkStmt(forStmt.Body)
			if pushed {
				tc.leaveScope()
			}
		}
	case ast.StmtForIn:
		if forIn := tc.builder.Stmts.ForIn(id); forIn != nil {
			scope := tc.scopeForStmt(id)
			pushed := tc.pushScope(scope)

			// 1. Get iterable type
			iterableType := tc.typeExpr(forIn.Iterable)

			// 2. Determine element type
			var elemType types.TypeID

			// 2a. Explicit type annotation
			if forIn.Type.IsValid() {
				elemType = tc.resolveTypeExprWithScope(forIn.Type, scope)
			}

			// 2b. Infer from iterable
			if elemType == types.NoTypeID && iterableType != types.NoTypeID {
				elemType = tc.inferForInElementType(iterableType, stmt.Span)
			}

			// 3. Assign type to loop variable symbol
			if forIn.Pattern != source.NoStringID {
				if symID := tc.stmtSymbols[id]; symID.IsValid() && elemType != types.NoTypeID {
					tc.bindingTypes[symID] = elemType
				}
			}

			tc.walkStmt(forIn.Body)
			if pushed {
				tc.leaveScope()
			}
		}
	case ast.StmtSignal:
		if signal := tc.builder.Stmts.Signal(id); signal != nil {
			tc.reporter.Report(diag.FutSignalNotSupported, diag.SevError, stmt.Span, "'signal' is not supported in v1, reserved for future use", nil, nil)
		}
	case ast.StmtDrop:
		if drop := tc.builder.Stmts.Drop(id); drop != nil {
			tc.handleDrop(drop.Expr, stmt.Span)
		}
	default:
		// StmtBreak / StmtContinue and others have no expressions to type.
	}
}

func (tc *typeChecker) ensureBindingTypeMatch(typeExpr ast.TypeID, declared, actual types.TypeID, valueExpr ast.ExprID) {
	if declared == types.NoTypeID {
		return
	}
	if data, ok := tc.builder.Exprs.Struct(valueExpr); ok && data != nil && !data.Type.IsValid() {
		tc.validateStructLiteralFields(declared, data, tc.exprSpan(valueExpr))
	}
	if actual == types.NoTypeID {
		return
	}
	actual = tc.coerceLiteralForBinding(declared, actual, valueExpr)
	if expElem, expLen, expFixed, okExp := tc.arrayInfo(declared); okExp {
		if actElem, actLen, actFixed, okAct := tc.arrayInfo(actual); okAct && tc.typesAssignable(expElem, actElem, true) {
			if expFixed {
				if actFixed && expLen == actLen {
					return
				}
				if !actFixed && valueExpr.IsValid() {
					if arr, okArr := tc.builder.Exprs.Array(valueExpr); okArr && arr != nil {
						if l, err := safecast.Conv[uint32](len(arr.Elements)); err == nil && l == expLen {
							return
						}
					}
				}
			} else {
				return
			}
		}
	}
	if tc.typesAssignable(declared, actual, true) {
		return
	}
	tc.reportBindingTypeMismatch(typeExpr, declared, actual, valueExpr)
}

func (tc *typeChecker) reportBindingTypeMismatch(typeExpr ast.TypeID, expected, actual types.TypeID, valueExpr ast.ExprID) {
	if tc.reporter == nil {
		return
	}
	expectedLabel := tc.typeLabel(expected)
	actualLabel := tc.typeLabel(actual)
	primary := tc.exprSpan(valueExpr)
	if primary == (source.Span{}) {
		primary = tc.typeSpan(typeExpr)
	}
	msg := fmt.Sprintf("cannot assign %s to %s", actualLabel, expectedLabel)
	b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, primary, msg)
	if b == nil {
		return
	}
	if typeSpan := tc.typeSpan(typeExpr); typeSpan != (source.Span{}) {
		changeType := fix.ReplaceSpan(
			fmt.Sprintf("change variable type to %s", actualLabel),
			typeSpan,
			actualLabel,
			"",
			fix.WithKind(diag.FixKindRefactor),
		)
		b.WithFixSuggestion(changeType)
	}
	if insertSpan := tc.exprSpan(valueExpr); insertSpan != (source.Span{}) {
		cast := fix.InsertText(
			fmt.Sprintf("cast expression to %s", expectedLabel),
			insertSpan.ZeroideToEnd(),
			" to "+expectedLabel,
			"",
			fix.WithKind(diag.FixKindRefactorRewrite),
			fix.WithApplicability(diag.FixApplicabilityManualReview),
		)
		b.WithFixSuggestion(cast)
	}
	b.Emit()
}

func (tc *typeChecker) bindTuplePattern(pattern ast.ExprID, valueType types.TypeID, scope symbols.ScopeID) {
	_ = scope // TODO: use scope for symbol registration in future
	tuple, ok := tc.builder.Exprs.Tuple(pattern)
	if !ok || tuple == nil {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(pattern), "expected tuple pattern")
		return
	}

	info, ok := tc.types.TupleInfo(tc.valueType(valueType))
	if !ok {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(pattern), "cannot destructure %s as tuple", tc.typeLabel(valueType))
		return
	}

	if len(tuple.Elements) != len(info.Elems) {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(pattern),
			"pattern has %d elements but tuple has %d", len(tuple.Elements), len(info.Elems))
		return
	}

	// For now, just validate that all elements are identifiers
	// Symbol registration for tuple pattern variables would need to be done in a symbol collection pass
	for i, elem := range tuple.Elements {
		ident, ok := tc.builder.Exprs.Ident(elem)
		if !ok || ident == nil {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(elem), "expected identifier in pattern")
			continue
		}
		// Type inference: associate each element with its type from the tuple
		tc.result.ExprTypes[elem] = info.Elems[i]
	}
}

func (tc *typeChecker) typeSpan(id ast.TypeID) source.Span {
	if !id.IsValid() || tc.builder == nil {
		return source.Span{}
	}
	typ := tc.builder.Types.Get(id)
	if typ == nil {
		return source.Span{}
	}
	return typ.Span
}

func (tc *typeChecker) symbolForStmt(id ast.StmtID) symbols.SymbolID {
	if tc.stmtSymbols == nil {
		return symbols.NoSymbolID
	}
	return tc.stmtSymbols[id]
}

func (tc *typeChecker) symbolForExtern(id ast.ExternMemberID) symbols.SymbolID {
	if tc.externSymbols == nil {
		return symbols.NoSymbolID
	}
	return tc.externSymbols[id]
}

func (tc *typeChecker) functionReturnType(fn *ast.FnItem, scope symbols.ScopeID) types.TypeID {
	if tc.types == nil || fn == nil {
		return types.NoTypeID
	}
	expected := tc.types.Builtins().Nothing
	if fn.ReturnType.IsValid() {
		if resolved := tc.resolveTypeExprWithScope(fn.ReturnType, scope); resolved != types.NoTypeID {
			expected = resolved
		}
	}
	return expected
}

func (tc *typeChecker) buildExportNameIndexes() {
	if tc.exports == nil {
		return
	}
	tc.typeNames = make(map[types.TypeID]string)
	tc.exportNames = make(map[source.StringID]string)
	for _, module := range tc.exports {
		if module == nil {
			continue
		}
		for _, list := range module.Symbols {
			for i := range list {
				sym := &list[i]
				if sym.NameID != source.NoStringID && sym.Name != "" {
					if _, ok := tc.exportNames[sym.NameID]; !ok {
						tc.exportNames[sym.NameID] = sym.Name
					}
				}
				if sym.Kind == symbols.SymbolType && sym.Type != types.NoTypeID {
					tc.recordTypeName(sym.Type, sym.Name)
					if tc.typeKeys != nil && sym.Name != "" {
						tc.typeKeys[sym.Name] = sym.Type
					}
				}
			}
		}
	}
}

func (tc *typeChecker) lookupTypeName(typeID types.TypeID, nameID source.StringID) string {
	if tc.typeNames != nil {
		if name := tc.typeNames[tc.resolveAlias(typeID)]; name != "" {
			return name
		}
	}
	if tc.exportNames != nil {
		if name := tc.exportNames[nameID]; name != "" {
			return name
		}
	}
	if name := tc.lookupName(nameID); name != "" {
		return name
	}
	return ""
}

func (tc *typeChecker) lookupExportedName(id source.StringID) string {
	if name := tc.lookupName(id); name != "" {
		return name
	}
	if tc.exportNames != nil {
		return tc.exportNames[id]
	}
	return ""
}

func (tc *typeChecker) recordTypeName(id types.TypeID, name string) {
	if id == types.NoTypeID || name == "" {
		return
	}
	if tc.typeNames == nil {
		tc.typeNames = make(map[types.TypeID]string)
	}
	if _, ok := tc.typeNames[id]; !ok {
		tc.typeNames[id] = name
	}
}

func (tc *typeChecker) typesAssignable(expected, actual types.TypeID, allowAlias bool) bool {
	if expected == actual {
		return true
	}
	if allowAlias {
		if tc.resolveAlias(expected) == tc.resolveAlias(actual) {
			return true
		}
	}
	if expElem, expLen, expFixed, okExp := tc.arrayInfo(expected); okExp {
		if actElem, actLen, actFixed, okAct := tc.arrayInfo(actual); okAct && tc.typesAssignable(expElem, actElem, true) {
			if expFixed {
				return actFixed && expLen == actLen
			}
			return true
		}
	}
	// Tuple assignability
	expInfo, expOk := tc.types.TupleInfo(expected)
	actInfo, actOk := tc.types.TupleInfo(actual)
	if expOk && actOk {
		if len(expInfo.Elems) != len(actInfo.Elems) {
			return false
		}
		for i := range expInfo.Elems {
			if !tc.typesAssignable(expInfo.Elems[i], actInfo.Elems[i], allowAlias) {
				return false
			}
		}
		return true
	}
	if tc.numericWidenable(actual, expected) {
		return true
	}
	return false
}

// inferForInElementType extracts the element type from an iterable.
// It checks for __range method or direct Range<T> type.
func (tc *typeChecker) inferForInElementType(iterableType types.TypeID, span source.Span) types.TypeID {
	if iterableType == types.NoTypeID {
		return types.NoTypeID
	}

	// Case 1: Iterable is already Range<T>
	if elem, ok := tc.rangePayload(iterableType); ok {
		return elem
	}

	// Case 2: Arrays have known element types
	if elem, ok := tc.arrayElemType(iterableType); ok {
		return elem
	}

	// Case 3: Check for __range magic method
	rangeType := tc.lookupRangeMethodResult(iterableType)
	if rangeType != types.NoTypeID {
		if elem, ok := tc.rangePayload(rangeType); ok {
			return elem
		}
	}

	// If no __range method found, emit diagnostic
	tc.report(diag.SemaIteratorNotImplemented, span,
		"type %s does not implement iterator (missing __range method)",
		tc.typeLabel(iterableType))
	return types.NoTypeID
}

// lookupRangeMethodResult looks up the __range method for a type and returns its result type.
func (tc *typeChecker) lookupRangeMethodResult(containerType types.TypeID) types.TypeID {
	if containerType == types.NoTypeID {
		return types.NoTypeID
	}

	for _, cand := range tc.typeKeyCandidates(containerType) {
		if cand.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(cand.key, "__range")
		for _, sig := range methods {
			if sig != nil && sig.Result != "" {
				if res := tc.typeFromKey(sig.Result); res != types.NoTypeID {
					return res
				}
			}
		}
	}
	return types.NoTypeID
}
