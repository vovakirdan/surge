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
		// Check for Task<T> escape to global scope - module-level let bindings
		if tc.isTaskType(valueType) {
			tc.report(diag.SemaTaskEscapesScope, tc.exprSpan(letItem.Value),
				"cannot store Task<T> in module-level variable - tasks must be scoped to functions or async blocks")
		}
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
		popFn := tc.pushFnSym(symID)
		defer popFn()
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
		if len(paramSpecs) == 0 && symID.IsValid() && tc.types != nil {
			paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
			paramTypes := make([]types.TypeID, 0, len(paramIDs))
			allParamsValid := true
			for _, pid := range paramIDs {
				param := tc.builder.Items.FnParam(pid)
				if param == nil {
					continue
				}
				paramType := tc.resolveTypeExprWithScope(param.Type, scope)
				if paramType == types.NoTypeID {
					allParamsValid = false
					break
				}
				paramTypes = append(paramTypes, paramType)
			}
			if allParamsValid {
				fnType := tc.types.RegisterFn(paramTypes, returnType)
				tc.assignSymbolType(symID, fnType)
			}
		}
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
			// Perform lock analysis and check @nonblocking constraint
			selfSym := tc.findSelfSymbol(fnItem, scope)
			tc.analyzeFunctionLocks(fnItem, selfSym)
			if tc.fnHasNonblocking(fnItem) {
				tc.checkNonblockingFunction(fnItem, fnItem.Span)
			}
			if pushed {
				tc.leaveScope()
			}
			tc.popReturnContext()
			if fnItem.Flags&ast.FnModifierAsync != 0 {
				tc.awaitDepth--
			}
		}
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
					// Track task binding for structured concurrency
					if tc.taskTracker != nil && tc.isTaskType(valueType) {
						tc.taskTracker.BindTaskByExpr(letStmt.Value, symID)
					}
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
				// Track task return for structured concurrency
				tc.trackTaskReturn(ret.Expr)
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
		if actElem, actLen, actFixed, okAct := tc.arrayInfo(actual); okAct {
			// Check if element types are assignable OR can be implicitly converted
			elemAssignable := tc.typesAssignable(expElem, actElem, true)
			var elemConvertible bool
			if !elemAssignable {
				_, found, _ := tc.tryImplicitConversion(actElem, expElem)
				elemConvertible = found
			}

			if elemAssignable || elemConvertible {
				if expFixed {
					if actFixed && expLen == actLen {
						// Array length matches
						if elemConvertible && !elemAssignable {
							// Element types need conversion - only allowed for array literals
							if valueExpr.IsValid() {
								if arr, okArr := tc.builder.Exprs.Array(valueExpr); okArr && arr != nil {
									tc.recordArrayElementConversions(arr, expElem)
									return
								}
							}
							// Not an array literal, can't convert elements - fall through to error
						} else {
							// Elements are assignable without conversion
							return
						}
					}
					if !actFixed && valueExpr.IsValid() {
						if arr, okArr := tc.builder.Exprs.Array(valueExpr); okArr && arr != nil {
							if l, err := safecast.Conv[uint32](len(arr.Elements)); err == nil && l == expLen {
								// Array length matches, record element conversions if needed
								if elemConvertible && !elemAssignable {
									tc.recordArrayElementConversions(arr, expElem)
								}
								return
							}
						}
					}
				} else {
					// Dynamic array
					if elemConvertible && !elemAssignable {
						// Element types need conversion - only allowed for array literals
						if valueExpr.IsValid() {
							if arr, okArr := tc.builder.Exprs.Array(valueExpr); okArr && arr != nil {
								tc.recordArrayElementConversions(arr, expElem)
								return
							}
						}
						// Not an array literal, can't convert elements - fall through to error
					} else {
						// Elements are assignable without conversion
						return
					}
				}
			}
		}
	}
	if tc.typesAssignable(declared, actual, true) {
		return
	}
	// Try implicit conversion before reporting error
	if convType, found, ambiguous := tc.tryImplicitConversion(actual, declared); found {
		tc.recordImplicitConversion(valueExpr, actual, convType)
		return
	} else if ambiguous {
		tc.report(diag.SemaAmbiguousConversion, tc.exprSpan(valueExpr),
			"ambiguous conversion from %s to %s: multiple __to methods found",
			tc.typeLabel(actual), tc.typeLabel(declared))
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

	for i, elem := range tuple.Elements {
		elemType := info.Elems[i]
		node := tc.builder.Exprs.Get(elem)
		if node == nil {
			continue
		}
		switch node.Kind {
		case ast.ExprIdent:
			ident, _ := tc.builder.Exprs.Ident(elem)
			if ident == nil {
				continue
			}
			tc.result.ExprTypes[elem] = elemType

			// Attach type to the bound symbol
			symID := tc.symbolForExpr(elem)
			if !symID.IsValid() && scope.IsValid() {
				symID = tc.symbolInScope(scope, ident.Name, symbols.SymbolLet)
			}
			if symID.IsValid() {
				tc.setBindingType(symID, elemType)
			}
		case ast.ExprTuple:
			tc.bindTuplePattern(elem, elemType, scope)
		default:
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(elem), "expected identifier in pattern")
		}
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
	// Check if actual is a member of expected union type
	if tc.isUnionMember(expected, actual) {
		return true
	}
	if expElem, expLen, expFixed, okExp := tc.arrayInfo(expected); okExp {
		if actElem, actLen, actFixed, okAct := tc.arrayInfo(actual); okAct && tc.typesAssignable(expElem, actElem, true) {
			if expFixed {
				return actFixed && expLen == actLen
			}
			return true
		}
	}
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
	expFn, expOk := tc.types.FnInfo(expected)
	actFn, actOk := tc.types.FnInfo(actual)
	if expOk && actOk {
		if len(expFn.Params) != len(actFn.Params) {
			return false
		}
		for i := range expFn.Params {
			if !tc.typesAssignable(expFn.Params[i], actFn.Params[i], allowAlias) {
				return false
			}
		}
		return tc.typesAssignable(expFn.Result, actFn.Result, allowAlias)
	}
	if tc.numericWidenable(actual, expected) {
		return true
	}
	return false
}

