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

func (tc *typeChecker) literalType(kind ast.ExprLitKind) types.TypeID {
	b := tc.types.Builtins()
	switch kind {
	case ast.ExprLitInt:
		return b.Int
	case ast.ExprLitUint:
		return b.Uint
	case ast.ExprLitFloat:
		return b.Float
	case ast.ExprLitString:
		return b.String
	case ast.ExprLitTrue, ast.ExprLitFalse:
		return b.Bool
	case ast.ExprLitNothing:
		return b.Nothing
	default:
		return types.NoTypeID
	}
}

// IsOperandKind identifies the target of an 'is' expression.
type IsOperandKind uint8

const (
	// IsOperandType indicates the target is a type.
	IsOperandType IsOperandKind = iota
	// IsOperandTag indicates the target is a union tag.
	IsOperandTag
)

// IsOperand stores resolved target information for 'is'.
type IsOperand struct {
	Kind IsOperandKind
	Type types.TypeID
	Tag  source.StringID
}

// HeirOperand stores resolved type operands for 'heir'.
type HeirOperand struct {
	Left  types.TypeID
	Right types.TypeID
}

func (tc *typeChecker) typeUnary(exprID ast.ExprID, span source.Span, data *ast.ExprUnaryData) types.TypeID {
	// Mark address-of operands for @atomic validation
	if data.Op == ast.ExprUnaryRef || data.Op == ast.ExprUnaryRefMut {
		if tc.addressOfOperands == nil {
			tc.addressOfOperands = make(map[ast.ExprID]struct{})
		}
		tc.addressOfOperands[data.Operand] = struct{}{}
	}
	operandType := tc.typeExpr(data.Operand)
	switch data.Op {
	case ast.ExprUnaryRef, ast.ExprUnaryRefMut:
		tc.handleBorrow(exprID, span, data.Op, data.Operand)
		if operandType == types.NoTypeID {
			return types.NoTypeID
		}
		mutable := data.Op == ast.ExprUnaryRefMut
		return tc.types.Intern(types.MakeReference(operandType, mutable))
	case ast.ExprUnaryDeref:
		elem, ok := tc.elementType(operandType)
		if !ok {
			tc.report(diag.SemaInvalidUnaryOperand, span, "cannot dereference %s", tc.typeLabel(operandType))
			return types.NoTypeID
		}
		return elem
	case ast.ExprUnaryAwait:
		payload := tc.taskPayloadType(operandType)
		if payload == types.NoTypeID {
			return operandType
		}
		return tc.taskResultType(payload, span)
	case ast.ExprUnaryOwn:
		if operandType == types.NoTypeID || tc.types == nil {
			return types.NoTypeID
		}
		resolved := tc.resolveAlias(operandType)
		if tt, ok := tc.types.Lookup(resolved); ok && tt.Kind == types.KindOwn {
			return operandType
		}
		return tc.types.Intern(types.MakeOwn(operandType))
	default:
		sig, cand, ambiguous, borrowInfo := tc.magicSignatureForUnaryExpr(data.Operand, operandType, data.Op)
		if ambiguous {
			tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for operator %s", tc.unaryOpLabel(data.Op))
			return types.NoTypeID
		}
		if sig != nil {
			if symID := tc.magicSymbolForSignature(sig); symID.IsValid() {
				tc.recordMagicUnarySymbol(exprID, symID)
				tc.recordMagicOpInstantiation(symID, operandType, span)
			}
			res := tc.typeFromKey(sig.Result)
			if res != types.NoTypeID {
				tc.applyParamOwnership(sig.Params[0], data.Operand, operandType, tc.exprSpan(data.Operand))
				tc.dropImplicitBorrowForRefParam(data.Operand, sig.Params[0], operandType, res, tc.exprSpan(data.Operand))
				tc.dropImplicitBorrowForValueParam(data.Operand, sig.Params[0], operandType, tc.exprSpan(data.Operand))
				return tc.adjustAliasUnaryResult(res, cand)
			}
		} else if borrowInfo.expr.IsValid() {
			tc.reportBorrowFailure(&borrowInfo)
			return types.NoTypeID
		}
		tc.reportMissingUnaryMethod(data.Op, operandType, span)
		return types.NoTypeID
	}
}

