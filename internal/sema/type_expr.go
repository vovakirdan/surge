package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"
)

func (tc *typeChecker) typeExpr(id ast.ExprID) types.TypeID {
	if !id.IsValid() {
		return types.NoTypeID
	}
	if ty, ok := tc.result.ExprTypes[id]; ok {
		return ty
	}
	expr := tc.builder.Exprs.Get(id)
	if expr == nil {
		return types.NoTypeID
	}

	// Track recursion depth
	tc.exprDepth++
	defer func() { tc.exprDepth-- }()

	// Only trace at debug level and limit depth to avoid noise
	var span *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDebug && tc.exprDepth <= 20 {
		span = trace.Begin(tc.tracer, trace.ScopeNode, "type_expr", 0)
		span.WithExtra("kind", fmt.Sprintf("%d", expr.Kind))
		span.WithExtra("depth", fmt.Sprintf("%d", tc.exprDepth))
	}

	var ty types.TypeID
	defer func() {
		if span != nil {
			if ty != types.NoTypeID {
				span.WithExtra("result", tc.typeLabel(ty))
			}
			span.End("")
		}
	}()
	switch expr.Kind {
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(id); ok && ident != nil {
			symID := tc.symbolForExpr(id)
			if symID == symbols.NoSymbolID {
				symID = tc.lookupValueSymbol(ident.Name, tc.currentScope())
			}
			sym := tc.symbolFromID(symID)
			if sym != nil && sym.Kind == symbols.SymbolImport {
				sym = tc.resolveImportedValueSymbol(sym, ident.Name, expr.Span)
			}
			switch {
			case sym == nil:
				if param := tc.lookupTypeParam(ident.Name); param != types.NoTypeID {
					name := tc.lookupName(ident.Name)
					if name == "" {
						name = "_"
					}
					tc.report(diag.SemaTypeMismatch, expr.Span, "type %s cannot be used as a value", name)
				}
				ty = types.NoTypeID
			case sym.Kind == symbols.SymbolLet || sym.Kind == symbols.SymbolParam:
				ty = tc.bindingType(symID)
				if tc.assignmentLHSDepth == 0 {
					tc.checkUseAfterMove(symID, expr.Span)
				}
				// Check for deprecated variable usage (let only, params are local)
				if sym.Kind == symbols.SymbolLet {
					tc.checkDeprecatedSymbol(symID, "variable", expr.Span)
				}
			case sym.Kind == symbols.SymbolConst:
				ty = tc.ensureConstEvaluated(symID)
				// Check for deprecated constant usage
				tc.checkDeprecatedSymbol(symID, "constant", expr.Span)
			case sym.Kind == symbols.SymbolType:
				name := tc.lookupName(ident.Name)
				if name == "" {
					name = "_"
				}
				tc.report(diag.SemaTypeMismatch, expr.Span, "type %s cannot be used as a value", name)
				ty = types.NoTypeID
			default:
				ty = sym.Type
			}
		}
	case ast.ExprLit:
		if lit, ok := tc.builder.Exprs.Literal(id); ok && lit != nil {
			ty = tc.literalType(lit.Kind)
		}
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(id); ok && group != nil {
			ty = tc.typeExpr(group.Inner)
		}
	case ast.ExprUnary:
		if data, ok := tc.builder.Exprs.Unary(id); ok && data != nil {
			ty = tc.typeUnary(id, expr.Span, data)
		}
	case ast.ExprBinary:
		if data, ok := tc.builder.Exprs.Binary(id); ok && data != nil {
			ty = tc.typeBinary(id, expr.Span, data)
		}
	case ast.ExprTernary:
		if tern, ok := tc.builder.Exprs.Ternary(id); ok && tern != nil {
			// 1. Validate condition is boolean
			tc.ensureBoolContext(tern.Cond, tc.exprSpan(tern.Cond))

			// 2. Type both branches
			trueType := tc.typeExpr(tern.TrueExpr)
			falseType := tc.typeExpr(tern.FalseExpr)

			// 3. Unify branch types
			ty = tc.unifyTernaryBranches(trueType, falseType, expr.Span)
			if ty != types.NoTypeID {
				tc.recordNumericWidening(tern.TrueExpr, trueType, ty)
				tc.recordNumericWidening(tern.FalseExpr, falseType, ty)
			}
		}
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(id); ok && call != nil {
			if member, okMem := tc.builder.Exprs.Member(call.Target); okMem && member != nil {
				if tc.moduleSymbolForExpr(member.Target) == nil {
					receiverType, receiverIsType := tc.memberReceiverType(member.Target)
					usedTypeArgsForReceiver := receiverIsType && len(call.TypeArgs) > 0
					// For static method calls with type args (e.g., Type::<int>::method()),
					// instantiate the receiver type with the call's type arguments
					if usedTypeArgsForReceiver {
						typeArgs := tc.resolveCallTypeArgs(call.TypeArgs)
						if instantiated := tc.instantiateGenericType(receiverType, typeArgs, expr.Span); instantiated != types.NoTypeID {
							receiverType = instantiated
						}
					}
					if !receiverIsType && tc.lookupName(member.Field) == "await" {
						allowAwait := tc.awaitDepth > 0
						if !allowAwait {
							sym := tc.symbolFromID(tc.currentFnSym())
							if sym == nil || sym.Flags&symbols.SymbolFlagEntrypoint == 0 {
								tc.report(diag.SemaIntrinsicBadContext, expr.Span, "await can only be used in async context")
							} else {
								allowAwait = true
							}
						}
						if receiverType != types.NoTypeID && !tc.isTaskType(receiverType) {
							tc.report(diag.SemaTypeMismatch, expr.Span, "await expects Task<T>, got %s", tc.typeLabel(receiverType))
						}
						if allowAwait {
							tc.checkTaskContainersLiveAcrossAwait(expr.Span)
						}
						// Track await for structured concurrency
						if tc.taskTracker != nil {
							tc.trackTaskAwait(member.Target)
						}
					}
					// Check channel send for @nosend values
					methodName := tc.lookupName(member.Field)
					if !receiverIsType && tc.isChannelType(receiverType) &&
						(methodName == "send" || methodName == "try_send") && len(call.Args) > 0 {
						tc.checkChannelSendValue(call.Args[0].Value, expr.Span)
					}
					argTypes := make([]types.TypeID, 0, len(call.Args))
					argExprs := make([]ast.ExprID, 0, len(call.Args))
					for _, arg := range call.Args {
						argTypes = append(argTypes, tc.typeExpr(arg.Value))
						argExprs = append(argExprs, arg.Value)
						tc.trackTaskPassedAsArg(arg.Value)
					}
					if !receiverIsType && methodName == "push" && len(argTypes) > 0 &&
						tc.isTaskType(argTypes[0]) && tc.isTaskContainerType(receiverType) {
						if place, ok := tc.taskContainerPlace(member.Target); ok {
							tc.markTaskContainerPending(place, expr.Span, receiverType)
						}
					}
					if !receiverIsType && methodName == "pop" && tc.isTaskContainerType(receiverType) {
						if place, ok := tc.taskContainerPlace(member.Target); ok {
							tc.noteTaskContainerPop(place)
						}
					}
					ty = tc.methodResultType(member, receiverType, member.Target, argTypes, argExprs, expr.Span, receiverIsType)
					symID := tc.recordMethodCallSymbol(id, member, receiverType, member.Target, argTypes, argExprs, receiverIsType)
					var explicitArgs []types.TypeID
					if !usedTypeArgsForReceiver {
						explicitArgs = tc.resolveCallTypeArgs(call.TypeArgs)
					}
					tc.recordMethodCallInstantiation(symID, receiverType, explicitArgs, expr.Span)
					appliedArgsOwnership := false
					if symID.IsValid() {
						if sym := tc.symbolFromID(symID); sym != nil && sym.Signature != nil {
							tc.recordImplicitConversionsForMethodCall(sym, member.Target, receiverType, call.Args, argTypes)
						}
						if !receiverIsType {
							tc.applyMethodReceiverOwnership(symID, member.Target, receiverType)
							if sym := tc.symbolFromID(symID); sym != nil && sym.Signature != nil {
								if len(sym.Signature.Params) > 0 {
									tc.dropImplicitBorrowForRefParam(member.Target, sym.Signature.Params[0], receiverType, ty, tc.exprSpan(member.Target))
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
										tc.dropImplicitBorrowForRefParam(arg.Value, sym.Signature.Params[paramIndex], argTypes[i], ty, tc.exprSpan(arg.Value))
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
										tc.dropImplicitBorrowForRefParam(arg.Value, sym.Signature.Params[i], argTypes[i], ty, tc.exprSpan(arg.Value))
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
						tc.checkArrayViewResizeMethod(member.Target, methodName, receiverType, expr.Span)
						tc.markArrayViewMethodCall(id, methodName, receiverType, argTypes)
					}
					break
				}
			}
			if ident, okIdent := tc.builder.Exprs.Ident(call.Target); okIdent && ident != nil {
				if tc.lookupName(ident.Name) == "timeout" && tc.awaitDepth == 0 {
					tc.report(diag.SemaIntrinsicBadContext, expr.Span, "timeout(...) is only available in async/task context; call it inside async/task and await it via x.await()")
				}
			}
			ty = tc.callResultType(id, call, expr.Span)
		}
	case ast.ExprArray:
		if arr, ok := tc.builder.Exprs.Array(id); ok && arr != nil {
			var elemType types.TypeID
			for _, elem := range arr.Elements {
				elemTy := tc.typeExpr(elem)
				if tc.isTaskType(elemTy) {
					tc.trackTaskPassedAsArg(elem)
				}
				if elemType == types.NoTypeID {
					elemType = elemTy
				} else if elemTy != types.NoTypeID && elemTy != elemType {
					tc.report(diag.SemaTypeMismatch, expr.Span, "array elements must have the same type")
				}
			}
			if elemType != types.NoTypeID {
				if len(arr.Elements) > 0 {
					if len(arr.Elements) > int(^uint32(0)) {
						tc.report(diag.SemaTypeMismatch, expr.Span, "array literal too large")
					} else if length, err := safecast.Conv[uint32](len(arr.Elements)); err == nil {
						ty = tc.instantiateArrayFixed(elemType, length)
					}
				}
				if ty == types.NoTypeID {
					ty = tc.instantiateArrayType(elemType)
				}
			}
		}
	case ast.ExprMap:
		if mp, ok := tc.builder.Exprs.Map(id); ok && mp != nil {
			var keyType types.TypeID
			var valueType types.TypeID
			for _, entry := range mp.Entries {
				kType := tc.typeExpr(entry.Key)
				if tc.isTaskType(kType) {
					tc.trackTaskPassedAsArg(entry.Key)
				}
				vType := tc.typeExpr(entry.Value)
				if tc.isTaskType(vType) {
					tc.trackTaskPassedAsArg(entry.Value)
				}
				if keyType == types.NoTypeID {
					keyType = kType
				} else if kType != types.NoTypeID && kType != keyType {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(entry.Key), "map keys must have the same type")
				}
				if valueType == types.NoTypeID {
					valueType = vType
				} else if vType != types.NoTypeID && vType != valueType {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(entry.Value), "map values must have the same type")
				}
			}
			if keyType != types.NoTypeID && valueType != types.NoTypeID {
				ty = tc.instantiateMapType(keyType, valueType, expr.Span)
			}
		}
	case ast.ExprRangeLit:
		if rng, ok := tc.builder.Exprs.RangeLit(id); ok && rng != nil {
			intType := tc.types.Builtins().Int
			if rng.Start.IsValid() {
				startType := tc.typeExpr(rng.Start)
				if startType != types.NoTypeID && !tc.sameType(startType, intType) {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(rng.Start),
						"range bound must be int, got %s", tc.typeLabel(startType))
				}
			}
			if rng.End.IsValid() {
				endType := tc.typeExpr(rng.End)
				if endType != types.NoTypeID && !tc.sameType(endType, intType) {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(rng.End),
						"range bound must be int, got %s", tc.typeLabel(endType))
				}
			}
			ty = tc.resolveRangeType(intType, expr.Span, tc.currentScope())
		}
	case ast.ExprTuple:
		if tuple, ok := tc.builder.Exprs.Tuple(id); ok && tuple != nil {
			elems := make([]types.TypeID, 0, len(tuple.Elements))
			allValid := true
			for _, elem := range tuple.Elements {
				elemType := tc.typeExpr(elem)
				if elemType == types.NoTypeID {
					allValid = false
				}
				elems = append(elems, elemType)
			}
			if allValid && len(elems) > 0 {
				ty = tc.types.RegisterTuple(elems)
			} else if len(elems) == 0 {
				// Empty tuple () is unit type
				ty = tc.types.Builtins().Unit
			}
		}
	case ast.ExprIndex:
		if idx, ok := tc.builder.Exprs.Index(id); ok && idx != nil {
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
			if ambiguous {
				tc.report(diag.SemaAmbiguousOverload, expr.Span, "ambiguous overload for index")
				ty = types.NoTypeID
				break
			}
			if sig != nil {
				ty = tc.magicIndexResultFromSig(sig, sigCand, sigSubst, indexType)
				if symID := tc.magicSymbolForSignature(sig); symID.IsValid() {
					tc.recordIndexSymbol(id, symID)
					tc.recordMethodCallInstantiation(symID, container, nil, expr.Span)
				}
			} else if borrowInfo.expr.IsValid() {
				tc.reportBorrowFailure(&borrowInfo)
				ty = types.NoTypeID
			} else if magic := tc.magicResultForIndex(container, indexType); magic != types.NoTypeID {
				ty = magic
			} else {
				ty = tc.indexResultType(container, indexType, expr.Span)
			}
			if sig != nil && !isAddressOfOperand {
				tc.applyParamOwnership(sig.Params[0], idx.Target, container, tc.exprSpan(idx.Target))
				tc.applyParamOwnership(sig.Params[1], idx.Index, indexType, tc.exprSpan(idx.Index))
			}
			if tc.isArrayRangeIndex(container, indexType) {
				tc.markArrayViewExpr(id)
			}
		}
	case ast.ExprMember:
		if member, ok := tc.builder.Exprs.Member(id); ok && member != nil {
			if module := tc.moduleSymbolForExpr(member.Target); module != nil {
				ty = tc.typeOfModuleMember(module, member.Field, expr.Span)
			} else if enumType := tc.enumTypeForExpr(member.Target); enumType != types.NoTypeID {
				// Type::Variant access for enum
				ty = tc.typeOfEnumVariant(enumType, member.Field, expr.Span)
			} else {
				targetType := tc.typeExpr(member.Target)
				ty = tc.memberResultType(targetType, member.Field, expr.Span)
				// Check @atomic field direct access (skip if this is operand of address-of)
				_, isAddressOfOperand := tc.addressOfOperands[id]
				tc.checkAtomicFieldDirectAccess(id, isAddressOfOperand, expr.Span)
			}
		}
	case ast.ExprTupleIndex:
		if data, ok := tc.builder.Exprs.TupleIndex(id); ok && data != nil {
			targetType := tc.typeExpr(data.Target)
			ty = tc.tupleIndexResultType(targetType, data.Index, tc.exprSpan(id))
		}
	case ast.ExprAwait:
		if awaitData, ok := tc.builder.Exprs.Await(id); ok && awaitData != nil {
			taskType := tc.typeExpr(awaitData.Value)
			payload := tc.taskPayloadType(taskType)
			if payload != types.NoTypeID {
				ty = tc.taskResultType(payload, expr.Span)
			} else {
				ty = taskType
			}
			allowAwait := tc.awaitDepth > 0
			if !allowAwait {
				sym := tc.symbolFromID(tc.currentFnSym())
				if sym != nil && sym.Flags&symbols.SymbolFlagEntrypoint != 0 {
					allowAwait = true
				}
			}
			if allowAwait {
				tc.checkTaskContainersLiveAcrossAwait(expr.Span)
			}
		}
	case ast.ExprCast:
		if cast, ok := tc.builder.Exprs.Cast(id); ok && cast != nil {
			sourceType := tc.typeExpr(cast.Value)
			if sourceType == types.NoTypeID {
				break
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
				break
			}
			if tc.isAddressLike(castSource) || tc.isAddressLike(targetType) {
				tc.report(diag.SemaTypeMismatch, expr.Span, "cannot cast %s to %s", tc.typeLabel(sourceType), tc.typeLabel(targetType))
				break
			}
			if numeric := tc.numericCastResult(castSource, targetType); numeric != types.NoTypeID {
				ty = numeric
			} else if magic := tc.magicResultForCast(castSource, targetType); magic != types.NoTypeID {
				symID := tc.resolveToSymbol(cast.Value, castSource, targetType)
				if tc.result != nil {
					if tc.result.ToSymbols == nil {
						tc.result.ToSymbols = make(map[ast.ExprID]symbols.SymbolID)
					}
					tc.result.ToSymbols[id] = symID
				}
				if symID.IsValid() {
					tc.recordMethodCallInstantiation(symID, castSource, nil, expr.Span)
				}
				if symID.IsValid() {
					if sym := tc.symbolFromID(symID); sym != nil && sym.Signature != nil && len(sym.Signature.Params) > 0 {
						tc.applyParamOwnership(sym.Signature.Params[0], cast.Value, sourceType, tc.exprSpan(cast.Value))
						tc.dropImplicitBorrowForRefParam(cast.Value, sym.Signature.Params[0], sourceType, magic, tc.exprSpan(cast.Value))
					}
				}
				ty = magic
			} else if cast.Type.IsValid() && tc.literalCoercible(targetType, sourceType) && tc.isLiteralExpr(cast.Value) {
				ty = targetType
			} else {
				tc.reportMissingCastMethod(sourceType, targetType, expr.Span)
			}
		}
	case ast.ExprCompare:
		if cmp, ok := tc.builder.Exprs.Compare(id); ok && cmp != nil {
			valueType := tc.typeExpr(cmp.Value)
			resultType := types.NoTypeID
			expectedCompare := tc.expectedTypeForExpr(id)
			if expectedCompare != types.NoTypeID {
				resultType = expectedCompare
			}
			remainingMembers := tc.unionMembers(valueType)
			nothingType := types.NoTypeID
			if tc.types != nil {
				nothingType = tc.types.Builtins().Nothing
			}
			armTypes := make([]types.TypeID, len(cmp.Arms))
			for i, arm := range cmp.Arms {
				armSubject := valueType
				if narrowed := tc.narrowCompareSubjectType(valueType, remainingMembers); narrowed != types.NoTypeID {
					armSubject = narrowed
				}
				tc.inferComparePatternTypes(arm.Pattern, armSubject)
				if arm.Guard.IsValid() {
					tc.ensureBoolContext(arm.Guard, tc.exprSpan(arm.Guard))
				}
				armResult := tc.typeExpr(arm.Result)
				explicitReturn := false
				if nothingType != types.NoTypeID && tc.compareArmIsExplicitReturn(arm.Result) {
					armResult = nothingType
					explicitReturn = true
				}
				armTypes[i] = armResult
				if armResult != types.NoTypeID {
					if expectedCompare != types.NoTypeID {
						if !explicitReturn && (nothingType == types.NoTypeID || armResult != nothingType) {
							tc.ensureBindingTypeMatch(ast.NoTypeID, expectedCompare, armResult, arm.Result)
						}
					} else {
						switch {
						case resultType == types.NoTypeID:
							resultType = armResult
						case nothingType != types.NoTypeID && resultType == nothingType:
							resultType = armResult
						case nothingType != types.NoTypeID && armResult == nothingType:
							// nothing can flow into any other arm result
						case tc.typesAssignable(resultType, armResult, true):
							// arm result fits the current inferred type
						case tc.typesAssignable(armResult, resultType, true):
							// widen the result type to the new arm
							resultType = armResult
						default:
							tc.report(diag.SemaTypeMismatch, tc.exprSpan(arm.Result), "compare arm type mismatch: expected %s, got %s", tc.typeLabel(resultType), tc.typeLabel(armResult))
						}
					}
				}
				if len(remainingMembers) > 0 {
					remainingMembers = tc.consumeCompareMembers(remainingMembers, arm)
				}
			}
			if expectedCompare == types.NoTypeID && resultType != types.NoTypeID {
				for i, arm := range cmp.Arms {
					tc.recordNumericWidening(arm.Result, armTypes[i], resultType)
				}
			}
			// Check exhaustiveness for tagged unions
			tc.checkCompareExhausiveness(cmp, valueType, expr.Span)
			ty = resultType
		}
	case ast.ExprSelect:
		ty = tc.typeSelectExpr(id, false, expr.Span)
	case ast.ExprRace:
		ty = tc.typeSelectExpr(id, true, expr.Span)
	case ast.ExprParallel:
		if par, ok := tc.builder.Exprs.Parallel(id); ok && par != nil {
			tc.reporter.Report(diag.FutParallelNotSupported, diag.SevError, expr.Span, "'parallel' requires multi-threading (v2+)", nil, nil)
		}
	case ast.ExprAsync:
		if asyncData, ok := tc.builder.Exprs.Async(id); ok && asyncData != nil {
			var returns []types.TypeID
			tc.pushReturnContext(types.NoTypeID, expr.Span, &returns)
			tc.awaitDepth++
			tc.asyncBlockDepth++
			tc.walkStmt(asyncData.Body)
			tc.asyncBlockDepth--
			tc.awaitDepth--
			tc.popReturnContext()
			payload := tc.types.Builtins().Nothing
			for _, rt := range returns {
				if rt == types.NoTypeID {
					continue
				}
				if payload == tc.types.Builtins().Nothing {
					payload = rt
					continue
				}
				if !tc.typesAssignable(payload, rt, true) && !tc.typesAssignable(rt, payload, true) {
					payload = types.NoTypeID
				}
			}
			if payload == types.NoTypeID {
				payload = tc.types.Builtins().Nothing
			}
			ty = tc.taskType(payload, expr.Span)
		}
	case ast.ExprBlocking:
		if blockingData, ok := tc.builder.Exprs.Blocking(id); ok && blockingData != nil {
			var returns []types.TypeID
			tc.pushReturnContext(types.NoTypeID, expr.Span, &returns)
			tc.walkStmt(blockingData.Body)
			tc.popReturnContext()
			payload := tc.types.Builtins().Nothing
			for _, rt := range returns {
				if rt == types.NoTypeID {
					continue
				}
				if payload == tc.types.Builtins().Nothing {
					payload = rt
					continue
				}
				if !tc.typesAssignable(payload, rt, true) && !tc.typesAssignable(rt, payload, true) {
					payload = types.NoTypeID
				}
			}
			if payload == types.NoTypeID {
				payload = tc.types.Builtins().Nothing
			}
			ty = tc.taskType(payload, expr.Span)

			captures := tc.collectBlockingCaptures(blockingData.Body)
			tc.recordBlockingCaptures(id, captures)
			for _, cap := range captures {
				capType := tc.bindingType(cap.symID)
				if tc.isReferenceType(capType) {
					tc.report(diag.SemaBlockingBorrowCapture, cap.span,
						"blocking captures must be by value; cannot capture reference %s", tc.typeLabel(capType))
					continue
				}
				tc.checkSpawnSendability(cap.symID, cap.span)
				tc.observeMove(cap.exprID, cap.span)
			}
		}
	case ast.ExprTask:
		if task, ok := tc.builder.Exprs.Task(id); ok && task != nil {
			ty = tc.typeSpawnExpr(id, expr.Span, task.Value, false)
		}
	case ast.ExprSpawn:
		if spawn, ok := tc.builder.Exprs.Spawn(id); ok && spawn != nil {
			local := tc.spawnHasAttr(id, "local")
			ty = tc.typeSpawnExpr(id, expr.Span, spawn.Value, local)
		}
	case ast.ExprSpread:
		if spread, ok := tc.builder.Exprs.Spread(id); ok && spread != nil {
			tc.typeExpr(spread.Value)
		}
	case ast.ExprStruct:
		if data, ok := tc.builder.Exprs.Struct(id); ok && data != nil {
			for _, field := range data.Fields {
				tc.typeExpr(field.Value)
			}
			if data.Type.IsValid() {
				scope := tc.scopeOrFile(tc.currentScope())
				if inferred, handled := tc.inferStructLiteralType(data, scope, expr.Span); handled {
					ty = inferred
				} else {
					ty = tc.resolveTypeExprWithScope(data.Type, scope)
					if ty != types.NoTypeID {
						tc.validateStructLiteralFields(ty, data, expr.Span)
					}
				}
			}
		}
	case ast.ExprBlock:
		if block, ok := tc.builder.Exprs.Block(id); ok && block != nil {
			ty = tc.typeBlockExpr(block)
		}
	default:
		// ExprIdent and other unhandled kinds default to unknown.
	}
	tc.result.ExprTypes[id] = ty
	return ty
}

