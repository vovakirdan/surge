package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) typeExprCall(id ast.ExprID, span source.Span, call *ast.ExprCallData) types.TypeID {
	if member, okMem := tc.builder.Exprs.Member(call.Target); okMem && member != nil {
		if tc.moduleSymbolForExpr(member.Target) == nil {
			receiverType, receiverIsType := tc.memberReceiverType(member.Target)
			usedTypeArgsForReceiver := receiverIsType && len(call.TypeArgs) > 0
			if usedTypeArgsForReceiver {
				typeArgs := tc.resolveCallTypeArgs(call.TypeArgs)
				if instantiated := tc.instantiateGenericType(receiverType, typeArgs, span); instantiated != types.NoTypeID {
					receiverType = instantiated
				}
			}
			if !receiverIsType && tc.lookupName(member.Field) == "await" {
				allowAwait := tc.awaitDepth > 0
				if !allowAwait {
					sym := tc.symbolFromID(tc.currentFnSym())
					if sym == nil || sym.Flags&symbols.SymbolFlagEntrypoint == 0 {
						tc.report(diag.SemaIntrinsicBadContext, span, "await can only be used in async context")
					} else {
						allowAwait = true
					}
				}
				if receiverType != types.NoTypeID && !tc.isTaskType(receiverType) {
					tc.report(diag.SemaTypeMismatch, span, "await expects Task<T>, got %s", tc.typeLabel(receiverType))
				}
				if allowAwait {
					tc.checkTaskContainersLiveAcrossAwait(span)
				}
				if tc.taskTracker != nil {
					tc.trackTaskAwait(member.Target)
				}
			}
			methodName := tc.lookupName(member.Field)
			argTypes := make([]types.TypeID, 0, len(call.Args))
			argExprs := make([]ast.ExprID, 0, len(call.Args))
			for _, arg := range call.Args {
				argTypes = append(argTypes, tc.typeExpr(arg.Value))
				argExprs = append(argExprs, arg.Value)
				tc.trackTaskPassedAsArg(arg.Value)
			}
			if !receiverIsType && tc.isChannelType(receiverType) &&
				(methodName == "send" || methodName == "try_send") && len(call.Args) > 0 {
				if tc.checkChannelSendValue(call.Args[0].Value, tc.exprSpan(call.Args[0].Value)) {
					return types.NoTypeID
				}
			}
			if !receiverIsType && methodName == "push" && len(argTypes) > 0 &&
				tc.isTaskType(argTypes[0]) && tc.isTaskContainerType(receiverType) {
				if place, ok := tc.taskContainerPlace(member.Target); ok {
					tc.markTaskContainerPending(place, span, receiverType)
				}
			}
			if !receiverIsType && methodName == "pop" && tc.isTaskContainerType(receiverType) {
				if place, ok := tc.taskContainerPlace(member.Target); ok {
					tc.noteTaskContainerPop(place)
				}
			}
			resultType := tc.methodResultType(member, receiverType, member.Target, argTypes, argExprs, span, receiverIsType)
			symID := tc.recordMethodCallSymbol(id, member, receiverType, member.Target, argTypes, argExprs, receiverIsType)
			var explicitArgs []types.TypeID
			if !usedTypeArgsForReceiver {
				explicitArgs = tc.resolveCallTypeArgs(call.TypeArgs)
			}
			tc.recordMethodCallInstantiation(symID, receiverType, explicitArgs, span)
			appliedArgsOwnership := false
			if symID.IsValid() {
				if sym := tc.symbolFromID(symID); sym != nil && sym.Signature != nil {
					tc.recordImplicitConversionsForMethodCall(sym, member.Target, receiverType, call.Args, argTypes)
				}
				if !receiverIsType {
					tc.applyMethodReceiverOwnership(symID, member.Target, receiverType)
					if sym := tc.symbolFromID(symID); sym != nil && sym.Signature != nil {
						if len(sym.Signature.Params) > 0 {
							tc.dropImplicitBorrowForRefParam(member.Target, sym.Signature.Params[0], receiverType, resultType, tc.exprSpan(member.Target))
							tc.dropImplicitBorrowForValueParam(member.Target, sym.Signature.Params[0], receiverType, tc.exprSpan(member.Target))
						}
						appliedArgsOwnership = tc.applyMethodArgsOwnership(sym, call.Args, argTypes)
						if appliedArgsOwnership {
							offset := 0
							if sym.Signature.HasSelf {
								offset = 1
							}
							for i, arg := range call.Args {
								paramIndex := i + offset
								if i >= len(argTypes) || paramIndex >= len(sym.Signature.Params) {
									break
								}
								tc.dropImplicitBorrowForRefParam(arg.Value, sym.Signature.Params[paramIndex], argTypes[i], resultType, tc.exprSpan(arg.Value))
								tc.dropImplicitBorrowForValueParam(arg.Value, sym.Signature.Params[paramIndex], argTypes[i], tc.exprSpan(arg.Value))
							}
						}
					}
				} else if methodName == "from_str" {
					appliedArgsOwnership = tc.applyCallArgsOwnership(symID, call.Args, argTypes)
					if appliedArgsOwnership {
						if sym := tc.symbolFromID(symID); sym != nil && sym.Signature != nil {
							for i, arg := range call.Args {
								if i >= len(sym.Signature.Params) || i >= len(argTypes) {
									break
								}
								tc.dropImplicitBorrowForRefParam(arg.Value, sym.Signature.Params[i], argTypes[i], resultType, tc.exprSpan(arg.Value))
								tc.dropImplicitBorrowForValueParam(arg.Value, sym.Signature.Params[i], argTypes[i], tc.exprSpan(arg.Value))
							}
						}
					}
				}
			}
			if !appliedArgsOwnership {
				for _, arg := range call.Args {
					tc.observeMove(arg.Value, tc.exprSpan(arg.Value))
				}
			}
			if !receiverIsType {
				tc.checkArrayViewResizeMethod(member.Target, methodName, receiverType, span)
				tc.markArrayViewMethodCall(id, methodName, receiverType, argTypes)
			}
			return resultType
		}
	}
	if ident, okIdent := tc.builder.Exprs.Ident(call.Target); okIdent && ident != nil {
		if tc.lookupName(ident.Name) == "timeout" && tc.awaitDepth == 0 {
			tc.report(diag.SemaIntrinsicBadContext, span, "timeout(...) is only available in async/task context; call it inside async/task and await it via x.await()")
		}
	}
	return tc.callResultType(id, call, span)
}

