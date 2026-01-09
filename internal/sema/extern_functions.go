package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) checkExternFns(itemID ast.ItemID, block *ast.ExternBlock) {
	if block == nil || !block.MembersStart.IsValid() || block.MembersCount == 0 {
		return
	}
	if tc.externTargetIsSealed(itemID, block) {
		return
	}
	scope := tc.scopeForItem(itemID)
	receiverSpecs := tc.externTypeParamSpecs(block.Target, scope)
	receiverOwner := tc.externTargetSymbol(block.Target, scope)
	start := uint32(block.MembersStart)
	for offset := range block.MembersCount {
		memberID := ast.ExternMemberID(start + offset)
		member := tc.builder.Items.ExternMember(memberID)
		if member == nil || member.Kind != ast.ExternMemberFn {
			continue
		}
		fn := tc.builder.Items.FnByPayload(member.Fn)
		if fn == nil {
			continue
		}
		tc.typecheckExternFn(memberID, fn, receiverSpecs, receiverOwner)
	}
}

func (tc *typeChecker) typecheckExternFn(memberID ast.ExternMemberID, fn *ast.FnItem, receiverSpecs []genericParamSpec, receiverOwner symbols.SymbolID) {
	if fn == nil {
		return
	}
	scope := tc.scopeOrFile(tc.scopeForExtern(memberID))
	symID := tc.symbolForExtern(memberID)
	popFn := tc.pushFnSym(symID)
	defer popFn()
	popParams := tc.pushFnParams(tc.fnParamSymbols(fn, scope))
	defer popParams()

	receiverParamsPushed := tc.pushTypeParams(receiverOwner, receiverSpecs, nil)
	if receiverOwner.IsValid() && receiverParamsPushed {
		tc.applyTypeParamBounds(receiverOwner)
	}
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetFnTypeParamIDs(fn), scope)
	if len(paramSpecs) == 0 && len(fn.Generics) > 0 {
		paramSpecs = specsFromNames(fn.Generics)
	}
	// Check for type parameter shadowing: method's type params must not shadow extern's type params
	tc.checkTypeParamShadowing(fn, paramSpecs, receiverSpecs)
	typeParamsPushed := tc.pushTypeParams(symID, paramSpecs, nil)
	if paramIDs := tc.builder.Items.GetFnTypeParamIDs(fn); len(paramIDs) > 0 {
		bounds := tc.resolveTypeParamBounds(paramIDs, scope, nil)
		tc.attachTypeParamSymbols(symID, bounds)
		tc.applyTypeParamBounds(symID)
	}

	returnType := tc.functionReturnType(fn, scope, true)
	returnSpan := fn.ReturnSpan
	if returnSpan == (source.Span{}) {
		returnSpan = fn.Span
	}

	tc.registerExternParamTypes(scope, fn, true)
	if symID.IsValid() && tc.types != nil {
		paramIDs := tc.builder.Items.GetFnParamIDs(fn)
		paramTypes := make([]types.TypeID, 0, len(paramIDs))
		allParamsValid := true
		for _, pid := range paramIDs {
			param := tc.builder.Items.FnParam(pid)
			if param == nil {
				continue
			}
			paramType := tc.resolveTypeExprWithScopeAllowPointer(param.Type, scope, true)
			if paramType == types.NoTypeID {
				allParamsValid = false
				break
			}
			paramTypes = append(paramTypes, paramType)
		}
		if allParamsValid {
			resultType := returnType
			if fn.Flags&ast.FnModifierAsync != 0 {
				resultType = tc.taskType(returnType, returnSpan)
			}
			fnType := tc.types.RegisterFn(paramTypes, resultType)
			tc.assignSymbolType(symID, fnType)
		}
	}

	if fn.Body.IsValid() {
		tc.pushReturnContext(returnType, returnSpan, nil)
		pushed := tc.pushScope(scope)
		tc.walkStmt(fn.Body)
		if returnType != tc.types.Builtins().Nothing && tc.returnStatus(fn.Body) != returnClosed {
			tc.report(diag.SemaMissingReturn, returnSpan, "function returning %s is missing a return", tc.typeLabel(returnType))
		}
		// Perform lock analysis after walking the body
		selfSym := tc.findSelfSymbol(fn, scope)
		tc.analyzeFunctionLocks(fn, selfSym)
		// Check @nonblocking constraint
		if tc.fnHasNonblocking(fn) {
			tc.checkNonblockingFunction(fn, fn.Span)
		}
		if pushed {
			tc.leaveScope()
		}
		tc.popReturnContext()
	}
	// Validate function attributes
	ownerTypeID := types.NoTypeID
	if receiverOwner.IsValid() {
		if sym := tc.symbolFromID(receiverOwner); sym != nil {
			ownerTypeID = sym.Type
		}
	}
	tc.validateFunctionAttrs(fn, symID, ownerTypeID)

	if typeParamsPushed {
		tc.popTypeParams()
	}
	if receiverParamsPushed {
		tc.popTypeParams()
	}
}

func (tc *typeChecker) registerExternParamTypes(scope symbols.ScopeID, fnItem *ast.FnItem, allowRawPointer bool) {
	if tc.builder == nil || fnItem == nil {
		return
	}
	scope = tc.scopeOrFile(scope)
	paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
	for _, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil || param.Name == source.NoStringID {
			continue
		}
		paramType := tc.resolveTypeExprWithScopeAllowPointer(param.Type, scope, allowRawPointer)
		symID := tc.symbolInScope(scope, param.Name, symbols.SymbolParam)
		if paramType == types.NoTypeID {
			continue
		}
		if symID.IsValid() {
			tc.setBindingType(symID, paramType)
			continue
		}
		if tc.symbols != nil && tc.symbols.Table != nil && tc.symbols.Table.Scopes != nil {
			if scopeData := tc.symbols.Table.Scopes.Get(scope); scopeData != nil {
				for _, cand := range scopeData.NameIndex[param.Name] {
					if sym := tc.symbolFromID(cand); sym != nil && sym.Kind == symbols.SymbolParam {
						tc.setBindingType(cand, paramType)
					}
				}
			}
		}
	}
}

// checkTypeParamShadowing reports an error if any method type parameter
// shadows an outer type parameter from the extern block.
func (tc *typeChecker) checkTypeParamShadowing(fn *ast.FnItem, methodSpecs, receiverSpecs []genericParamSpec) {
	if fn == nil || len(methodSpecs) == 0 || len(receiverSpecs) == 0 {
		return
	}
	// Build a set of receiver type parameter names
	receiverNames := make(map[source.StringID]struct{}, len(receiverSpecs))
	for _, spec := range receiverSpecs {
		if spec.name != source.NoStringID {
			receiverNames[spec.name] = struct{}{}
		}
	}
	// Check each method type parameter against receiver names
	for _, spec := range methodSpecs {
		if spec.name == source.NoStringID {
			continue
		}
		if _, shadows := receiverNames[spec.name]; shadows {
			// Get the string representation of the name
			nameStr := tc.lookupName(spec.name)
			span := fn.GenericsSpan
			if span == (source.Span{}) {
				span = fn.Span
			}
			tc.report(diag.SemaTypeParamShadow, span,
				"type parameter '%s' shadows outer type parameter from extern block", nameStr)
		}
	}
}