func (tc *typeChecker) typeExprAssignLHS(id ast.ExprID) types.TypeID {
	tc.assignmentLHSDepth++
	ty := tc.typeExpr(id)
	tc.assignmentLHSDepth--
	if tc.builder != nil && tc.isReferenceType(ty) {
		exprID := tc.unwrapGroupExpr(id)
		if idx, ok := tc.builder.Exprs.Index(exprID); ok && idx != nil {
			if elem, ok := tc.elementType(ty); ok {
				return elem
			}
		}
	}
	return ty
}

func (tc *typeChecker) typeSpawnExpr(exprID ast.ExprID, span source.Span, value ast.ExprID, local bool) types.TypeID {
	exprType := tc.typeExpr(value)
	tc.observeMove(value, tc.exprSpan(value))
	tc.enforceSpawn(value, local)

	var ty types.TypeID
	if tc.isTaskType(exprType) {
		ty = exprType
		if tc.isCheckpointCall(value) {
			tc.warn(diag.SemaSpawnCheckpointUseless, span,
				"spawn checkpoint() has no effect; use checkpoint().await() or ignore the result")
		}
	} else if exprType != types.NoTypeID {
		tc.report(diag.SemaSpawnNotTask, span,
			"spawn requires async function call or Task<T> expression, got %s",
			tc.typeLabel(exprType))
		ty = types.NoTypeID
	}

	if tc.taskTracker != nil && ty != types.NoTypeID {
		inAsyncBlock := tc.asyncBlockDepth > 0
		tc.taskTracker.SpawnTask(exprID, span, tc.currentScope(), inAsyncBlock, local)
	}

	return ty
}
