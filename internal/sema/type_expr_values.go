package sema

import (
	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) typeExprIdent(id ast.ExprID, span source.Span) types.TypeID {
	ident, ok := tc.builder.Exprs.Ident(id)
	if !ok || ident == nil {
		return types.NoTypeID
	}
	symID := tc.symbolForExpr(id)
	if symID == symbols.NoSymbolID {
		symID = tc.lookupValueSymbol(ident.Name, tc.currentScope())
	}
	sym := tc.symbolFromID(symID)
	if sym != nil && sym.Kind == symbols.SymbolImport {
		sym = tc.resolveImportedValueSymbol(sym, ident.Name, span)
	}
	switch {
	case sym == nil:
		if param := tc.lookupTypeParam(ident.Name); param != types.NoTypeID {
			name := tc.lookupName(ident.Name)
			if name == "" {
				name = "_"
			}
			tc.report(diag.SemaTypeMismatch, span, "type %s cannot be used as a value", name)
		}
		return types.NoTypeID
	case sym.Kind == symbols.SymbolLet || sym.Kind == symbols.SymbolParam:
		ty := tc.bindingType(symID)
		if tc.assignmentLHSDepth == 0 {
			tc.checkUseAfterMove(symID, span)
		}
		if sym.Kind == symbols.SymbolLet {
			tc.checkDeprecatedSymbol(symID, "variable", span)
		}
		return ty
	case sym.Kind == symbols.SymbolConst:
		ty := tc.ensureConstEvaluated(symID)
		tc.checkDeprecatedSymbol(symID, "constant", span)
		return ty
	case sym.Kind == symbols.SymbolType:
		name := tc.lookupName(ident.Name)
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaTypeMismatch, span, "type %s cannot be used as a value", name)
		return types.NoTypeID
	default:
		if sym.Kind == symbols.SymbolFunction && len(sym.TypeParams) > 0 {
			if expected := tc.expectedTypeForExpr(id); expected != types.NoTypeID && tc.tryBindGenericFnValue(id, expected) {
				return expected
			}
		}
		return sym.Type
	}
}

func (tc *typeChecker) typeExprLiteral(id ast.ExprID) types.TypeID {
	lit, ok := tc.builder.Exprs.Literal(id)
	if !ok || lit == nil {
		return types.NoTypeID
	}
	return tc.literalType(lit.Kind)
}

func (tc *typeChecker) typeExprGroup(id ast.ExprID) types.TypeID {
	group, ok := tc.builder.Exprs.Group(id)
	if !ok || group == nil {
		return types.NoTypeID
	}
	return tc.typeExpr(group.Inner)
}

func (tc *typeChecker) typeExprTernary(id ast.ExprID, span source.Span) types.TypeID {
	tern, ok := tc.builder.Exprs.Ternary(id)
	if !ok || tern == nil {
		return types.NoTypeID
	}
	tc.ensureBoolContext(tern.Cond, tc.exprSpan(tern.Cond))
	trueType := tc.typeExpr(tern.TrueExpr)
	falseType := tc.typeExpr(tern.FalseExpr)
	resultType := tc.unifyTernaryBranches(trueType, falseType, span)
	if resultType != types.NoTypeID {
		tc.recordNumericWidening(tern.TrueExpr, trueType, resultType)
		tc.recordNumericWidening(tern.FalseExpr, falseType, resultType)
	}
	return resultType
}

func (tc *typeChecker) typeExprArray(id ast.ExprID, span source.Span) types.TypeID {
	arr, ok := tc.builder.Exprs.Array(id)
	if !ok || arr == nil {
		return types.NoTypeID
	}
	var elemType types.TypeID
	for _, elem := range arr.Elements {
		elemTy := tc.typeExpr(elem)
		if tc.isTaskType(elemTy) {
			tc.trackTaskPassedAsArg(elem)
		}
		if elemType == types.NoTypeID {
			elemType = elemTy
			continue
		}
		if elemTy != types.NoTypeID && elemTy != elemType {
			tc.report(diag.SemaTypeMismatch, span, "array elements must have the same type")
		}
	}
	if elemType == types.NoTypeID {
		return types.NoTypeID
	}
	if len(arr.Elements) > 0 {
		if len(arr.Elements) > int(^uint32(0)) {
			tc.report(diag.SemaTypeMismatch, span, "array literal too large")
		} else if length, err := safecast.Conv[uint32](len(arr.Elements)); err == nil {
			if fixed := tc.instantiateArrayFixed(elemType, length); fixed != types.NoTypeID {
				return fixed
			}
		}
	}
	return tc.instantiateArrayType(elemType)
}

