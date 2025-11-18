package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
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
			sym := tc.symbolFromID(symID)
			switch {
			case sym == nil:
				ty = types.NoTypeID
			case sym.Kind == symbols.SymbolLet || sym.Kind == symbols.SymbolParam:
				ty = tc.bindingType(symID)
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
				argTypes := make([]types.TypeID, 0, len(call.Args))
				for _, arg := range call.Args {
					argTypes = append(argTypes, tc.typeExpr(arg))
					tc.observeMove(arg, tc.exprSpan(arg))
				}
				ty = tc.methodResultType(member, receiverType, argTypes, expr.Span)
			} else {
				tc.typeExpr(call.Target)
				for _, arg := range call.Args {
					tc.typeExpr(arg)
					tc.observeMove(arg, tc.exprSpan(arg))
				}
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
				ty = tc.types.Intern(types.MakeArray(elemType, types.ArrayDynamicLength))
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
			tc.typeExpr(idx.Index)
			ty = tc.indexResultType(container, expr.Span)
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
			if magic := tc.magicResultForCast(sourceType, targetType); magic != types.NoTypeID {
				ty = magic
			} else {
				tc.reportMissingCastMethod(sourceType, targetType, expr.Span)
			}
		}
	case ast.ExprCompare:
		if cmp, ok := tc.builder.Exprs.Compare(id); ok && cmp != nil {
			tc.typeExpr(cmp.Value)
			for _, arm := range cmp.Arms {
				tc.typeExpr(arm.Pattern)
				tc.typeExpr(arm.Guard)
				tc.typeExpr(arm.Result)
			}
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
	case ast.ExprSpawn:
		if spawn, ok := tc.builder.Exprs.Spawn(id); ok && spawn != nil {
			ty = tc.typeExpr(spawn.Value)
			tc.observeMove(spawn.Value, tc.exprSpan(spawn.Value))
			tc.enforceSpawn(spawn.Value)
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
	default:
		// fallthrough to error reporting
	}
	tc.reportExpectTypeOperand(opLabel, exprID)
	return types.NoTypeID, false
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
	tc.handleAssignment(fullOp, leftExpr, rightExpr, span)
	return leftType
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
			return types.NoTypeID
		}
	}
	tc.report(diag.SemaInvalidBinaryOperands, span, "operator %s cannot be applied to %s and %s", tc.binaryOpLabel(data.Op), tc.typeLabel(leftType), tc.typeLabel(rightType))
	return types.NoTypeID
}

func (tc *typeChecker) elementType(id types.TypeID) (types.TypeID, bool) {
	if tc.types == nil {
		return types.NoTypeID, false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return types.NoTypeID, false
	}
	switch tt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn, types.KindArray:
		return tt.Elem, true
	default:
		return types.NoTypeID, false
	}
}

func (tc *typeChecker) familyOf(id types.TypeID) types.FamilyMask {
	if id == types.NoTypeID || tc.types == nil {
		return types.FamilyNone
	}
	id = tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return types.FamilyNone
	}
	switch tt.Kind {
	case types.KindBool:
		return types.FamilyBool
	case types.KindInt:
		return types.FamilySignedInt
	case types.KindUint:
		return types.FamilyUnsignedInt
	case types.KindFloat:
		return types.FamilyFloat
	case types.KindString:
		return types.FamilyString
	case types.KindArray:
		return types.FamilyArray
	case types.KindPointer:
		return types.FamilyPointer
	case types.KindReference:
		return types.FamilyReference
	case types.KindAlias:
		target, ok := tc.types.AliasTarget(id)
		if !ok {
			return types.FamilyAny
		}
		return tc.familyOf(target)
	default:
		return types.FamilyAny
	}
}

func (tc *typeChecker) familyMatches(actual, expected types.FamilyMask) bool {
	if expected == types.FamilyAny {
		return actual != types.FamilyNone
	}
	return actual&expected != 0
}