func (tc *typeChecker) typeBinary(exprID ast.ExprID, span source.Span, data *ast.ExprBinaryData) types.TypeID {
	if data.Op == ast.ExprBinaryAssign {
		leftType := tc.typeExprAssignLHS(data.Left)
		rightType := tc.typeExpr(data.Right)
		tc.ensureBindingTypeMatch(ast.NoTypeID, leftType, rightType, data.Right)
		tc.ensureIndexAssignment(data.Left, leftType, span)
		tc.applyIndexSetterOwnership(data.Left, data.Right, rightType)
		tc.trackTaskContainerStore(data.Left, data.Right, rightType)
		tc.trackTaskContainerAssign(data.Left, data.Right, rightType, span)
		tc.handleAssignment(data.Op, data.Left, data.Right, span)
		tc.updateArrayViewBindingFromAssign(data.Left, data.Right)
		tc.updateLocalTaskBindingFromAssign(data.Left, data.Right)
		return leftType
	}
	leftType := tc.typeExpr(data.Left)
	if data.Op == ast.ExprBinaryIs {
		return tc.typeIsExpr(exprID, leftType, data.Right, data.Op)
	}
	if data.Op == ast.ExprBinaryHeir {
		return tc.typeHeirExpr(exprID, leftType, data.Right, data.Op)
	}
	rightType := tc.typeExpr(data.Right)
	var ok bool
	leftType, rightType, ok = tc.materializeNumericBinaryLiterals(data.Op, data.Left, data.Right, leftType, rightType)
	if !ok {
		return types.NoTypeID
	}
	if !tc.enforceSameNumericOperands(data.Op, leftType, rightType, span) {
		return types.NoTypeID
	}
	if baseOp, ok := tc.assignmentBaseOp(data.Op); ok {
		return tc.typeCompoundAssignment(baseOp, data.Op, span, data.Left, data.Right, leftType, rightType)
	}
	sig, lc, rc, ambiguous, borrowInfo := tc.magicSignatureForBinaryExpr(data.Left, data.Right, leftType, rightType, data.Op)
	if ambiguous {
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for operator %s", tc.binaryOpLabel(data.Op))
		return types.NoTypeID
	}
	if sig != nil {
		if symID := tc.magicSymbolForSignature(sig); symID.IsValid() {
			tc.recordMagicBinarySymbol(exprID, symID)
			tc.recordMagicOpInstantiation(symID, leftType, span)
		}
		res := tc.typeFromKey(sig.Result)
		if res == types.NoTypeID {
			res = tc.magicResultFallback(sig.Result, lc, rc)
		}
		if res != types.NoTypeID {
			tc.applyParamOwnership(sig.Params[0], data.Left, leftType, tc.exprSpan(data.Left))
			tc.applyParamOwnership(sig.Params[1], data.Right, rightType, tc.exprSpan(data.Right))
			tc.dropImplicitBorrowForRefParam(data.Left, sig.Params[0], leftType, res, tc.exprSpan(data.Left))
			tc.dropImplicitBorrowForRefParam(data.Right, sig.Params[1], rightType, res, tc.exprSpan(data.Right))
			tc.dropImplicitBorrowForValueParam(data.Left, sig.Params[0], leftType, tc.exprSpan(data.Left))
			tc.dropImplicitBorrowForValueParam(data.Right, sig.Params[1], rightType, tc.exprSpan(data.Right))
			return tc.adjustAliasBinaryResult(res, lc, rc)
		}
	} else if borrowInfo.expr.IsValid() {
		tc.reportBorrowFailure(&borrowInfo)
		return types.NoTypeID
	}
	return tc.typeBinaryFallback(span, data, leftType, rightType)
}

func (tc *typeChecker) typeIsExpr(exprID ast.ExprID, leftType types.TypeID, rightExpr ast.ExprID, op ast.ExprBinaryOp) types.TypeID {
	operand, ok := tc.resolveIsOperand(leftType, rightExpr, tc.binaryOpLabel(op))
	if !ok {
		return types.NoTypeID
	}
	tc.recordIsOperand(exprID, operand)
	if leftType == types.NoTypeID {
		return types.NoTypeID
	}
	return tc.types.Builtins().Bool
}