func (tc *typeChecker) typeExprMap(id ast.ExprID, span source.Span) types.TypeID {
	mp, ok := tc.builder.Exprs.Map(id)
	if !ok || mp == nil {
		return types.NoTypeID
	}
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
	if keyType == types.NoTypeID || valueType == types.NoTypeID {
		return types.NoTypeID
	}
	return tc.instantiateMapType(keyType, valueType, span)
}

func (tc *typeChecker) typeExprRange(id ast.ExprID, span source.Span) types.TypeID {
	rng, ok := tc.builder.Exprs.RangeLit(id)
	if !ok || rng == nil {
		return types.NoTypeID
	}
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
	return tc.resolveRangeType(intType, span, tc.currentScope())
}

func (tc *typeChecker) typeExprTuple(id ast.ExprID) types.TypeID {
	tuple, ok := tc.builder.Exprs.Tuple(id)
	if !ok || tuple == nil {
		return types.NoTypeID
	}
	elems := make([]types.TypeID, 0, len(tuple.Elements))
	allValid := true
	for _, elem := range tuple.Elements {
		elemType := tc.typeExpr(elem)
		if elemType == types.NoTypeID {
			allValid = false
		}
		elems = append(elems, elemType)
	}
	if len(elems) == 0 {
		return tc.types.Builtins().Unit
	}
	if !allValid {
		return types.NoTypeID
	}
	return tc.types.RegisterTuple(elems)
}

func (tc *typeChecker) typeExprMember(id ast.ExprID, span source.Span) types.TypeID {
	member, ok := tc.builder.Exprs.Member(id)
	if !ok || member == nil {
		return types.NoTypeID
	}
	if module := tc.moduleSymbolForExpr(member.Target); module != nil {
		return tc.typeOfModuleMember(module, member.Field, span)
	}
	if enumType := tc.enumTypeForExpr(member.Target); enumType != types.NoTypeID {
		return tc.typeOfEnumVariant(enumType, member.Field, span)
	}
	targetType := tc.typeExpr(member.Target)
	resultType := tc.memberResultType(targetType, member.Field, span)
	_, isAddressOfOperand := tc.addressOfOperands[id]
	tc.checkAtomicFieldDirectAccess(id, isAddressOfOperand, span)
	return resultType
}

func (tc *typeChecker) typeExprTupleIndex(id ast.ExprID, span source.Span) types.TypeID {
	data, ok := tc.builder.Exprs.TupleIndex(id)
	if !ok || data == nil {
		return types.NoTypeID
	}
	targetType := tc.typeExpr(data.Target)
	return tc.tupleIndexResultType(targetType, data.Index, span)
}

func (tc *typeChecker) typeExprSpread(id ast.ExprID) {
	spread, ok := tc.builder.Exprs.Spread(id)
	if ok && spread != nil {
		tc.typeExpr(spread.Value)
	}
}

func (tc *typeChecker) typeExprStruct(id ast.ExprID, span source.Span) types.TypeID {
	data, ok := tc.builder.Exprs.Struct(id)
	if !ok || data == nil {
		return types.NoTypeID
	}
	for _, field := range data.Fields {
		tc.typeExpr(field.Value)
	}
	if !data.Type.IsValid() {
		return types.NoTypeID
	}
	scope := tc.scopeOrFile(tc.currentScope())
	if inferred, handled := tc.inferStructLiteralType(data, scope, span); handled {
		return inferred
	}
	ty := tc.resolveTypeExprWithScope(data.Type, scope)
	if ty != types.NoTypeID {
		tc.validateStructLiteralFields(ty, data, span)
	}
	return ty
}