func (tc *typeChecker) typeLabel(id types.TypeID) string {
	if id == types.NoTypeID || tc.types == nil {
		return "unknown"
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return "unknown"
	}
	switch tt.Kind {
	case types.KindBool:
		return "bool"
	case types.KindInt:
		return "int"
	case types.KindUint:
		return "uint"
	case types.KindFloat:
		return "float"
	case types.KindString:
		return "string"
	case types.KindNothing:
		return "nothing"
	case types.KindGenericParam:
		if info, ok := tc.types.TypeParamInfo(id); ok && info != nil {
			if name := tc.lookupName(info.Name); name != "" {
				return name
			}
		}
		return "T"
	case types.KindUnit:
		return "unit"
	case types.KindReference:
		prefix := "&"
		if tt.Mutable {
			prefix = "&mut "
		}
		return prefix + tc.typeLabel(tt.Elem)
	case types.KindPointer:
		return fmt.Sprintf("*%s", tc.typeLabel(tt.Elem))
	case types.KindArray:
		return fmt.Sprintf("[%s]", tc.typeLabel(tt.Elem))
	case types.KindOwn:
		return fmt.Sprintf("own %s", tc.typeLabel(tt.Elem))
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				if len(info.TypeArgs) == 0 {
					return name
				}
				args := make([]string, 0, len(info.TypeArgs))
				for _, arg := range info.TypeArgs {
					args = append(args, tc.typeLabel(arg))
				}
				return fmt.Sprintf("%s<%s>", name, strings.Join(args, ", "))
			}
		}
		return "struct"
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				if len(info.TypeArgs) == 0 {
					return name
				}
				args := make([]string, 0, len(info.TypeArgs))
				for _, arg := range info.TypeArgs {
					args = append(args, tc.typeLabel(arg))
				}
				return fmt.Sprintf("%s<%s>", name, strings.Join(args, ", "))
			}
		}
		if target, ok := tc.types.AliasTarget(id); ok && target != types.NoTypeID {
			return tc.typeLabel(target)
		}
		return "alias"
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				if len(info.TypeArgs) == 0 {
					return name
				}
				args := make([]string, 0, len(info.TypeArgs))
				for _, arg := range info.TypeArgs {
					args = append(args, tc.typeLabel(arg))
				}
				return fmt.Sprintf("%s<%s>", name, strings.Join(args, ", "))
			}
		}
		return "union"
	default:
		return tt.Kind.String()
	}
}

func (tc *typeChecker) report(code diag.Code, span source.Span, format string, args ...interface{}) {
	if tc.reporter == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if b := diag.ReportError(tc.reporter, code, span, msg); b != nil {
		b.Emit()
	}
}

func (tc *typeChecker) assignmentBaseOp(op ast.ExprBinaryOp) (ast.ExprBinaryOp, bool) {
	return binaryAssignmentBaseOp(op)
}

func methodNameForBinaryOp(op ast.ExprBinaryOp) string {
	if base, ok := binaryAssignmentBaseOp(op); ok {
		op = base
	}
	return magicNameForBinaryOp(op)
}

func (tc *typeChecker) reportMissingBinaryMethod(op ast.ExprBinaryOp, left, right types.TypeID, span source.Span) {
	name := methodNameForBinaryOp(op)
	label := tc.binaryOpLabel(op)
	if name != "" {
		tc.report(diag.SemaInvalidBinaryOperands, span, "operator %s (%s) is not defined for %s and %s", label, name, tc.typeLabel(left), tc.typeLabel(right))
		return
	}
	tc.report(diag.SemaInvalidBinaryOperands, span, "operator %s is not defined for %s and %s", label, tc.typeLabel(left), tc.typeLabel(right))
}

func (tc *typeChecker) reportMissingUnaryMethod(op ast.ExprUnaryOp, operand types.TypeID, span source.Span) {
	name := magicNameForUnaryOp(op)
	label := tc.unaryOpLabel(op)
	if name != "" {
		tc.report(diag.SemaInvalidUnaryOperand, span, "operator %s (%s) is not defined for %s", label, name, tc.typeLabel(operand))
		return
	}
	tc.report(diag.SemaInvalidUnaryOperand, span, "operator %s is not defined for %s", label, tc.typeLabel(operand))
}

func (tc *typeChecker) reportMissingCastMethod(from, target types.TypeID, span source.Span) {
	tc.report(diag.SemaTypeMismatch, span, "operator to (__to) is not defined for %s and %s", tc.typeLabel(from), tc.typeLabel(target))
}

func (tc *typeChecker) sameType(a, b types.TypeID) bool {
	return a == b
}

func (tc *typeChecker) methodResultType(member *ast.ExprMemberData, recv types.TypeID, args []types.TypeID, span source.Span) types.TypeID {
	if member == nil || tc.magic == nil {
		return types.NoTypeID
	}
	name := tc.lookupExportedName(member.Field)
	if name == "" {
		return types.NoTypeID
	}
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recvCand.key, name)
		for _, sig := range methods {
			if sig == nil || len(sig.Params) == 0 || sig.Params[0] != recvCand.key {
				continue
			}
			if len(sig.Params)-1 != len(args) {
				continue
			}
			if !tc.methodParamsMatch(sig.Params[1:], args) {
				continue
			}
			res := tc.typeFromKey(sig.Result)
			return tc.adjustAliasUnaryResult(res, recvCand)
		}
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "%s has no method %s", tc.typeLabel(recv), name)
	return types.NoTypeID
}

func (tc *typeChecker) methodParamsMatch(expected []symbols.TypeKey, args []types.TypeID) bool {
	if len(expected) != len(args) {
		return false
	}
	for i, arg := range args {
		if !tc.methodParamMatches(expected[i], arg) {
			return false
		}
	}
	return true
}

func (tc *typeChecker) methodParamMatches(expected symbols.TypeKey, arg types.TypeID) bool {
	if expected == "" {
		return false
	}
	for _, cand := range tc.typeKeyCandidates(arg) {
		if cand.key == expected {
			return true
		}
	}
	return false
}
