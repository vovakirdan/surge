package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"

	"fortio.org/safecast"
)

type typeCacheKey struct {
	Type  ast.TypeID
	Scope symbols.ScopeID
	Env   uint32
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

	scopeStack         []symbols.ScopeID
	scopeByItem        map[ast.ItemID]symbols.ScopeID
	scopeByStmt        map[ast.StmtID]symbols.ScopeID
	stmtSymbols        map[ast.StmtID]symbols.SymbolID
	bindingBorrow      map[symbols.SymbolID]BorrowID
	bindingTypes       map[symbols.SymbolID]types.TypeID
	typeItems          map[ast.ItemID]types.TypeID
	typeCache          map[typeCacheKey]types.TypeID
	typeKeys           map[string]types.TypeID
	returnStack        []returnContext
	typeParams         []map[source.StringID]types.TypeID
	typeParamNames     map[types.TypeID]source.StringID
	typeParamEnv       []uint32
	nextParamEnv       uint32
	typeInstantiations map[string]types.TypeID
}

type returnContext struct {
	expected types.TypeID
	span     source.Span
}

func (tc *typeChecker) run() {
	if tc.builder == nil || tc.result == nil || tc.types == nil {
		return
	}
	tc.buildMagicIndex()
	tc.ensureBuiltinMagic()
	tc.buildScopeIndex()
	tc.buildSymbolIndex()
	tc.borrow = NewBorrowTable()
	tc.bindingBorrow = make(map[symbols.SymbolID]BorrowID)
	tc.bindingTypes = make(map[symbols.SymbolID]types.TypeID)
	tc.typeItems = make(map[ast.ItemID]types.TypeID)
	tc.typeCache = make(map[typeCacheKey]types.TypeID)
	tc.typeKeys = make(map[string]types.TypeID)
	tc.typeParamNames = make(map[types.TypeID]source.StringID)
	tc.nextParamEnv = 1
	tc.typeInstantiations = make(map[string]types.TypeID)
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	tc.registerTypeDecls(file)
	tc.populateTypeDecls(file)
	root := tc.fileScope()
	rootPushed := tc.pushScope(root)
	for _, itemID := range file.Items {
		tc.walkItem(itemID)
	}
	if rootPushed {
		tc.leaveScope()
	}
	tc.flushBorrowResults()
}

func (tc *typeChecker) walkItem(id ast.ItemID) {
	item := tc.builder.Items.Get(id)
	if item == nil {
		return
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
	case ast.ItemFn:
		fnItem, ok := tc.builder.Items.Fn(id)
		if !ok || fnItem == nil || !fnItem.Body.IsValid() {
			return
		}
		scope := tc.scopeForItem(id)
		returnType := tc.functionReturnType(fnItem, scope)
		returnSpan := fnItem.ReturnSpan
		if returnSpan == (source.Span{}) {
			returnSpan = fnItem.Span
		}
		tc.pushReturnContext(returnType, returnSpan)
		tc.registerFnParamTypes(id, fnItem)
		pushed := tc.pushScope(scope)
		tc.walkStmt(fnItem.Body)
		if pushed {
			tc.leaveScope()
		}
		tc.popReturnContext()
	default:
		// Other item kinds are currently ignored.
	}
}