func (tc *typeChecker) typeHeirExpr(exprID ast.ExprID, leftType types.TypeID, rightExpr ast.ExprID, op ast.ExprBinaryOp) types.TypeID {
	rightType, okRight := tc.resolveTypeOperand(rightExpr, tc.binaryOpLabel(op))
	if !okRight {
		return types.NoTypeID
	}
	if leftType == types.NoTypeID {
		return types.NoTypeID
	}
	tc.recordHeirOperand(exprID, leftType, rightType)
	return tc.types.Builtins().Bool
}

func (tc *typeChecker) recordIsOperand(exprID ast.ExprID, operand IsOperand) {
	if tc.result == nil {
		return
	}
	if tc.result.IsOperands == nil {
		tc.result.IsOperands = make(map[ast.ExprID]IsOperand)
	}
	tc.result.IsOperands[exprID] = operand
}

func (tc *typeChecker) recordHeirOperand(exprID ast.ExprID, leftType, rightType types.TypeID) {
	if tc.result == nil {
		return
	}
	if tc.result.HeirOperands == nil {
		tc.result.HeirOperands = make(map[ast.ExprID]HeirOperand)
	}
	tc.result.HeirOperands[exprID] = HeirOperand{Left: leftType, Right: rightType}
}

func (tc *typeChecker) recordMagicUnarySymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.MagicUnarySymbols == nil {
		tc.result.MagicUnarySymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.MagicUnarySymbols[exprID] = symID
}

func (tc *typeChecker) recordMagicOpInstantiation(symID symbols.SymbolID, recv types.TypeID, span source.Span) {
	if tc == nil || !symID.IsValid() {
		return
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || len(sym.TypeParams) == 0 {
		return
	}
	recvArgs := tc.receiverTypeArgs(recv)
	if len(recvArgs) == 0 || len(recvArgs) != len(sym.TypeParams) {
		return
	}
	tc.rememberFunctionInstantiation(symID, recvArgs, span, "magic-op")
}

func (tc *typeChecker) recordMagicBinarySymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.MagicBinarySymbols == nil {
		tc.result.MagicBinarySymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.MagicBinarySymbols[exprID] = symID
}

func (tc *typeChecker) recordIndexSymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.IndexSymbols == nil {
		tc.result.IndexSymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.IndexSymbols[exprID] = symID
}

func (tc *typeChecker) recordIndexSetSymbol(exprID ast.ExprID, symID symbols.SymbolID) {
	if tc.result == nil || !symID.IsValid() {
		return
	}
	if tc.result.IndexSetSymbols == nil {
		tc.result.IndexSetSymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	tc.result.IndexSetSymbols[exprID] = symID
}

func (tc *typeChecker) resolveIsOperand(leftType types.TypeID, rightExpr ast.ExprID, opLabel string) (IsOperand, bool) {
	if tc.builder == nil || !rightExpr.IsValid() {
		tc.reportExpectTypeOperand(opLabel, rightExpr)
		return IsOperand{}, false
	}
	expr := tc.builder.Exprs.Get(rightExpr)
	if expr == nil {
		tc.reportExpectTypeOperand(opLabel, rightExpr)
		return IsOperand{}, false
	}
	switch expr.Kind {
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(rightExpr); ok && group != nil {
			return tc.resolveIsOperand(leftType, group.Inner, opLabel)
		}
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(rightExpr); ok && ident != nil {
			if tag, ok := tc.resolveTagOperand(rightExpr, ident.Name); ok {
				if tc.validateIsTagOperand(leftType, tag, rightExpr) {
					return IsOperand{Kind: IsOperandTag, Tag: tag}, true
				}
				return IsOperand{}, false
			}
		}
	case ast.ExprLit:
		if lit, ok := tc.builder.Exprs.Literal(rightExpr); ok && lit != nil && lit.Kind == ast.ExprLitNothing {
			if tc.isUnionNothingOperand(leftType) {
				return IsOperand{Kind: IsOperandTag, Tag: tc.builder.StringsInterner.Intern("nothing")}, true
			}
			return IsOperand{Kind: IsOperandType, Type: tc.types.Builtins().Nothing}, true
		}
	}
	if ty, ok := tc.resolveTypeOperand(rightExpr, opLabel); ok {
		return IsOperand{Kind: IsOperandType, Type: ty}, true
	}
	return IsOperand{}, false
}

func (tc *typeChecker) resolveTagOperand(exprID ast.ExprID, name source.StringID) (source.StringID, bool) {
	if name == source.NoStringID || tc.builder == nil || tc.symbols == nil {
		return source.NoStringID, false
	}
	if symID := tc.symbolForExpr(exprID); symID.IsValid() {
		if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolTag {
			return sym.Name, true
		}
		return source.NoStringID, false
	}
	scope := tc.scopeOrFile(tc.currentScope())
	if symID := tc.lookupTagSymbol(name, scope); symID.IsValid() {
		if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolTag {
			return sym.Name, true
		}
	}
	return source.NoStringID, false
}

func (tc *typeChecker) validateIsTagOperand(leftType types.TypeID, tag source.StringID, exprID ast.ExprID) bool {
	if leftType == types.NoTypeID || tc.types == nil {
		return true
	}
	info, unionType := tc.unionInfoForIs(leftType)
	if info == nil {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(exprID),
			"tag %s requires a union value, got %s",
			tc.lookupName(tag), tc.typeLabel(leftType))
		return false
	}
	if !tc.unionHasTag(info, tag) {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(exprID),
			"tag %s is not a member of %s",
			tc.lookupName(tag), tc.typeLabel(unionType))
		return false
	}
	return true
}

