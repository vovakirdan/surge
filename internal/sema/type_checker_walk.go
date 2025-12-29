package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/dialect"
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
		// Validate and record let item attributes
		tc.validateLetAttrs(letItem, symID)
		declaredType := tc.resolveTypeExprWithScope(letItem.Type, scope)
		if declaredType != types.NoTypeID {
			tc.setBindingType(symID, declaredType)
		}
		if !letItem.Value.IsValid() {
			tc.handleLetDefaultInit(scope, letItem.Type, declaredType, item.Span)
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
		tc.markArrayViewBinding(symID, tc.isArrayViewExpr(letItem.Value))
	case ast.ItemConst:
		symID := tc.typeSymbolForItem(id)
		// Validate and record const item attributes
		if constItem, ok := tc.builder.Items.Const(id); ok && constItem != nil {
			tc.validateConstAttrs(constItem, symID)
		}
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
		popParams := tc.pushFnParams(tc.fnParamSymbols(fnItem, scope))
		defer popParams()
		allowRawPointer := tc.hasIntrinsicAttr(fnItem.AttrStart, fnItem.AttrCount)
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
		returnType := tc.functionReturnType(fnItem, scope, allowRawPointer)
		returnSpan := fnItem.ReturnSpan
		if returnSpan == (source.Span{}) {
			returnSpan = fnItem.Span
		}
		tc.registerFnParamTypes(id, fnItem, allowRawPointer)
		if len(paramSpecs) == 0 && symID.IsValid() && tc.types != nil {
			paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
			paramTypes := make([]types.TypeID, 0, len(paramIDs))
			allParamsValid := true
			for _, pid := range paramIDs {
				param := tc.builder.Items.FnParam(pid)
				if param == nil {
					continue
				}
				paramType := tc.resolveTypeExprWithScopeAllowPointer(param.Type, scope, allowRawPointer)
				if paramType == types.NoTypeID {
					allParamsValid = false
					break
				}
				if param.Variadic {
					paramType = tc.instantiateArrayType(paramType)
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
				tc.maybeRecordRustImplicitReturn(fnItem, returnType, returnSpan)
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
		tc.validateFunctionAttrs(fnItem, symID, types.NoTypeID)
		// Validate entrypoint constraints if this is an entrypoint function
		if sym := tc.symbolFromID(symID); sym != nil && sym.Flags&symbols.SymbolFlagEntrypoint != 0 {
			tc.validateEntrypoint(fnItem, sym)
		}
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
				if !letStmt.Value.IsValid() {
					tc.handleLetDefaultInit(scope, letStmt.Type, declaredType, stmt.Span)
					return
				}
				if letStmt.Value.IsValid() {
					valueType := tc.typeExpr(letStmt.Value)
					tc.observeMove(letStmt.Value, tc.exprSpan(letStmt.Value))
					tc.ensureBindingTypeMatch(letStmt.Type, declaredType, valueType, letStmt.Value)
					if declaredType == types.NoTypeID {
						tc.setBindingType(symID, valueType)
					}
					tc.updateStmtBinding(id, letStmt.Value)
					tc.markArrayViewBinding(symID, tc.isArrayViewExpr(letStmt.Value))
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
			tc.checkTrivialReturnRecursion(ret.Expr)
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

func (tc *typeChecker) maybeRecordRustImplicitReturn(fn *ast.FnItem, returnType types.TypeID, returnSpan source.Span) {
	if tc.builder == nil || fn == nil || tc.types == nil || tc.result == nil {
		return
	}
	if returnType == types.NoTypeID {
		return
	}
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	body := tc.builder.Stmts.Block(fn.Body)
	if body == nil || len(body.Stmts) == 0 {
		return
	}
	lastStmtID := body.Stmts[len(body.Stmts)-1]
	stmt := tc.builder.Stmts.Get(lastStmtID)
	if stmt == nil || stmt.Kind != ast.StmtExpr {
		return
	}
	exprStmt := tc.builder.Stmts.Expr(lastStmtID)
	if exprStmt == nil || !exprStmt.MissingSemicolon {
		return
	}
	exprType, ok := tc.result.ExprTypes[exprStmt.Expr]
	if !ok || exprType == types.NoTypeID {
		return
	}
	resolvedReturn := tc.resolveAlias(returnType)
	resolvedExpr := tc.resolveAlias(exprType)
	if resolvedReturn == types.NoTypeID || resolvedExpr == types.NoTypeID {
		return
	}
	if resolvedReturn != resolvedExpr {
		return
	}
	span := returnSpan
	if expr := tc.builder.Exprs.Get(exprStmt.Expr); expr != nil {
		if span == (source.Span{}) {
			span = expr.Span
		} else {
			span = span.Cover(expr.Span)
		}
	}
	ev := file.DialectEvidence
	if ev == nil {
		ev = dialect.NewEvidence()
		file.DialectEvidence = ev
	}
	ev.Add(dialect.Hint{
		Dialect: dialect.DialectRust,
		Score:   6,
		Reason:  "implicit return without ';' at block end",
		Span:    span,
	})
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

func (tc *typeChecker) handleLetDefaultInit(scope symbols.ScopeID, typeExpr ast.TypeID, declaredType types.TypeID, span source.Span) {
	if declaredType == types.NoTypeID {
		return
	}
	if !tc.defaultable(declaredType) {
		reportSpan := tc.typeSpan(typeExpr)
		if reportSpan == (source.Span{}) {
			reportSpan = span
		}
		tc.report(diag.SemaTypeMismatch, reportSpan, "default is not defined for %s", tc.typeLabel(declaredType))
		return
	}
	if tc.builder == nil {
		return
	}
	nameID := tc.builder.StringsInterner.Intern("default")
	symID := tc.symbolInScope(tc.scopeOrFile(scope), nameID, symbols.SymbolFunction)
	if symID.IsValid() {
		tc.rememberFunctionInstantiation(symID, []types.TypeID{declaredType}, span, "default-init")
	}
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

func (tc *typeChecker) functionReturnType(fn *ast.FnItem, scope symbols.ScopeID, allowRawPointer bool) types.TypeID {
	if tc.types == nil || fn == nil {
		return types.NoTypeID
	}
	expected := tc.types.Builtins().Nothing
	if fn.ReturnType.IsValid() {
		if resolved := tc.resolveTypeExprWithScopeAllowPointer(fn.ReturnType, scope, allowRawPointer); resolved != types.NoTypeID {
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
