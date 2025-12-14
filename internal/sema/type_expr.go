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
			case sym.Kind == symbols.SymbolConst:
				ty = tc.ensureConstEvaluated(symID)
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
			ty = tc.typeBinary(expr.Span, data)
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
		}
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(id); ok && call != nil {
			if member, okMem := tc.builder.Exprs.Member(call.Target); okMem && member != nil {
				if tc.moduleSymbolForExpr(member.Target) == nil {
					receiverType, receiverIsType := tc.memberReceiverType(member.Target)
					// For static method calls with type args (e.g., Type::<int>::method()),
					// instantiate the receiver type with the call's type arguments
					if receiverIsType && len(call.TypeArgs) > 0 {
						typeArgs := tc.resolveCallTypeArgs(call.TypeArgs)
						if instantiated := tc.instantiateGenericType(receiverType, typeArgs, expr.Span); instantiated != types.NoTypeID {
							receiverType = instantiated
						}
					}
					if !receiverIsType && tc.lookupName(member.Field) == "await" {
						if tc.awaitDepth == 0 {
							tc.report(diag.SemaIntrinsicBadContext, expr.Span, "await can only be used in async context")
						}
						if receiverType != types.NoTypeID && !tc.isTaskType(receiverType) {
							tc.report(diag.SemaTypeMismatch, expr.Span, "await expects Task<T>, got %s", tc.typeLabel(receiverType))
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
					for _, arg := range call.Args {
						argTypes = append(argTypes, tc.typeExpr(arg.Value))
						tc.observeMove(arg.Value, tc.exprSpan(arg.Value))
					}
					ty = tc.methodResultType(member, receiverType, argTypes, expr.Span, receiverIsType)
					break
				}
			}
			ty = tc.callResultType(call, expr.Span)
		}
	case ast.ExprArray:
		if arr, ok := tc.builder.Exprs.Array(id); ok && arr != nil {
			var elemType types.TypeID
			for _, elem := range arr.Elements {
				elemTy := tc.typeExpr(elem)
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
			if magic := tc.magicResultForIndex(container, indexType); magic != types.NoTypeID {
				ty = magic
			} else {
				ty = tc.indexResultType(container, expr.Span)
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
			ty = tc.typeExpr(awaitData.Value)
		}
	case ast.ExprCast:
		if cast, ok := tc.builder.Exprs.Cast(id); ok && cast != nil {
			sourceType := tc.typeExpr(cast.Value)
			if sourceType == types.NoTypeID {
				break
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
			if tc.isAddressLike(sourceType) || tc.isAddressLike(targetType) {
				tc.report(diag.SemaTypeMismatch, expr.Span, "cannot cast %s to %s", tc.typeLabel(sourceType), tc.typeLabel(targetType))
				break
			}
			if numeric := tc.numericCastResult(sourceType, targetType); numeric != types.NoTypeID {
				ty = numeric
			} else if magic := tc.magicResultForCast(sourceType, targetType); magic != types.NoTypeID {
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
			remainingMembers := tc.unionMembers(valueType)
			nothingType := types.NoTypeID
			if tc.types != nil {
				nothingType = tc.types.Builtins().Nothing
			}
			for _, arm := range cmp.Arms {
				armSubject := valueType
				if narrowed := tc.narrowCompareSubjectType(valueType, remainingMembers); narrowed != types.NoTypeID {
					armSubject = narrowed
				}
				tc.inferComparePatternTypes(arm.Pattern, armSubject)
				if arm.Guard.IsValid() {
					tc.ensureBoolContext(arm.Guard, tc.exprSpan(arm.Guard))
				}
				armResult := tc.typeExpr(arm.Result)
				if armResult != types.NoTypeID {
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
				if len(remainingMembers) > 0 {
					remainingMembers = tc.consumeCompareMembers(remainingMembers, arm)
				}
			}
			// Check exhaustiveness for tagged unions
			tc.checkCompareExhausiveness(cmp, valueType, expr.Span)
			ty = resultType
		}
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
	case ast.ExprSpawn:
		if spawn, ok := tc.builder.Exprs.Spawn(id); ok && spawn != nil {
			exprType := tc.typeExpr(spawn.Value)
			tc.observeMove(spawn.Value, tc.exprSpan(spawn.Value))
			tc.enforceSpawn(spawn.Value)

			// spawn requires Task<T> — passthrough without re-wrapping
			if tc.isTaskType(exprType) {
				ty = exprType
				// Warn if spawning checkpoint() - it has no useful effect
				if tc.isCheckpointCall(spawn.Value) {
					tc.warn(diag.SemaSpawnCheckpointUseless, expr.Span,
						"spawn checkpoint() has no effect; use checkpoint().await() or ignore the result")
				}
			} else if exprType != types.NoTypeID {
				tc.report(diag.SemaSpawnNotTask, expr.Span,
					"spawn requires async function call or Task<T> expression, got %s",
					tc.typeLabel(exprType))
				ty = types.NoTypeID
			}

			// Track spawned task for structured concurrency
			if tc.taskTracker != nil && ty != types.NoTypeID {
				inAsyncBlock := tc.asyncBlockDepth > 0
				tc.taskTracker.SpawnTask(id, expr.Span, tc.currentScope(), inAsyncBlock)
			}
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

// memberReceiverType determines the receiver type for a method call target.
// It first tries to treat the target as a type operand (for static/associated methods),
// and falls back to value typing.
func (tc *typeChecker) memberReceiverType(target ast.ExprID) (types.TypeID, bool) {
	if t := tc.tryResolveTypeOperand(target); t != types.NoTypeID {
		return t, true
	}
	return tc.typeExpr(target), false
}

// unifyTernaryBranches determines the result type of a ternary expression
// by unifying the types of the true and false branches.
func (tc *typeChecker) unifyTernaryBranches(trueType, falseType types.TypeID, span source.Span) types.TypeID {
	if trueType == types.NoTypeID || falseType == types.NoTypeID {
		if trueType != types.NoTypeID {
			return trueType
		}
		return falseType
	}

	nothingType := tc.types.Builtins().Nothing

	switch {
	case trueType == nothingType:
		return falseType
	case falseType == nothingType:
		return trueType
	case tc.typesAssignable(trueType, falseType, true):
		return trueType
	case tc.typesAssignable(falseType, trueType, true):
		return falseType
	default:
		tc.report(diag.SemaTypeMismatch, span,
			"ternary branches have incompatible types: %s and %s",
			tc.typeLabel(trueType), tc.typeLabel(falseType))
		return types.NoTypeID
	}
}
