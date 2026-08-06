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
	if isPlacementType(typesIn, id) {
		return "i64", nil
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return "void", fmt.Errorf("unknown type id %d", id)
	}
	// Answered before the kind switch, because the switch groups enums with the
	// composites and an enum is not one.
	if tt.Kind == types.KindEnum {
		return enumBaseCarrier(typesIn, id)
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
	case types.KindString, types.KindPointer, types.KindReference, types.KindFar, types.KindFn:
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

// enumBaseCarrier spells an enum as the type its constants already are.
//
// An enum declares a set of named constants of one base type. It has no members
// and owns nothing, so there is no storage of its own to lay out — which is
// exactly why `types.IsValueComposite` excludes it and why `mir.CallLayout`
// puts it on the direct path rather than passing it through memory. Every other
// view agrees: `internal/layout` computes an enum's layout by descending to its
// base type, the layout root collector registers that base type INSTEAD of the
// enum, and the constants themselves are lowered to base-typed literals long
// before emission. Spelling every enum as a pointer was the one view that
// disagreed — it made a `uint8`-based enum eight bytes wide instead of one — and
// nothing in the language could show it, because nothing in the language
// produces an enum-typed value.
//
// Sema defaults an unnamed base to `int`, so the no-base case below is not a
// shape a source reaches; it matches the layout engine's own fallback for it.
func enumBaseCarrier(typesIn *types.Interner, id types.TypeID) (string, error) {
	info, ok := typesIn.EnumInfo(id)
	if !ok || info == nil || info.BaseType == types.NoTypeID {
		return "i32", nil
	}
	base := resolveAliasAndOwn(typesIn, info.BaseType)
	baseType, ok := typesIn.Lookup(base)
	if !ok {
		return "void", fmt.Errorf("enum %s has an unknown base type", types.Label(typesIn, id))
	}
	// The base is spelled here rather than by recursing through llvmType so
	// that a base an enum may not legally have — another enum, a composite —
	// is refused instead of being carried as whatever that type is carried as.
	switch baseType.Kind {
	case types.KindInt, types.KindUint:
		return intWidthType(baseType.Width), nil
	case types.KindString:
		return "ptr", nil
	default:
		return "void", fmt.Errorf(
			"enum %s has base type %s, which is not a type constants can be carried as",
			types.Label(typesIn, id), baseType.Kind.String())
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
