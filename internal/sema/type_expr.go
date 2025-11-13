package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
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
			ty = tc.typeBinary(id, expr.Span, data)
		}
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(id); ok && call != nil {
			tc.typeExpr(call.Target)
			for _, arg := range call.Args {
				tc.typeExpr(arg)
				tc.observeMove(arg, tc.exprSpan(arg))
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
			ty = tc.typeExpr(cast.Value)
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
	spec, ok := types.UnarySpecFor(data.Op)
	if !ok {
		return operandType
	}
	if operandType == types.NoTypeID && spec.Result != types.UnaryResultReference {
		return types.NoTypeID
	}
	family := tc.familyOf(operandType)
	if spec.Result != types.UnaryResultReference && !tc.familyMatches(family, spec.Operand) {
		tc.report(diag.SemaInvalidUnaryOperand, span, "operator %s cannot be applied to %s", tc.unaryOpLabel(data.Op), tc.typeLabel(operandType))
		return types.NoTypeID
	}
	if magic := tc.magicResultForUnary(operandType, data.Op); magic != types.NoTypeID {
		return magic
	}
	switch spec.Result {
	case types.UnaryResultNumeric, types.UnaryResultSame:
		return operandType
	case types.UnaryResultBool:
		return tc.types.Builtins().Bool
	case types.UnaryResultReference:
		tc.handleBorrow(exprID, span, data.Op, data.Operand)
		if operandType == types.NoTypeID {
			return types.NoTypeID
		}
		mutable := data.Op == ast.ExprUnaryRefMut
		return tc.types.Intern(types.MakeReference(operandType, mutable))
	case types.UnaryResultDeref:
		elem, ok := tc.elementType(operandType)
		if !ok {
			tc.report(diag.SemaInvalidUnaryOperand, span, "cannot dereference %s", tc.typeLabel(operandType))
			return types.NoTypeID
		}
		return elem
	case types.UnaryResultAwait:
		return operandType
	default:
		return types.NoTypeID
	}
}

func (tc *typeChecker) typeBinary(exprID ast.ExprID, span source.Span, data *ast.ExprBinaryData) types.TypeID {
	leftType := tc.typeExpr(data.Left)
	rightType := tc.typeExpr(data.Right)
	specs := types.BinarySpecs(data.Op)
	if len(specs) == 0 {
		return types.NoTypeID
	}
	leftFamily := tc.familyOf(leftType)
	rightFamily := tc.familyOf(rightType)
	for _, spec := range specs {
		assign := spec.Flags&types.BinaryFlagAssignment != 0
		if !assign {
			if leftType == types.NoTypeID || rightType == types.NoTypeID {
				continue
			}
			if !tc.familyMatches(leftFamily, spec.Left) || !tc.familyMatches(rightFamily, spec.Right) {
				continue
			}
			if spec.Flags&types.BinaryFlagSameFamily != 0 && leftFamily != rightFamily {
				continue
			}
		}
		switch spec.Result {
		case types.BinaryResultLeft:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return leftType
		case types.BinaryResultRight:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return rightType
		case types.BinaryResultBool:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return tc.types.Builtins().Bool
		case types.BinaryResultNumeric:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return tc.pickNumericResult(leftType, rightType)
		case types.BinaryResultRange:
			tc.handleAssignmentIfNeeded(data.Op, data.Left, data.Right, span, spec.Flags)
			return types.NoTypeID
		default:
			return types.NoTypeID
		}
	}
	if magic := tc.magicResultForBinary(leftType, rightType, data.Op); magic != types.NoTypeID {
		return magic
	}
	tc.report(diag.SemaInvalidBinaryOperands, span, "operator %s is not defined for %s and %s", tc.binaryOpLabel(data.Op), tc.typeLabel(leftType), tc.typeLabel(rightType))
	return types.NoTypeID
}

func (tc *typeChecker) pickNumericResult(left, right types.TypeID) types.TypeID {
	if left != types.NoTypeID {
		return left
	}
	if right != types.NoTypeID {
		return right
	}
	return tc.types.Builtins().Int
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
			if name := tc.lookupName(info.Name); name != "" {
				return name
			}
		}
		return "struct"
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(id); ok && info != nil {
			if name := tc.lookupName(info.Name); name != "" {
				return name
			}
		}
		if target, ok := tc.types.AliasTarget(id); ok && target != types.NoTypeID {
			return tc.typeLabel(target)
		}
		return "alias"
	default:
		return tt.Kind.String()
	}
}

