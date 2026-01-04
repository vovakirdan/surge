package llvm

import (
	"fmt"

	"surge/internal/types"
)

func resolveAliasAndOwn(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	seen := make(map[types.TypeID]struct{}, 8)
	for id != types.NoTypeID {
		if _, ok := seen[id]; ok {
			return id
		}
		seen[id] = struct{}{}
		tt, ok := typesIn.Lookup(id)
		if !ok {
			return id
		}
		switch tt.Kind {
		case types.KindAlias:
			target, ok := typesIn.AliasTarget(id)
			if !ok || target == types.NoTypeID {
				return id
			}
			id = target
		case types.KindOwn:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func llvmType(typesIn *types.Interner, id types.TypeID) (string, error) {
	if id == types.NoTypeID {
		return "void", nil
	}
	if typesIn == nil {
		return "void", fmt.Errorf("missing type interner")
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return "void", fmt.Errorf("unknown type id %d", id)
	}
	switch tt.Kind {
	case types.KindUnit, types.KindNothing:
		return "void", nil
	case types.KindBool:
		return "i1", nil
	case types.KindInt:
		return intWidthType(tt.Width), nil
	case types.KindUint:
		return intWidthType(tt.Width), nil
	case types.KindFloat:
		return floatWidthType(tt.Width), nil
	case types.KindString, types.KindPointer, types.KindReference, types.KindFn:
		return "ptr", nil
	case types.KindStruct:
		if _, ok := typesIn.ArrayInfo(id); ok {
			return "ptr", nil
		}
		if _, _, ok := typesIn.MapInfo(id); ok {
			return "ptr", nil
		}
		return "ptr", nil
	case types.KindTuple, types.KindUnion, types.KindEnum:
		return "ptr", nil
	case types.KindArray:
		return "ptr", nil
	case types.KindConst, types.KindGenericParam:
		return "void", nil
	default:
		return "void", fmt.Errorf("unsupported type kind %s", tt.Kind.String())
	}
}

func llvmValueType(typesIn *types.Interner, id types.TypeID) (string, error) {
	// Void types cannot be stored; fall back to i8 when needed.
	ty, err := llvmType(typesIn, id)
	if err != nil {
		return "", err
	}
	if ty == "void" {
		return "i8", nil
	}
	return ty, nil
}

func intWidthType(width types.Width) string {
	if width == types.WidthAny {
		return "ptr"
	}
	switch width {
	case types.Width8:
		return "i8"
	case types.Width16:
		return "i16"
	case types.Width32:
		return "i32"
	case types.Width64:
		return "i64"
	default:
		return "i64"
	}
}

func floatWidthType(width types.Width) string {
	if width == types.WidthAny {
		return "ptr"
	}
	switch width {
	case types.Width16:
		return "half"
	case types.Width32:
		return "float"
	case types.Width64:
		return "double"
	default:
		return "double"
	}
}