func (tc *typeChecker) typeExprIndex(id ast.ExprID, span source.Span) types.TypeID {
	idx, ok := tc.builder.Exprs.Index(id)
	if !ok || idx == nil {
		return types.NoTypeID
	}
	container := tc.typeExpr(idx.Target)
	indexType := tc.typeExpr(idx.Index)
	_, isAddressOfOperand := tc.addressOfOperands[id]
	var sig *symbols.FunctionSignature
	var sigCand typeKeyCandidate
	var sigSubst map[string]symbols.TypeKey
	var borrowInfo borrowMatchInfo
	var ambiguous bool
	if tc.assignmentLHSDepth == 0 {
		sig, sigCand, sigSubst, ambiguous, borrowInfo = tc.magicSignatureForIndexExpr(idx.Target, idx.Index, container, indexType)
	}
	switch {
	case ambiguous:
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for index")
		return types.NoTypeID
	case sig != nil:
		resultType := tc.magicIndexResultFromSig(sig, sigCand, sigSubst, indexType)
		if symID := tc.ensureMagicMethodSymbol("__index", sig, span); symID.IsValid() {
			tc.recordIndexSymbol(id, symID)
			tc.recordMethodCallInstantiation(symID, container, nil, span)
		}
		if !isAddressOfOperand {
			tc.applyParamOwnership(sig.Params[0], idx.Target, container, tc.exprSpan(idx.Target))
			tc.applyParamOwnership(sig.Params[1], idx.Index, indexType, tc.exprSpan(idx.Index))
		}
		if tc.isArrayRangeIndex(container, indexType) {
			tc.markArrayViewExpr(id)
		}
		return resultType
	case borrowInfo.expr.IsValid():
		tc.reportBorrowFailure(&borrowInfo)
		return types.NoTypeID
	}
	resultType := tc.magicResultForIndex(container, indexType)
	if resultType == types.NoTypeID {
		resultType = tc.indexResultType(container, indexType, span)
	}
	if tc.isArrayRangeIndex(container, indexType) {
		tc.markArrayViewExpr(id)
	}
	return resultType
}