func (tc *typeChecker) indexResultType(container types.TypeID, span source.Span) types.TypeID {
	if container == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	base := tc.valueType(container)
	if base == types.NoTypeID {
		return types.NoTypeID
	}
	tt, ok := tc.types.Lookup(base)
	if !ok {
		return types.NoTypeID
	}
	switch tt.Kind {
	case types.KindArray:
		return tt.Elem
	case types.KindString:
		return tc.types.Builtins().Uint
	default:
		tc.report(diag.SemaTypeMismatch, span, "%s is not indexable", tc.typeLabel(base))
		return types.NoTypeID
	}
}

func (tc *typeChecker) memberResultType(base types.TypeID, field source.StringID, span source.Span) types.TypeID {
	if base == types.NoTypeID || field == source.NoStringID {
		return types.NoTypeID
	}
	info, structType := tc.structInfoForType(base)
	if info == nil {
		tc.report(diag.SemaTypeMismatch, span, "%s has no fields", tc.typeLabel(base))
		return types.NoTypeID
	}
	for _, f := range info.Fields {
		if f.Name == field {
			return f.Type
		}
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "%s has no field %s", tc.typeLabel(structType), tc.lookupName(field))
	return types.NoTypeID
}

func (tc *typeChecker) validateStructLiteralFields(structType types.TypeID, data *ast.ExprStructData, span source.Span) {
	info, normalized := tc.structInfoForType(structType)
	if info == nil {
		tc.report(diag.SemaTypeMismatch, span, "%s is not a struct", tc.typeLabel(structType))
		return
	}
	if data.Positional {
		tc.validatePositionalStructLiteral(normalized, info, data, span)
		return
	}
	fieldMap := make(map[source.StringID]types.StructField, len(info.Fields))
	for _, f := range info.Fields {
		fieldMap[f.Name] = f
	}
	seen := make(map[source.StringID]struct{}, len(info.Fields))
	for _, field := range data.Fields {
		spec, ok := fieldMap[field.Name]
		if !ok {
			tc.report(diag.SemaUnresolvedSymbol, span, "%s has no field %s", tc.typeLabel(normalized), tc.lookupName(field.Name))
			continue
		}
		tc.ensureStructFieldType(field.Name, field.Value, spec.Type)
		if _, dup := seen[field.Name]; dup {
			tc.report(diag.SemaTypeMismatch, span, "field %s specified multiple times", tc.lookupName(field.Name))
		} else {
			seen[field.Name] = struct{}{}
		}
	}
	if len(seen) != len(info.Fields) {
		tc.report(diag.SemaTypeMismatch, span, "%s literal is missing %d field(s)", tc.typeLabel(normalized), len(info.Fields)-len(seen))
	}
}

func (tc *typeChecker) validatePositionalStructLiteral(structType types.TypeID, info *types.StructInfo, data *ast.ExprStructData, span source.Span) {
	if info == nil {
		return
	}
	if len(data.Fields) != len(info.Fields) {
		tc.report(diag.SemaTypeMismatch, span, "%s literal expects %d fields, got %d", tc.typeLabel(structType), len(info.Fields), len(data.Fields))
	}
	limit := len(data.Fields)
	if len(info.Fields) < limit {
		limit = len(info.Fields)
	}
	for i := 0; i < limit; i++ {
		data.Fields[i].Name = info.Fields[i].Name
		tc.ensureStructFieldType(info.Fields[i].Name, data.Fields[i].Value, info.Fields[i].Type)
	}
}

func (tc *typeChecker) ensureStructFieldType(name source.StringID, value ast.ExprID, expected types.TypeID) {
	if expected == types.NoTypeID || !value.IsValid() {
		return
	}
	actual := tc.typeExpr(value)
	if actual == types.NoTypeID {
		return
	}
	if tc.valueType(actual) == tc.valueType(expected) {
		return
	}
	fieldName := tc.lookupName(name)
	tc.report(diag.SemaTypeMismatch, tc.exprSpan(value), "field %s expects %s, got %s", fieldName, tc.typeLabel(expected), tc.typeLabel(actual))
}

