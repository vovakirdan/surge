package llvm

import (
	"surge/internal/mir"
	"surge/internal/types"
)

type intMeta struct {
	bits   int
	signed bool
}

func intInfo(typesIn *types.Interner, id types.TypeID) (intMeta, bool) {
	if typesIn == nil {
		return intMeta{}, false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return intMeta{}, false
	}
	switch tt.Kind {
	case types.KindBool:
		return intMeta{bits: 1, signed: false}, true
	case types.KindInt:
		if tt.Width == types.WidthAny {
			return intMeta{}, false
		}
		return intMeta{bits: widthBits(tt.Width), signed: true}, true
	case types.KindUint:
		if tt.Width == types.WidthAny {
			return intMeta{}, false
		}
		return intMeta{bits: widthBits(tt.Width), signed: false}, true
	default:
		return intMeta{}, false
	}
}

func widthBits(width types.Width) int {
	if width == types.WidthAny {
		return 64
	}
	return int(width)
}

func isBigIntType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindInt && tt.Width == types.WidthAny
}

func isBigUintType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindUint && tt.Width == types.WidthAny
}

func isBigFloatType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindFloat && tt.Width == types.WidthAny
}

func resolveValueType(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	for i := 0; i < 32 && id != types.NoTypeID; i++ {
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
		case types.KindOwn, types.KindReference, types.KindPointer:
			id = tt.Elem
		default:
			return id
		}
	}
	return id
}

func isStringLike(typesIn *types.Interner, id types.TypeID) bool {
	id = resolveValueType(typesIn, id)
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindString
}

func isArrayLike(typesIn *types.Interner, id types.TypeID) bool {
	_, dynamic, ok := arrayElemType(typesIn, id)
	return ok && dynamic
}

func arrayFixedInfo(typesIn *types.Interner, id types.TypeID) (elem types.TypeID, length uint32, ok bool) {
	if typesIn == nil || id == types.NoTypeID {
		return types.NoTypeID, 0, false
	}
	id = resolveValueType(typesIn, id)
	if elem, length, ok := typesIn.ArrayFixedInfo(id); ok {
		return elem, length, true
	}
	if tt, ok := typesIn.Lookup(id); ok && tt.Kind == types.KindArray && tt.Count != types.ArrayDynamicLength {
		return tt.Elem, tt.Count, true
	}
	return types.NoTypeID, 0, false
}

func arrayElemType(typesIn *types.Interner, id types.TypeID) (types.TypeID, bool, bool) {
	if typesIn == nil || id == types.NoTypeID {
		return types.NoTypeID, false, false
	}
	id = resolveValueType(typesIn, id)
	if elem, ok := typesIn.ArrayInfo(id); ok {
		return elem, true, true
	}
	if elem, _, ok := typesIn.ArrayFixedInfo(id); ok {
		return elem, false, true
	}
	if tt, ok := typesIn.Lookup(id); ok && tt.Kind == types.KindArray {
		return tt.Elem, tt.Count == types.ArrayDynamicLength, true
	}
	return types.NoTypeID, false, false
}

func isBytesViewType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID || typesIn.Strings == nil {
		return false
	}
	id = resolveValueType(typesIn, id)
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil {
		return false
	}
	return typesIn.Strings.MustLookup(info.Name) == "BytesView"
}

func isRangeType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID || typesIn.Strings == nil {
		return false
	}
	id = resolveValueType(typesIn, id)
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil {
		return false
	}
	return typesIn.Strings.MustLookup(info.Name) == "Range"
}

func isRefType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindReference
}

func isHandleValueType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindStruct, types.KindTuple, types.KindUnion, types.KindEnum, types.KindString, types.KindArray, types.KindFn:
		return true
	case types.KindPointer, types.KindReference:
		return false
	default:
		return false
	}
}

func isNothingType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	id = resolveAliasAndOwn(typesIn, id)
	tt, ok := typesIn.Lookup(id)
	return ok && tt.Kind == types.KindNothing
}

func llvmLocalValueType(typesIn *types.Interner, local mir.Local) (string, error) {
	if local.Flags&(mir.LocalFlagRef|mir.LocalFlagRefMut|mir.LocalFlagPtr) != 0 {
		return "ptr", nil
	}
	return llvmValueType(typesIn, local.Type)
}