func (tc *typeChecker) walkStmt(id ast.StmtID) {
	stmt := tc.builder.Stmts.Get(id)
	if stmt == nil {
		return
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
			tc.typeExpr(ifStmt.Cond)
			tc.walkStmt(ifStmt.Then)
			if ifStmt.Else.IsValid() {
				tc.walkStmt(ifStmt.Else)
			}
		}
	case ast.StmtWhile:
		if whileStmt := tc.builder.Stmts.While(id); whileStmt != nil {
			tc.typeExpr(whileStmt.Cond)
			tc.walkStmt(whileStmt.Body)
		}
	case ast.StmtForClassic:
		if forStmt := tc.builder.Stmts.ForClassic(id); forStmt != nil {
			scope := tc.scopeForStmt(id)
			pushed := tc.pushScope(scope)
			if forStmt.Init.IsValid() {
				tc.walkStmt(forStmt.Init)
			}
			tc.typeExpr(forStmt.Cond)
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
			tc.typeExpr(forIn.Iterable)
			tc.walkStmt(forIn.Body)
			if pushed {
				tc.leaveScope()
			}
		}
	case ast.StmtSignal:
		if signal := tc.builder.Stmts.Signal(id); signal != nil {
			tc.typeExpr(signal.Value)
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
	if declared == types.NoTypeID || actual == types.NoTypeID {
		return
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

func (tc *typeChecker) pushReturnContext(expected types.TypeID, span source.Span) {
	ctx := returnContext{expected: expected, span: span}
	tc.returnStack = append(tc.returnStack, ctx)
}

func (tc *typeChecker) popReturnContext() {
	if len(tc.returnStack) == 0 {
		return
	}
	tc.returnStack = tc.returnStack[:len(tc.returnStack)-1]
}

func (tc *typeChecker) currentReturnContext() *returnContext {
	if len(tc.returnStack) == 0 {
		return nil
	}
	return &tc.returnStack[len(tc.returnStack)-1]
}

func (tc *typeChecker) pushTypeParams(owner symbols.SymbolID, names []source.StringID, bindings []types.TypeID) bool {
	if len(names) == 0 || tc.types == nil {
		return false
	}
	if len(bindings) > 0 && len(bindings) != len(names) {
		return false
	}
	scope := make(map[source.StringID]types.TypeID, len(names))
	for i, name := range names {
		var id types.TypeID
		if len(bindings) > 0 {
			id = bindings[i]
		} else {
			ui32, err := safecast.Conv[uint32](i)
			if err != nil {
				panic(fmt.Errorf("type param index overflow: %w", err))
			}
			id = tc.types.RegisterTypeParam(name, uint32(owner), ui32)
			tc.typeParamNames[id] = name
		}
		scope[name] = id
	}
	tc.typeParams = append(tc.typeParams, scope)
	tc.typeParamEnv = append(tc.typeParamEnv, tc.nextParamEnv)
	tc.nextParamEnv++
	return true
}

func (tc *typeChecker) popTypeParams() {
	if len(tc.typeParams) == 0 {
		return
	}
	tc.typeParams = tc.typeParams[:len(tc.typeParams)-1]
	if len(tc.typeParamEnv) > 0 {
		tc.typeParamEnv = tc.typeParamEnv[:len(tc.typeParamEnv)-1]
	}
}

func (tc *typeChecker) currentTypeParamEnv() uint32 {
	if len(tc.typeParamEnv) == 0 {
		return 0
	}
	return tc.typeParamEnv[len(tc.typeParamEnv)-1]
}

func (tc *typeChecker) lookupTypeParam(name source.StringID) types.TypeID {
	if name == source.NoStringID {
		return types.NoTypeID
	}
	for i := len(tc.typeParams) - 1; i >= 0; i-- {
		scope := tc.typeParams[i]
		if id, ok := scope[name]; ok {
			return id
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) validateReturn(span source.Span, expr ast.ExprID, actual types.TypeID) {
	ctx := tc.currentReturnContext()
	if ctx == nil || tc.types == nil {
		return
	}
	expected := ctx.expected
	if expected == types.NoTypeID {
		expected = tc.types.Builtins().Nothing
	}
	nothing := tc.types.Builtins().Nothing
	if !expr.IsValid() {
		if expected != nothing {
			tc.report(diag.SemaTypeMismatch, span, "return value must have type %s", tc.typeLabel(expected))
		}
		return
	}
	if expected == nothing {
		if actual == nothing {
			return
		}
		tc.report(diag.SemaTypeMismatch, span, "function returning nothing cannot return a value")
		return
	}
	if actual == types.NoTypeID {
		return
	}
	if !tc.typesAssignable(expected, actual, false) {
		tc.report(diag.SemaTypeMismatch, span, "return type mismatch: expected %s, got %s", tc.typeLabel(expected), tc.typeLabel(actual))
	}
}

func (tc *typeChecker) typesAssignable(expected, actual types.TypeID, allowAlias bool) bool {
	if expected == actual {
		return true
	}
	if allowAlias {
		return tc.resolveAlias(expected) == tc.resolveAlias(actual)
	}
	return false
}