func (tc *typeChecker) structInfoForType(id types.TypeID) (*types.StructInfo, types.TypeID) {
	if id == types.NoTypeID || tc.types == nil {
		return nil, types.NoTypeID
	}
	val := tc.valueType(id)
	if val == types.NoTypeID {
		return nil, types.NoTypeID
	}
	info, ok := tc.types.StructInfo(val)
	if !ok {
		return nil, val
	}
	return info, val
}

func (tc *typeChecker) valueType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	for {
		id = tc.resolveAlias(id)
		tt, ok := tc.types.Lookup(id)
		if !ok {
			return types.NoTypeID
		}
		switch tt.Kind {
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
}

func (tc *typeChecker) resolveAlias(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return id
	}
	const maxDepth = 32
	for depth := 0; depth < maxDepth; depth++ {
		tt, ok := tc.types.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := tc.types.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
	}
	return id
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

func (tc *typeChecker) binaryOpLabel(op ast.ExprBinaryOp) string {
	switch op {
	case ast.ExprBinaryAdd:
		return "+"
	case ast.ExprBinarySub:
		return "-"
	case ast.ExprBinaryMul:
		return "*"
	case ast.ExprBinaryDiv:
		return "/"
	case ast.ExprBinaryMod:
		return "%"
	case ast.ExprBinaryLogicalAnd:
		return "&&"
	case ast.ExprBinaryLogicalOr:
		return "||"
	case ast.ExprBinaryBitAnd:
		return "&"
	case ast.ExprBinaryBitOr:
		return "|"
	case ast.ExprBinaryBitXor:
		return "^"
	case ast.ExprBinaryShiftLeft:
		return "<<"
	case ast.ExprBinaryShiftRight:
		return ">>"
	default:
		return fmt.Sprintf("op#%d", op)
	}
}

func (tc *typeChecker) unaryOpLabel(op ast.ExprUnaryOp) string {
	switch op {
	case ast.ExprUnaryPlus:
		return "+"
	case ast.ExprUnaryMinus:
		return "-"
	case ast.ExprUnaryNot:
		return "!"
	case ast.ExprUnaryDeref:
		return "*"
	case ast.ExprUnaryRef:
		return "&"
	case ast.ExprUnaryRefMut:
		return "&mut"
	default:
		return fmt.Sprintf("op#%d", op)
	}
}

func (tc *typeChecker) symbolName(id source.StringID) string {
	return tc.lookupName(id)
}

func (tc *typeChecker) typeKeyForType(id types.TypeID) symbols.TypeKey {
	if id == types.NoTypeID || tc.types == nil {
		return ""
	}
	tt, ok := tc.types.Lookup(id)
	if !ok {
		return ""
	}
	switch tt.Kind {
	case types.KindBool:
		return symbols.TypeKey("bool")
	case types.KindInt:
		return symbols.TypeKey("int")
	case types.KindUint:
		return symbols.TypeKey("uint")
	case types.KindFloat:
		return symbols.TypeKey("float")
	case types.KindString:
		return symbols.TypeKey("string")
	case types.KindNothing:
		return symbols.TypeKey("nothing")
	case types.KindUnit:
		return symbols.TypeKey("unit")
	default:
		return ""
	}
}

func (tc *typeChecker) typeFromKey(key symbols.TypeKey) types.TypeID {
	if key == "" {
		return types.NoTypeID
	}
	switch string(key) {
	case "bool":
		return tc.types.Builtins().Bool
	case "int":
		return tc.types.Builtins().Int
	case "uint":
		return tc.types.Builtins().Uint
	case "float":
		return tc.types.Builtins().Float
	case "string":
		return tc.types.Builtins().String
	case "nothing":
		return tc.types.Builtins().Nothing
	case "unit":
		return tc.types.Builtins().Unit
	default:
		return types.NoTypeID
	}
}