func (tc *typeChecker) isUnionNothingOperand(leftType types.TypeID) bool {
	if leftType == types.NoTypeID || tc.types == nil {
		return false
	}
	info, _ := tc.unionInfoForIs(leftType)
	if info == nil {
		return false
	}
	return tc.unionHasNothing(info)
}

func (tc *typeChecker) unionInfoForIs(leftType types.TypeID) (*types.UnionInfo, types.TypeID) {
	normalized := tc.stripOwnType(leftType)
	normalized = tc.resolveAlias(normalized)
	normalized = tc.stripOwnType(normalized)
	if normalized == types.NoTypeID || tc.types == nil {
		return nil, types.NoTypeID
	}
	tt, ok := tc.types.Lookup(normalized)
	if !ok || tt.Kind != types.KindUnion {
		return nil, normalized
	}
	info, ok := tc.types.UnionInfo(normalized)
	if !ok || info == nil {
		return nil, normalized
	}
	return info, normalized
}

func (tc *typeChecker) unionHasTag(info *types.UnionInfo, tag source.StringID) bool {
	if info == nil || tag == source.NoStringID {
		return false
	}
	for _, member := range info.Members {
		if member.Kind == types.UnionMemberTag && member.TagName == tag {
			return true
		}
	}
	return false
}

func (tc *typeChecker) unionHasNothing(info *types.UnionInfo) bool {
	if info == nil {
		return false
	}
	for _, member := range info.Members {
		if member.Kind == types.UnionMemberNothing {
			return true
		}
	}
	return false
}

func (tc *typeChecker) reportExpectTypeOperand(opLabel string, operand ast.ExprID) {
	if tc.reporter == nil {
		return
	}
	span := tc.exprSpan(operand)
	msg := fmt.Sprintf("operator %s requires a type operand", opLabel)
	b := diag.ReportError(tc.reporter, diag.SemaExpectTypeOperand, span, msg)
	if b == nil {
		return
	}
	if replacement := tc.typeOperandReplacement(operand); replacement != "" {
		fixEdit := fix.ReplaceSpan(
			fmt.Sprintf("replace with %s", replacement),
			span,
			replacement,
			"",
			fix.WithKind(diag.FixKindQuickFix),
		)
		b.WithFixSuggestion(fixEdit)
	}
	b.Emit()
}

func (tc *typeChecker) typeOperandReplacement(operand ast.ExprID) string {
	if !operand.IsValid() {
		return ""
	}
	expr := tc.builder.Exprs.Get(operand)
	if expr == nil {
		return ""
	}
	switch expr.Kind {
	case ast.ExprIdent:
		if symID := tc.symbolForExpr(operand); symID.IsValid() {
			if sym := tc.symbolFromID(symID); sym != nil {
				switch sym.Kind {
				case symbols.SymbolLet, symbols.SymbolParam:
					if ty := tc.bindingType(symID); ty != types.NoTypeID {
						return tc.typeLabel(ty)
					}
				}
			}
		}
	case ast.ExprLit:
		if lit, ok := tc.builder.Exprs.Literal(operand); ok && lit != nil {
			if ty := tc.literalType(lit.Kind); ty != types.NoTypeID {
				return tc.typeLabel(ty)
			}
		}
	}
	return ""
}

