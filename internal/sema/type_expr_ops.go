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
	case ast.ExprUnaryAwait, ast.ExprUnaryOwn:
		return operandType
	default:
		if magic := tc.magicResultForUnary(operandType, data.Op); magic != types.NoTypeID {
			return magic
		}
		tc.reportMissingUnaryMethod(data.Op, operandType, span)
		return types.NoTypeID
	}
}

func (tc *typeChecker) typeBinary(span source.Span, data *ast.ExprBinaryData) types.TypeID {
	leftType := tc.typeExpr(data.Left)
	if data.Op == ast.ExprBinaryAssign {
		tc.ensureIndexAssignment(data.Left, leftType, span)
		tc.handleAssignment(data.Op, data.Left, data.Right, span)
		return leftType
	}
	switch data.Op {
	case ast.ExprBinaryIs:
		return tc.typeIsExpr(leftType, data.Right, data.Op)
	case ast.ExprBinaryHeir:
		return tc.typeHeirExpr(leftType, data.Right, data.Op)
	}
	rightType := tc.typeExpr(data.Right)
	if baseOp, ok := tc.assignmentBaseOp(data.Op); ok {
		return tc.typeCompoundAssignment(baseOp, data.Op, span, data.Left, data.Right, leftType, rightType)
	}
	if magic := tc.magicResultForBinary(leftType, rightType, data.Op); magic != types.NoTypeID {
		return magic
	}
	return tc.typeBinaryFallback(span, data, leftType, rightType)
}

func (tc *typeChecker) typeIsExpr(leftType types.TypeID, rightExpr ast.ExprID, op ast.ExprBinaryOp) types.TypeID {
	if _, ok := tc.resolveTypeOperand(rightExpr, tc.binaryOpLabel(op)); !ok {
		return types.NoTypeID
	}
	if leftType == types.NoTypeID {
		return types.NoTypeID
	}
	return tc.types.Builtins().Bool
}

func (tc *typeChecker) typeHeirExpr(leftType types.TypeID, rightExpr ast.ExprID, op ast.ExprBinaryOp) types.TypeID {
	if _, ok := tc.resolveTypeOperand(rightExpr, tc.binaryOpLabel(op)); !ok {
		return types.NoTypeID
	}
	if leftType == types.NoTypeID {
		return types.NoTypeID
	}
	return tc.types.Builtins().Bool
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
