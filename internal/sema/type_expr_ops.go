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

type IsOperandKind uint8

const (
	IsOperandType IsOperandKind = iota
	IsOperandTag
)

type IsOperand struct {
	Kind IsOperandKind
	Type types.TypeID
	Tag  source.StringID
}

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
		return operandType
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
		if magic := tc.magicResultForUnary(operandType, data.Op); magic != types.NoTypeID {
			return magic
		}
		tc.reportMissingUnaryMethod(data.Op, operandType, span)
		return types.NoTypeID
	}
}

func (tc *typeChecker) typeBinary(exprID ast.ExprID, span source.Span, data *ast.ExprBinaryData) types.TypeID {
	if data.Op == ast.ExprBinaryHeir {
		return tc.typeHeirExpr(exprID, data.Left, data.Right, data.Op)
	}
	leftType := tc.typeExpr(data.Left)
	if data.Op == ast.ExprBinaryAssign {
		rightType := tc.typeExpr(data.Right)
		tc.ensureBindingTypeMatch(ast.NoTypeID, leftType, rightType, data.Right)
		tc.ensureIndexAssignment(data.Left, leftType, span)
		tc.handleAssignment(data.Op, data.Left, data.Right, span)
		tc.updateArrayViewBindingFromAssign(data.Left, data.Right)
		return leftType
	}
	if data.Op == ast.ExprBinaryIs {
		return tc.typeIsExpr(exprID, leftType, data.Right, data.Op)
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
	if magic := tc.magicResultForBinary(leftType, rightType, data.Op); magic != types.NoTypeID {
		return magic
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

func (tc *typeChecker) typeHeirExpr(exprID, leftExpr, rightExpr ast.ExprID, op ast.ExprBinaryOp) types.TypeID {
	leftType, okLeft := tc.resolveTypeOperand(leftExpr, tc.binaryOpLabel(op))
	rightType, okRight := tc.resolveTypeOperand(rightExpr, tc.binaryOpLabel(op))
	if !okLeft || !okRight {
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

func (tc *typeChecker) stripOwnType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}
	for range 32 {
		tt, ok := tc.types.Lookup(id)
		if !ok || tt.Kind != types.KindOwn {
			return id
		}
		id = tt.Elem
	}
	return id
}

func (tc *typeChecker) resolveTypeOperand(exprID ast.ExprID, opLabel string) (types.TypeID, bool) {
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		tc.reportExpectTypeOperand(opLabel, exprID)
		return types.NoTypeID, false
	}
	switch expr.Kind {
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(exprID); ok && group != nil {
			return tc.resolveTypeOperand(group.Inner, opLabel)
		}
	case ast.ExprUnary:
		if unary, ok := tc.builder.Exprs.Unary(exprID); ok && unary != nil {
			switch unary.Op {
			case ast.ExprUnaryOwn:
				if inner, ok := tc.resolveTypeOperand(unary.Operand, opLabel); ok {
					return tc.types.Intern(types.MakeOwn(inner)), true
				}
			case ast.ExprUnaryRef, ast.ExprUnaryRefMut:
				if inner, ok := tc.resolveTypeOperand(unary.Operand, opLabel); ok {
					mutable := unary.Op == ast.ExprUnaryRefMut
					return tc.types.Intern(types.MakeReference(inner, mutable)), true
				}
			case ast.ExprUnaryDeref:
				if inner, ok := tc.resolveTypeOperand(unary.Operand, opLabel); ok {
					return tc.types.Intern(types.MakePointer(inner)), true
				}
			}
		}
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(exprID); ok && ident != nil {
			if symID := tc.symbolForExpr(exprID); symID.IsValid() {
				if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolType {
					return sym.Type, true
				}
			}
			if literal := tc.lookupName(ident.Name); literal != "" {
				if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
					return builtin, true
				}
			}
			scope := tc.scopeOrFile(tc.currentScope())
			if symID := tc.lookupTypeSymbol(ident.Name, scope); symID.IsValid() {
				return tc.symbolType(symID), true
			}
		}
	case ast.ExprLit:
		// Handle 'nothing' literal as type operand
		if lit, ok := tc.builder.Exprs.Literal(exprID); ok && lit != nil {
			if lit.Kind == ast.ExprLitNothing {
				return tc.types.Builtins().Nothing, true
			}
		}
	default:
		// fallthrough to error reporting
	}
	tc.reportExpectTypeOperand(opLabel, exprID)
	return types.NoTypeID, false
}

// tryResolveTypeOperand attempts to resolve an expression used as a type operand without emitting diagnostics.
func (tc *typeChecker) tryResolveTypeOperand(exprID ast.ExprID) types.TypeID {
	if !exprID.IsValid() || tc.builder == nil {
		return types.NoTypeID
	}
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return types.NoTypeID
	}
	switch expr.Kind {
	case ast.ExprGroup:
		if group, ok := tc.builder.Exprs.Group(exprID); ok && group != nil {
			return tc.tryResolveTypeOperand(group.Inner)
		}
	case ast.ExprUnary:
		if unary, ok := tc.builder.Exprs.Unary(exprID); ok && unary != nil {
			switch unary.Op {
			case ast.ExprUnaryOwn:
				if inner := tc.tryResolveTypeOperand(unary.Operand); inner != types.NoTypeID {
					return tc.types.Intern(types.MakeOwn(inner))
				}
			case ast.ExprUnaryRef, ast.ExprUnaryRefMut:
				if inner := tc.tryResolveTypeOperand(unary.Operand); inner != types.NoTypeID {
					mutable := unary.Op == ast.ExprUnaryRefMut
					return tc.types.Intern(types.MakeReference(inner, mutable))
				}
			case ast.ExprUnaryDeref:
				if inner := tc.tryResolveTypeOperand(unary.Operand); inner != types.NoTypeID {
					return tc.types.Intern(types.MakePointer(inner))
				}
			}
		}
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(exprID); ok && ident != nil {
			if symID := tc.symbolForExpr(exprID); symID.IsValid() {
				if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolType && sym.Type != types.NoTypeID {
					return sym.Type
				}
			}
			if param := tc.lookupTypeParam(ident.Name); param != types.NoTypeID {
				return param
			}
			if literal := tc.lookupName(ident.Name); literal != "" {
				if builtin := tc.builtinTypeByName(literal); builtin != types.NoTypeID {
					return builtin
				}
			}
			scope := tc.scopeOrFile(tc.currentScope())
			if symID := tc.lookupTypeSymbol(ident.Name, scope); symID.IsValid() {
				return tc.symbolType(symID)
			}
		}
	case ast.ExprLit:
		// Handle 'nothing' literal as type operand
		if lit, ok := tc.builder.Exprs.Literal(exprID); ok && lit != nil {
			if lit.Kind == ast.ExprLitNothing {
				return tc.types.Builtins().Nothing
			}
		}
	}
	return types.NoTypeID
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
	container := tc.typeExpr(index.Target)
	if container == types.NoTypeID {
		return
	}
	indexType := tc.typeExpr(index.Index)
	if tc.hasIndexSetter(container, indexType, value) {
		return
	}
	tc.report(diag.SemaTypeMismatch, span, "%s does not support indexed assignment", tc.typeLabel(container))
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