func (tc *typeChecker) typeCompoundAssignment(baseOp, fullOp ast.ExprBinaryOp, span source.Span, leftExpr, rightExpr ast.ExprID, leftType, rightType types.TypeID) types.TypeID {
	if leftType == types.NoTypeID || rightType == types.NoTypeID {
		return types.NoTypeID
	}
	result := tc.magicResultForBinary(leftType, rightType, baseOp)
	if result == types.NoTypeID {
		tc.reportMissingBinaryMethod(fullOp, leftType, rightType, span)
		return types.NoTypeID
	}
	if !tc.sameType(leftType, result) {
		tc.report(diag.SemaTypeMismatch, span, "operator %s changes type from %s to %s", tc.binaryOpLabel(fullOp), tc.typeLabel(leftType), tc.typeLabel(result))
		return types.NoTypeID
	}
	tc.ensureIndexAssignment(leftExpr, leftType, span)
	tc.applyIndexSetterOwnership(leftExpr, rightExpr, rightType)
	tc.handleAssignment(fullOp, leftExpr, rightExpr, span)
	return leftType
}

func (tc *typeChecker) ensureIndexAssignment(expr ast.ExprID, value types.TypeID, span source.Span) {
	if value == types.NoTypeID || !expr.IsValid() || tc.builder == nil {
		return
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil || node.Kind != ast.ExprIndex {
		return
	}
	index, ok := tc.builder.Exprs.Index(expr)
	if !ok || index == nil {
		return
	}
	container := tc.typeExprAssignLHS(index.Target)
	if container == types.NoTypeID {
		return
	}
	indexType := tc.typeExpr(index.Index)
	if tc.hasIndexSetter(container, indexType, value) {
		return
	}
	tc.report(diag.SemaTypeMismatch, span, "%s does not support indexed assignment", tc.typeLabel(container))
}

func (tc *typeChecker) applyIndexSetterOwnership(leftExpr, rightExpr ast.ExprID, value types.TypeID) {
	if value == types.NoTypeID || !leftExpr.IsValid() || tc.builder == nil {
		return
	}
	node := tc.builder.Exprs.Get(leftExpr)
	if node == nil || node.Kind != ast.ExprIndex {
		return
	}
	index, ok := tc.builder.Exprs.Index(leftExpr)
	if !ok || index == nil {
		return
	}
	container := tc.typeExprAssignLHS(index.Target)
	if container == types.NoTypeID {
		return
	}
	indexType := tc.typeExpr(index.Index)
	sig := tc.magicSignatureForIndexSet(container, indexType, value)
	if sig == nil || len(sig.Params) < 3 {
		return
	}
	if symID := tc.magicSymbolForSignature(sig); symID.IsValid() {
		tc.recordIndexSetSymbol(leftExpr, symID)
		tc.recordMethodCallInstantiation(symID, container, nil, tc.exprSpan(leftExpr))
	}
	tc.applyParamOwnership(sig.Params[0], index.Target, container, tc.exprSpan(index.Target))
	tc.applyParamOwnership(sig.Params[1], index.Index, indexType, tc.exprSpan(index.Index))
	if rightExpr.IsValid() {
		tc.applyParamOwnership(sig.Params[2], rightExpr, value, tc.exprSpan(rightExpr))
	}
	tc.dropImplicitBorrowForRefParam(index.Target, sig.Params[0], container, types.NoTypeID, tc.exprSpan(index.Target))
}

func (tc *typeChecker) typeBinaryFallback(span source.Span, data *ast.ExprBinaryData, leftType, rightType types.TypeID) types.TypeID {
	specs := types.BinarySpecs(data.Op)
	if len(specs) == 0 {
		tc.reportMissingBinaryMethod(data.Op, leftType, rightType, span)
		return types.NoTypeID
	}
	leftFamily := tc.familyOf(leftType)
	rightFamily := tc.familyOf(rightType)
	for _, spec := range specs {
		if leftType == types.NoTypeID || rightType == types.NoTypeID {
			continue
		}
		if !tc.familyMatches(leftFamily, spec.Left) || !tc.familyMatches(rightFamily, spec.Right) {
			continue
		}
		switch spec.Result {
		case types.BinaryResultLeft:
			return leftType
		case types.BinaryResultRight:
			return rightType
		case types.BinaryResultBool:
			return tc.types.Builtins().Bool
		case types.BinaryResultRange:
			// Both operands must have compatible types
			if leftType == types.NoTypeID || rightType == types.NoTypeID {
				return types.NoTypeID
			}
			// Determine element type from operands
			var elemType types.TypeID
			//nolint:gocritic // if-else chain is clearer here than switch
			if tc.sameType(leftType, rightType) {
				elemType = leftType
			} else if tc.typesAssignable(leftType, rightType, true) {
				elemType = leftType
			} else if tc.typesAssignable(rightType, leftType, true) {
				elemType = rightType
			} else {
				tc.report(diag.SemaRangeTypeMismatch, span,
					"range operands have incompatible types %s and %s",
					tc.typeLabel(leftType), tc.typeLabel(rightType))
				return types.NoTypeID
			}
			return tc.resolveRangeType(elemType, span, tc.currentScope())
		}
	}
	tc.report(diag.SemaInvalidBinaryOperands, span, "operator %s cannot be applied to %s and %s", tc.binaryOpLabel(data.Op), tc.typeLabel(leftType), tc.typeLabel(rightType))
	return types.NoTypeID
}

func (tc *typeChecker) materializeNumericBinaryLiterals(
	op ast.ExprBinaryOp,
	leftExpr, rightExpr ast.ExprID,
	leftType, rightType types.TypeID,
) (leftOut, rightOut types.TypeID, ok bool) {
	if !isNumericBinaryOp(op) {
		return leftType, rightType, true
	}
	if leftType == types.NoTypeID || rightType == types.NoTypeID {
		return leftType, rightType, true
	}
	if tc.sameType(leftType, rightType) {
		return leftType, rightType, true
	}
	if applied, ok := tc.materializeNumericLiteral(leftExpr, rightType); applied {
		if !ok {
			return leftType, rightType, false
		}
		leftType = rightType
	}
	if applied, ok := tc.materializeNumericLiteral(rightExpr, leftType); applied {
		if !ok {
			return leftType, rightType, false
		}
		rightType = leftType
	}
	return leftType, rightType, true
}

func (tc *typeChecker) enforceSameNumericOperands(op ast.ExprBinaryOp, leftType, rightType types.TypeID, span source.Span) bool {
	if !isNumericBinaryOp(op) {
		return true
	}
	if leftType == types.NoTypeID || rightType == types.NoTypeID {
		return false
	}
	if !tc.isNumericType(leftType) || !tc.isNumericType(rightType) {
		return true
	}
	if tc.sameType(leftType, rightType) {
		return true
	}
	tc.report(diag.SemaInvalidBinaryOperands, span,
		"operator %s requires operands of the same type, got %s and %s",
		tc.binaryOpLabel(op),
		tc.typeLabel(leftType),
		tc.typeLabel(rightType),
	)
	return false
}

func isNumericBinaryOp(op ast.ExprBinaryOp) bool {
	switch op {
	case ast.ExprBinaryAdd,
		ast.ExprBinarySub,
		ast.ExprBinaryMul,
		ast.ExprBinaryDiv,
		ast.ExprBinaryMod,
		ast.ExprBinaryBitAnd,
		ast.ExprBinaryBitOr,
		ast.ExprBinaryBitXor,
		ast.ExprBinaryShiftLeft,
		ast.ExprBinaryShiftRight,
		ast.ExprBinaryEq,
		ast.ExprBinaryNotEq,
		ast.ExprBinaryLess,
		ast.ExprBinaryLessEq,
		ast.ExprBinaryGreater,
		ast.ExprBinaryGreaterEq,
		ast.ExprBinaryAddAssign,
		ast.ExprBinarySubAssign,
		ast.ExprBinaryMulAssign,
		ast.ExprBinaryDivAssign,
		ast.ExprBinaryModAssign,
		ast.ExprBinaryBitAndAssign,
		ast.ExprBinaryBitOrAssign,
		ast.ExprBinaryBitXorAssign,
		ast.ExprBinaryShlAssign,
		ast.ExprBinaryShrAssign:
		return true
	default:
		return false
	}
}
