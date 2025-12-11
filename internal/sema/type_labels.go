package sema

import (
	"fmt"
	"strconv"
	"strings"
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
	if elem, length, ok := tc.arrayFixedInfo(id); ok && tt.Kind != types.KindAlias {
		inner := tc.typeKeyForType(elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		if length > 0 {
			return symbols.TypeKey("[" + string(inner) + "; " + fmt.Sprintf("%d", length) + "]")
		}
		return symbols.TypeKey("[" + string(inner) + "]")
	}
	if elem, ok := tc.arrayElemType(id); ok && tt.Kind != types.KindAlias {
		inner := tc.typeKeyForType(elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		return symbols.TypeKey("[" + string(inner) + "]")
	}
	switch tt.Kind {
	case types.KindBool:
		return symbols.TypeKey("bool")
	case types.KindInt:
		switch tt.Width {
		case types.Width8:
			return symbols.TypeKey("int8")
		case types.Width16:
			return symbols.TypeKey("int16")
		case types.Width32:
			return symbols.TypeKey("int32")
		case types.Width64:
			return symbols.TypeKey("int64")
		default:
			return symbols.TypeKey("int")
		}
	case types.KindUint:
		switch tt.Width {
		case types.Width8:
			return symbols.TypeKey("uint8")
		case types.Width16:
			return symbols.TypeKey("uint16")
		case types.Width32:
			return symbols.TypeKey("uint32")
		case types.Width64:
			return symbols.TypeKey("uint64")
		default:
			return symbols.TypeKey("uint")
		}
	case types.KindFloat:
		switch tt.Width {
		case types.Width16:
			return symbols.TypeKey("float16")
		case types.Width32:
			return symbols.TypeKey("float32")
		case types.Width64:
			return symbols.TypeKey("float64")
		default:
			return symbols.TypeKey("float")
		}
	case types.KindString:
		return symbols.TypeKey("string")
	case types.KindGenericParam:
		if name := tc.typeParamNames[id]; name != source.NoStringID {
			if lookup := tc.lookupName(name); lookup != "" {
				return symbols.TypeKey(lookup)
			}
		}
	case types.KindConst:
		return symbols.TypeKey(fmt.Sprintf("%d", tt.Count))
	case types.KindReference:
		inner := tc.typeKeyForType(tt.Elem)
		if inner != "" {
			prefix := "&"
			if tt.Mutable {
				prefix = "&mut "
			}
			return symbols.TypeKey(prefix + string(inner))
		}
	case types.KindOwn:
		if inner := tc.typeKeyForType(tt.Elem); inner != "" {
			return symbols.TypeKey("own " + string(inner))
		}
	case types.KindPointer:
		if inner := tc.typeKeyForType(tt.Elem); inner != "" {
			return symbols.TypeKey("*" + string(inner))
		}
	case types.KindArray:
		inner := tc.typeKeyForType(tt.Elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		return symbols.TypeKey("[" + string(inner) + "]")
	case types.KindNothing:
		return symbols.TypeKey("nothing")
	case types.KindUnit:
		return symbols.TypeKey("unit")
	case types.KindStruct:
		if info, ok := tc.types.StructInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				if len(info.TypeArgs) > 0 {
					args := make([]string, 0, len(info.TypeArgs))
					for _, arg := range info.TypeArgs {
						if key := tc.typeKeyForType(arg); key != "" {
							args = append(args, string(key))
						}
					}
					return symbols.TypeKey(name + "<" + strings.Join(args, ",") + ">")
				}
				return symbols.TypeKey(name)
			}
		}
	case types.KindAlias:
		if info, ok := tc.types.AliasInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				if len(info.TypeArgs) > 0 {
					args := make([]string, 0, len(info.TypeArgs))
					for _, arg := range info.TypeArgs {
						if key := tc.typeKeyForType(arg); key != "" {
							args = append(args, string(key))
						}
					}
					return symbols.TypeKey(name + "<" + strings.Join(args, ",") + ">")
				}
				return symbols.TypeKey(name)
			}
		}
	case types.KindUnion:
		if info, ok := tc.types.UnionInfo(id); ok && info != nil {
			if name := tc.lookupTypeName(id, info.Name); name != "" {
				if len(info.TypeArgs) > 0 {
					args := make([]string, 0, len(info.TypeArgs))
					for _, arg := range info.TypeArgs {
						if key := tc.typeKeyForType(arg); key != "" {
							args = append(args, string(key))
						}
					}
					return symbols.TypeKey(name + "<" + strings.Join(args, ",") + ">")
				}
				return symbols.TypeKey(name)
			}
		}
	case types.KindTuple:
		if info, ok := tc.types.TupleInfo(id); ok && info != nil {
			elems := make([]string, 0, len(info.Elems))
			for _, e := range info.Elems {
				if key := tc.typeKeyForType(e); key != "" {
					elems = append(elems, string(key))
				}
			}
			return symbols.TypeKey("(" + strings.Join(elems, ",") + ")")
		}
		return symbols.TypeKey("()")
	default:
		return ""
	}
	return ""
}

