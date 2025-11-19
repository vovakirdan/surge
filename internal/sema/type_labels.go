package sema

import (
	"fmt"
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
	case ast.ExprBinaryAddAssign:
		return "+="
	case ast.ExprBinarySubAssign:
		return "-="
	case ast.ExprBinaryMulAssign:
		return "*="
	case ast.ExprBinaryDivAssign:
		return "/="
	case ast.ExprBinaryModAssign:
		return "%="
	case ast.ExprBinaryBitAndAssign:
		return "&="
	case ast.ExprBinaryBitOrAssign:
		return "|="
	case ast.ExprBinaryBitXorAssign:
		return "^="
	case ast.ExprBinaryShlAssign:
		return "<<="
	case ast.ExprBinaryShrAssign:
		return ">>="
	case ast.ExprBinaryAssign:
		return "="
	case ast.ExprBinaryNullCoalescing:
		return "??"
	case ast.ExprBinaryRange:
		return ".."
	case ast.ExprBinaryRangeInclusive:
		return "..="
	case ast.ExprBinaryIs:
		return "is"
	case ast.ExprBinaryHeir:
		return "heir"
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
	case ast.ExprUnaryOwn:
		return "own"
	case ast.ExprUnaryAwait:
		return "await"
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
	case types.KindArray:
		return symbols.TypeKey("[]")
	case types.KindNothing:
		return symbols.TypeKey("nothing")
	case types.KindUnit:
		return symbols.TypeKey("unit")
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				return symbols.TypeKey(name)
			}
		}
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				return symbols.TypeKey(name)
			}
		}
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				return symbols.TypeKey(name)
			}
		}
	default:
		return ""
	}
	return ""
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
		if tc.typeKeys != nil {
			if ty := tc.typeKeys[string(key)]; ty != types.NoTypeID {
				return ty
			}
		}
		return types.NoTypeID
	}
}

func binaryAssignmentBaseOp(op ast.ExprBinaryOp) (ast.ExprBinaryOp, bool) {
	switch op {
	case ast.ExprBinaryAddAssign:
		return ast.ExprBinaryAdd, true
	case ast.ExprBinarySubAssign:
		return ast.ExprBinarySub, true
	case ast.ExprBinaryMulAssign:
		return ast.ExprBinaryMul, true
	case ast.ExprBinaryDivAssign:
		return ast.ExprBinaryDiv, true
	case ast.ExprBinaryModAssign:
		return ast.ExprBinaryMod, true
	case ast.ExprBinaryBitAndAssign:
		return ast.ExprBinaryBitAnd, true
	case ast.ExprBinaryBitOrAssign:
		return ast.ExprBinaryBitOr, true
	case ast.ExprBinaryBitXorAssign:
		return ast.ExprBinaryBitXor, true
	case ast.ExprBinaryShlAssign:
		return ast.ExprBinaryShiftLeft, true
	case ast.ExprBinaryShrAssign:
		return ast.ExprBinaryShiftRight, true
	default:
		return 0, false
	}
}
