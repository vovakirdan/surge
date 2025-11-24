package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
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
	constState         map[symbols.SymbolID]constEvalState
	typeItems          map[ast.ItemID]types.TypeID
	typeCache          map[typeCacheKey]types.TypeID
	typeKeys           map[string]types.TypeID
	typeIDItems        map[types.TypeID]ast.ItemID
	structBases        map[types.TypeID]types.TypeID
	externFields       map[symbols.TypeKey]*externFieldSet
	returnStack        []returnContext
	typeParams         []map[source.StringID]types.TypeID
	typeParamNames     map[types.TypeID]source.StringID
	typeParamEnv       []uint32
	nextParamEnv       uint32
	typeInstantiations map[string]types.TypeID
	typeNames          map[types.TypeID]string
	exportNames        map[source.StringID]string
	typeParamBounds    map[types.TypeID][]symbols.BoundInstance
	typeParamStack     []types.TypeID
	typeParamMarks     []int
	arrayName          source.StringID
	arraySymbol        symbols.SymbolID
	arrayType          types.TypeID
	arrayFixedName     source.StringID
	arrayFixedSymbol   symbols.SymbolID
	arrayFixedType     types.TypeID
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
	tc.buildExportNameIndexes()
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
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	tc.ensureBuiltinArrayType()
	tc.registerTypeDecls(file)
	tc.populateTypeDecls(file)
	tc.collectExternFields(file)
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
			tc.pushReturnContext(returnType, returnSpan)
			pushed := tc.pushScope(scope)
			tc.walkStmt(fnItem.Body)
			if pushed {
				tc.leaveScope()
			}
			tc.popReturnContext()
		}
		if typeParamsPushed {
			tc.popTypeParams()
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
	actual = tc.coerceReturnType(expected, actual)
	if !tc.typesAssignable(expected, actual, false) {
		tc.report(diag.SemaTypeMismatch, span, "return type mismatch: expected %s, got %s", tc.typeLabel(expected), tc.typeLabel(actual))
	}
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
	if tc.numericWidenable(actual, expected) {
		return true
	}
	return false
}

func (tc *typeChecker) coerceLiteralForBinding(declared, actual types.TypeID, expr ast.ExprID) types.TypeID {
	if !tc.isLiteralExpr(expr) {
		return actual
	}
	if tc.literalCoercible(declared, actual) {
		return declared
	}
	return actual
}

func (tc *typeChecker) isLiteralExpr(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprLit:
		return true
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(expr); ok && group != nil {
			return tc.isLiteralExpr(group.Inner)
		}
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(expr); ok && data != nil {
			switch data.Op {
			case ast.ExprUnaryPlus, ast.ExprUnaryMinus:
				return tc.isLiteralExpr(data.Operand)
			}
		}
	}
	return false
}

func (tc *typeChecker) literalCoercible(target, from types.TypeID) bool {
	if target == types.NoTypeID || from == types.NoTypeID || tc.types == nil {
		return false
	}
	targetKind, ok := tc.typeKind(target)
	if !ok {
		return false
	}
	sourceKind, ok := tc.typeKind(from)
	if !ok {
		return false
	}
	switch sourceKind {
	case types.KindInt:
		return targetKind == types.KindInt || targetKind == types.KindUint
	case types.KindUint:
		return targetKind == types.KindUint
	case types.KindFloat:
		return targetKind == types.KindFloat
	case types.KindBool:
		return targetKind == types.KindBool
	case types.KindString:
		return targetKind == types.KindString
	default:
		return false
	}
}

func (tc *typeChecker) typeKind(id types.TypeID) (types.Kind, bool) {
	if id == types.NoTypeID || tc.types == nil {
		return types.KindInvalid, false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return types.KindInvalid, false
	}
	return tt.Kind, true
}

func (tc *typeChecker) coerceReturnType(expected, actual types.TypeID) types.TypeID {
	if expected == types.NoTypeID || actual == types.NoTypeID || tc.types == nil {
		return actual
	}
	actualResolved := tc.resolveAlias(actual)
	if elem, ok := tc.optionPayload(expected); ok {
		if actualResolved == tc.types.Builtins().Nothing {
			return expected
		}
		if tc.typesAssignable(elem, actualResolved, true) {
			return expected
		}
		if payload := tc.unwrapTaggedPayload(actualResolved, "Some"); payload != types.NoTypeID && tc.typesAssignable(elem, payload, true) {
			return expected
		}
	}
	if okType, _, ok := tc.resultPayload(expected); ok {
		if tc.typesAssignable(okType, actualResolved, true) {
			return expected
		}
		if payload := tc.unwrapTaggedPayload(actualResolved, "Ok"); payload != types.NoTypeID && tc.typesAssignable(okType, payload, true) {
			return expected
		}
	}
	return actual
}

func (tc *typeChecker) unwrapTaggedPayload(id types.TypeID, tag string) types.TypeID {
	if id == types.NoTypeID || tc.types == nil || tag == "" {
		return types.NoTypeID
	}
	info, ok := tc.types.UnionInfo(id)
	if !ok || info == nil {
		return types.NoTypeID
	}
	for _, member := range info.Members {
		if member.Kind != types.UnionMemberTag {
			continue
		}
		if tc.lookupExportedName(member.TagName) != tag {
			continue
		}
		if len(member.TagArgs) == 0 {
			return types.NoTypeID
		}
		return member.TagArgs[0]
	}
	return types.NoTypeID
}