func (tc *typeChecker) typeFromKey(key symbols.TypeKey) types.TypeID {
	if key == "" || tc.types == nil {
		return types.NoTypeID
	}
	s := strings.TrimSpace(string(key))
	if n, err := strconv.ParseUint(s, 10, 32); err == nil {
		return tc.types.Intern(types.MakeConstUint(uint32(n)))
	}
	if tc.builder != nil {
		nameID := tc.builder.StringsInterner.Intern(s)
		if sym := tc.lookupConstSymbol(nameID, tc.scopeOrFile(tc.currentScope())); sym.IsValid() {
			if val, ok := tc.constUintFromSymbol(sym); ok && val <= uint64(^uint32(0)) {
				return tc.types.Intern(types.MakeConstUint(uint32(val)))
			}
		}
	}
	if inner, lengthKey, length, hasLen, ok := parseArrayKey(s); ok {
		if innerType := tc.typeFromKey(symbols.TypeKey(inner)); innerType != types.NoTypeID {
			if hasLen {
				lenType := types.NoTypeID
				if lengthKey != "" {
					lenType = tc.typeFromKey(symbols.TypeKey(lengthKey))
				}
				if lenType == types.NoTypeID && length <= uint64(^uint32(0)) {
					lenType = tc.types.Intern(types.MakeConstUint(uint32(length)))
				}
				if lenType == types.NoTypeID {
					return types.NoTypeID
				}
				return tc.instantiateArrayFixedWithArg(innerType, lenType)
			}
			return tc.instantiateArrayType(innerType)
		}
	}
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "("), ")"))
		if inner == "" {
			return tc.types.Builtins().Unit
		}
		parts := splitTopLevel(inner)
		elems := make([]types.TypeID, 0, len(parts))
		for _, part := range parts {
			elem := tc.typeFromKey(symbols.TypeKey(part))
			if elem == types.NoTypeID {
				return types.NoTypeID
			}
			elems = append(elems, elem)
		}
		if len(elems) == 0 {
			return tc.types.Builtins().Unit
		}
		return tc.types.RegisterTuple(elems)
	}
	if strings.HasPrefix(s, "fn(") {
		parts := strings.SplitN(strings.TrimPrefix(s, "fn("), ")->", 2)
		if len(parts) != 2 {
			return types.NoTypeID
		}
		paramsPart := strings.TrimSuffix(parts[0], ")")
		resultPart := strings.TrimSpace(parts[1])

		var paramTypes []types.TypeID
		if trimmed := strings.TrimSpace(paramsPart); trimmed != "" {
			paramKeys := splitTopLevel(trimmed)
			paramTypes = make([]types.TypeID, 0, len(paramKeys))
			for _, pk := range paramKeys {
				paramType := tc.typeFromKey(symbols.TypeKey(pk))
				if paramType == types.NoTypeID {
					return types.NoTypeID
				}
				paramTypes = append(paramTypes, paramType)
			}
		}

		resultType := tc.typeFromKey(symbols.TypeKey(resultPart))
		if resultType == types.NoTypeID {
			return types.NoTypeID
		}
		return tc.types.RegisterFn(paramTypes, resultType)
	}
	switch {
	case strings.HasPrefix(s, "&mut "):
		if inner := tc.typeFromKey(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&mut ")))); inner != types.NoTypeID {
			return tc.types.Intern(types.MakeReference(inner, true))
		}
	case strings.HasPrefix(s, "&"):
		if inner := tc.typeFromKey(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "&")))); inner != types.NoTypeID {
			return tc.types.Intern(types.MakeReference(inner, false))
		}
	case strings.HasPrefix(s, "own "):
		if inner := tc.typeFromKey(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "own ")))); inner != types.NoTypeID {
			return tc.types.Intern(types.MakeOwn(inner))
		}
	case strings.HasPrefix(s, "*"):
		if inner := tc.typeFromKey(symbols.TypeKey(strings.TrimSpace(strings.TrimPrefix(s, "*")))); inner != types.NoTypeID {
			return tc.types.Intern(types.MakePointer(inner))
		}
	case strings.HasPrefix(s, "Option<") && strings.HasSuffix(s, ">"):
		innerKey := strings.TrimSuffix(strings.TrimPrefix(s, "Option<"), ">")
		if innerType := tc.typeFromKey(symbols.TypeKey(innerKey)); innerType != types.NoTypeID {
			scope := tc.scopeOrFile(tc.currentScope())
			if opt := tc.resolveOptionType(innerType, source.Span{}, scope); opt != types.NoTypeID {
				return opt
			}
		}
	case strings.HasPrefix(s, "Result<") && strings.HasSuffix(s, ">"):
		content := strings.TrimSuffix(strings.TrimPrefix(s, "Result<"), ">")
		parts := splitTopLevel(content)
		if len(parts) == 2 {
			okType := tc.typeFromKey(symbols.TypeKey(parts[0]))
			errType := tc.typeFromKey(symbols.TypeKey(parts[1]))
			if okType != types.NoTypeID && errType != types.NoTypeID {
				scope := tc.scopeOrFile(tc.currentScope())
				if res := tc.resolveResultType(okType, errType, source.Span{}, scope); res != types.NoTypeID {
					return res
				}
			}
		}
	case strings.HasPrefix(s, "Task<") && strings.HasSuffix(s, ">"):
		innerKey := strings.TrimSuffix(strings.TrimPrefix(s, "Task<"), ">")
		if payload := tc.typeFromKey(symbols.TypeKey(innerKey)); payload != types.NoTypeID {
			scope := tc.scopeOrFile(tc.currentScope())
			taskName := tc.builder.StringsInterner.Intern("Task")
			if ty := tc.resolveNamedType(taskName, []types.TypeID{payload}, nil, source.Span{}, scope); ty != types.NoTypeID {
				return ty
			}
		}
	}
	switch s {
	case "bool":
		return tc.types.Builtins().Bool
	case "int":
		return tc.types.Builtins().Int
	case "int8":
		return tc.types.Builtins().Int8
	case "int16":
		return tc.types.Builtins().Int16
	case "int32":
		return tc.types.Builtins().Int32
	case "int64":
		return tc.types.Builtins().Int64
	case "uint":
		return tc.types.Builtins().Uint
	case "uint8":
		return tc.types.Builtins().Uint8
	case "uint16":
		return tc.types.Builtins().Uint16
	case "uint32":
		return tc.types.Builtins().Uint32
	case "uint64":
		return tc.types.Builtins().Uint64
	case "float":
		return tc.types.Builtins().Float
	case "float16":
		return tc.types.Builtins().Float16
	case "float32":
		return tc.types.Builtins().Float32
	case "float64":
		return tc.types.Builtins().Float64
	case "string":
		return tc.types.Builtins().String
	case "nothing":
		return tc.types.Builtins().Nothing
	case "unit":
		return tc.types.Builtins().Unit
	default:
		if tc.typeKeys != nil {
			if ty := tc.typeKeys[s]; ty != types.NoTypeID {
				return ty
			}
		}
		return types.NoTypeID
	}
}

func splitTopLevel(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '<', '[', '(':
			depth++
		case '>', ']', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	return filtered
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