func (tc *typeChecker) typeExprAwait(id ast.ExprID, span source.Span) types.TypeID {
	awaitData, ok := tc.builder.Exprs.Await(id)
	if !ok || awaitData == nil {
		return types.NoTypeID
	}
	taskType := tc.typeExpr(awaitData.Value)
	payload := tc.taskPayloadType(taskType)
	resultType := taskType
	if payload != types.NoTypeID {
		resultType = tc.taskResultType(payload, span)
	}
	allowAwait := tc.awaitDepth > 0
	if !allowAwait {
		sym := tc.symbolFromID(tc.currentFnSym())
		if sym != nil && sym.Flags&symbols.SymbolFlagEntrypoint != 0 {
			allowAwait = true
		}
	}
	if allowAwait {
		tc.checkTaskContainersLiveAcrossAwait(span)
	}
	return resultType
}

func (tc *typeChecker) typeExprCast(id ast.ExprID, span source.Span) types.TypeID {
	cast, ok := tc.builder.Exprs.Cast(id)
	if !ok || cast == nil {
		return types.NoTypeID
	}
	sourceType := tc.typeExpr(cast.Value)
	if sourceType == types.NoTypeID {
		return types.NoTypeID
	}
	castSource := sourceType
	if tc.isReferenceType(sourceType) {
		inner := tc.valueType(sourceType)
		if inner != types.NoTypeID && tc.isCopyType(inner) {
			castSource = inner
		}
	}
	scope := tc.scopeOrFile(tc.currentScope())
	targetType := types.NoTypeID
	if cast.Type.IsValid() {
		targetType = tc.resolveTypeExprWithScope(cast.Type, scope)
	} else if cast.RawType.IsValid() {
		targetType, _ = tc.resolveTypeOperand(cast.RawType, "to")
	}
	if targetType == types.NoTypeID {
		return types.NoTypeID
	}
	if tc.isAddressLike(castSource) || tc.isAddressLike(targetType) {
		tc.report(diag.SemaTypeMismatch, span, "cannot cast %s to %s", tc.typeLabel(sourceType), tc.typeLabel(targetType))
		return types.NoTypeID
	}
	if numeric := tc.numericCastResult(castSource, targetType); numeric != types.NoTypeID {
		return numeric
	}
	if magic := tc.magicResultForCast(castSource, targetType); magic != types.NoTypeID {
		symID := tc.resolveToSymbol(cast.Value, castSource, targetType)
		if tc.result != nil {
			if tc.result.ToSymbols == nil {
				tc.result.ToSymbols = make(map[ast.ExprID]symbols.SymbolID)
			}
			tc.result.ToSymbols[id] = symID
		}
		if symID.IsValid() {
			tc.recordMethodCallInstantiation(symID, castSource, nil, span)
			if sym := tc.symbolFromID(symID); sym != nil && sym.Signature != nil && len(sym.Signature.Params) > 0 {
				tc.applyParamOwnership(sym.Signature.Params[0], cast.Value, sourceType, tc.exprSpan(cast.Value))
				tc.dropImplicitBorrowForRefParam(cast.Value, sym.Signature.Params[0], sourceType, magic, tc.exprSpan(cast.Value))
			}
		}
		return magic
	}
	if cast.Type.IsValid() && tc.literalCoercible(targetType, sourceType) && tc.isLiteralExpr(cast.Value) {
		return targetType
	}
	tc.reportMissingCastMethod(sourceType, targetType, span)
	return types.NoTypeID
}
