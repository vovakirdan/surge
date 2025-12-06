package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

func (tc *typeChecker) elementType(id types.TypeID) (types.TypeID, bool) {
	if tc.types == nil {
		return types.NoTypeID, false
	}
	if elem, ok := tc.arrayElemType(id); ok {
		return elem, true
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return types.NoTypeID, false
	}
	switch tt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn:
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
	if tc.isArrayType(id) {
		return types.FamilyArray
	}
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
	case types.KindConst:
		return types.FamilyUnsignedInt
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
	if elem, length, ok := tc.arrayFixedInfo(id); ok && tt.Kind != types.KindAlias {
		if length > 0 {
			return fmt.Sprintf("[%s; %d]", tc.typeLabel(elem), length)
		}
		return fmt.Sprintf("[%s]", tc.typeLabel(elem))
	}
	if elem, ok := tc.arrayElemType(id); ok && tt.Kind != types.KindAlias {
		return fmt.Sprintf("[%s]", tc.typeLabel(elem))
	}
	switch tt.Kind {
	case types.KindBool:
		return "bool"
	case types.KindInt:
		return numericTypeLabel("int", tt.Width)
	case types.KindUint:
		return numericTypeLabel("uint", tt.Width)
	case types.KindFloat:
		return numericTypeLabel("float", tt.Width)
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
	case types.KindConst:
		return fmt.Sprintf("%d", tt.Count)
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
	case types.KindTuple:
		if info, ok := tc.types.TupleInfo(id); ok && info != nil {
			elems := make([]string, 0, len(info.Elems))
			for _, e := range info.Elems {
				elems = append(elems, tc.typeLabel(e))
			}
			if len(elems) == 1 {
				return "(" + elems[0] + ",)"
			}
			return "(" + strings.Join(elems, ", ") + ")"
		}
		return "()"
	default:
		return tt.Kind.String()
	}
}

func numericTypeLabel(base string, width types.Width) string {
	switch width {
	case types.WidthAny:
		return base
	case types.Width8, types.Width16, types.Width32, types.Width64:
		return fmt.Sprintf("%s%d", base, width)
	default:
		return base
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

func (tc *typeChecker) isAddressLike(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn:
		return true
	default:
		return false
	}
}

func (tc *typeChecker) substituteTypeParams(id types.TypeID, mapping map[types.TypeID]types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil || len(mapping) == 0 {
		return id
	}
	resolved := tc.resolveAlias(id)
	if repl, ok := mapping[resolved]; ok && repl != types.NoTypeID {
		return repl
	}
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return resolved
	}
	if tt.Kind == types.KindStruct {
		if elem, ok := tc.arrayElemType(resolved); ok {
			inner := tc.substituteTypeParams(elem, mapping)
			if inner == elem {
				return resolved
			}
			return tc.instantiateArrayType(inner)
		}
	}
	switch tt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn:
		elem := tc.substituteTypeParams(tt.Elem, mapping)
		if elem == tt.Elem {
			return resolved
		}
		clone := tt
		clone.Elem = elem
		return tc.types.Intern(clone)
	case types.KindArray:
		elem := tc.substituteTypeParams(tt.Elem, mapping)
		if elem == tt.Elem {
			return resolved
		}
		clone := tt
		clone.Elem = elem
		return tc.types.Intern(clone)
	default:
		return resolved
	}
}
