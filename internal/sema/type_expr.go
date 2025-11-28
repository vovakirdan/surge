package sema

import (
	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
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
	var ty types.TypeID
	switch expr.Kind {
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(id); ok && ident != nil {
			symID := tc.symbolForExpr(id)
			if symID == symbols.NoSymbolID {
				symID = tc.lookupValueSymbol(ident.Name, tc.currentScope())
			}
			sym := tc.symbolFromID(symID)
			switch {
			case sym == nil:
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
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(id); ok && call != nil {
			if member, okMem := tc.builder.Exprs.Member(call.Target); okMem && member != nil {
				receiverType := tc.typeExpr(member.Target)
				if tc.lookupName(member.Field) == "await" {
					if tc.awaitDepth == 0 {
						tc.report(diag.SemaIntrinsicBadContext, expr.Span, "await can only be used in async context")
					}
					if receiverType != types.NoTypeID && !tc.isTaskType(receiverType) {
						tc.report(diag.SemaTypeMismatch, expr.Span, "await expects Task<T>, got %s", tc.typeLabel(receiverType))
					}
				}
				argTypes := make([]types.TypeID, 0, len(call.Args))
				for _, arg := range call.Args {
					argTypes = append(argTypes, tc.typeExpr(arg))
					tc.observeMove(arg, tc.exprSpan(arg))
				}
				ty = tc.methodResultType(member, receiverType, argTypes, expr.Span)
			} else {
				ty = tc.callResultType(call, expr.Span)
			}
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
			for _, elem := range tuple.Elements {
				tc.typeExpr(elem)
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
			targetType := tc.typeExpr(member.Target)
			ty = tc.memberResultType(targetType, member.Field, expr.Span)
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
			for _, arm := range cmp.Arms {
				tc.inferComparePatternTypes(arm.Pattern, valueType)
				if arm.Guard.IsValid() {
					tc.ensureBoolContext(arm.Guard, tc.exprSpan(arm.Guard))
				}
				armResult := tc.typeExpr(arm.Result)
				if armResult == types.NoTypeID {
					continue
				}
				if resultType == types.NoTypeID {
					resultType = armResult
					continue
				}
				if !tc.typesAssignable(resultType, armResult, true) {
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(arm.Result), "compare arm type mismatch: expected %s, got %s", tc.typeLabel(resultType), tc.typeLabel(armResult))
				}
			}
			ty = resultType
		}
	case ast.ExprParallel:
		if par, ok := tc.builder.Exprs.Parallel(id); ok && par != nil {
			tc.typeExpr(par.Iterable)
			tc.typeExpr(par.Init)
			for _, arg := range par.Args {
				tc.typeExpr(arg)
			}
			tc.typeExpr(par.Body)
		}
	case ast.ExprAsync:
		if asyncData, ok := tc.builder.Exprs.Async(id); ok && asyncData != nil {
			var returns []types.TypeID
			tc.pushReturnContext(types.NoTypeID, expr.Span, &returns)
			tc.awaitDepth++
			tc.walkStmt(asyncData.Body)
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
			payload := tc.typeExpr(spawn.Value)
			tc.observeMove(spawn.Value, tc.exprSpan(spawn.Value))
			tc.enforceSpawn(spawn.Value)
			ty = tc.taskType(payload, expr.Span)
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
				ty = tc.resolveTypeExprWithScope(data.Type, scope)
				if ty != types.NoTypeID {
					tc.validateStructLiteralFields(ty, data, expr.Span)
				}
			}
		}
	default:
		// ExprIdent and other unhandled kinds default to unknown.
	}
	tc.result.ExprTypes[id] = ty
	return ty
}