// isUnionMember checks if actual type is a member of expected union type.
// This enables assigning union members directly to union variables,
// e.g. `let x: Option<int> = nothing;` or `let y: Foo = Bar(1);`
func (tc *typeChecker) isUnionMember(expected, actual types.TypeID) bool {
	if expected == types.NoTypeID || actual == types.NoTypeID || tc.types == nil {
		return false
	}

	// Защита от бесконечной рекурсии при взаимно-рекурсивных типах
	// (e.g., type A = union<Tag1<B>>, type B = union<Tag2<A>>)
	key := assignabilityKey{Expected: expected, Actual: actual}
	if tc.assignabilityInProgress != nil {
		if _, inProgress := tc.assignabilityInProgress[key]; inProgress {
			return false // Прерываем рекурсию
		}
		tc.assignabilityInProgress[key] = struct{}{}
		defer delete(tc.assignabilityInProgress, key)
	}
	// Resolve aliases first
	expectedResolved := tc.resolveAlias(expected)
	actualResolved := tc.resolveAlias(actual)

	info, ok := tc.types.UnionInfo(expectedResolved)
	if !ok || info == nil {
		return false
	}

	for _, member := range info.Members {
		switch member.Kind {
		case types.UnionMemberNothing:
			// `nothing` is a union member
			if actualResolved == tc.types.Builtins().Nothing {
				return true
			}
		case types.UnionMemberType:
			// Type member (e.g., `int` in union)
			if member.Type == actualResolved {
				return true
			}
			// Also check if member type is assignable (for nested unions)
			if tc.resolveAlias(member.Type) == actualResolved {
				return true
			}
		case types.UnionMemberTag:
			// Tagged member (e.g., `Some(T)` in Option<T>)
			// Check if actual is a tag type matching this member
			if tc.isTagTypeMatch(actualResolved, member.TagName, member.TagArgs) {
				return true
			}
		}
	}
	return false
}

// isTagTypeMatch checks if the given type is a tag type matching the specified tag name and arguments.
// This is used to check if a value like `Some(1)` matches a union member `Some(T)`.
func (tc *typeChecker) isTagTypeMatch(typeID types.TypeID, tagName source.StringID, tagArgs []types.TypeID) bool {
	if typeID == types.NoTypeID || tc.types == nil {
		return false
	}

	// Get union info for the type - tag types are represented as single-member unions
	info, ok := tc.types.UnionInfo(typeID)
	if !ok || info == nil {
		return false
	}

	// Check if this is a tag type by looking at its name
	typeName := tc.lookupName(info.Name)
	expectedTagName := tc.lookupName(tagName)
	if typeName != expectedTagName {
		return false
	}

	// Check type arguments match
	if len(info.TypeArgs) != len(tagArgs) {
		return false
	}
	for i, arg := range info.TypeArgs {
		if !tc.typesAssignable(tagArgs[i], arg, true) {
			return false
		}
	}

	return true
}

// inferForInElementType extracts the element type from an iterable.
// It checks for __range method or direct Range<T> type.
func (tc *typeChecker) inferForInElementType(iterableType types.TypeID, span source.Span) types.TypeID {
	if iterableType == types.NoTypeID {
		return types.NoTypeID
	}

	if elem, ok := tc.rangePayload(iterableType); ok {
		return elem
	}

	if elem, ok := tc.arrayElemType(iterableType); ok {
		return elem
	}

	rangeType := tc.lookupRangeMethodResult(iterableType)
	if rangeType != types.NoTypeID {
		if elem, ok := tc.rangePayload(rangeType); ok {
			return elem
		}
	}

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

// recordArrayElementConversions records implicit conversions for array elements
// when the expected element type differs from the actual element types.
func (tc *typeChecker) recordArrayElementConversions(arr *ast.ExprArrayData, expectedElemType types.TypeID) {
	if arr == nil || expectedElemType == types.NoTypeID {
		return
	}

	for _, elem := range arr.Elements {
		if !elem.IsValid() {
			continue
		}
		// Get the actual type of this element (should already be typed)
		actualElemType := tc.result.ExprTypes[elem]
		if actualElemType == types.NoTypeID {
			continue
		}

		// Check if implicit conversion is needed
		if !tc.typesAssignable(expectedElemType, actualElemType, true) {
			if convType, found, _ := tc.tryImplicitConversion(actualElemType, expectedElemType); found {
				tc.recordImplicitConversion(elem, actualElemType, convType)
			}
		}
	}
}
