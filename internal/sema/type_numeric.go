package sema

import "surge/internal/types"

type numericKind uint8

const (
	numericInvalid numericKind = iota
	numericSigned
	numericUnsigned
	numericFloat
)

type numericInfo struct {
	kind  numericKind
	width types.Width
}

func (tc *typeChecker) numericInfo(id types.TypeID) (numericInfo, bool) {
	if id == types.NoTypeID || tc.types == nil {
		return numericInfo{}, false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return numericInfo{}, false
	}
	switch tt.Kind {
	case types.KindInt:
		return numericInfo{kind: numericSigned, width: tt.Width}, true
	case types.KindUint:
		return numericInfo{kind: numericUnsigned, width: tt.Width}, true
	case types.KindFloat:
		return numericInfo{kind: numericFloat, width: tt.Width}, true
	default:
		return numericInfo{}, false
	}
}

func (tc *typeChecker) numericWidenable(from, to types.TypeID) bool {
	fromInfo, okFrom := tc.numericInfo(from)
	toInfo, okTo := tc.numericInfo(to)
	if !okFrom || !okTo || fromInfo.kind != toInfo.kind {
		return false
	}
	return widthCanWiden(fromInfo.width, toInfo.width)
}

func widthCanWiden(from, to types.Width) bool {
	if from == to {
		return true
	}
	// WidthAny represents arbitrary precision and is considered the widest.
	if to == types.WidthAny {
		return true
	}
	if from == types.WidthAny {
		return false
	}
	return from < to
}

func (tc *typeChecker) numericCastResult(source, target types.TypeID) types.TypeID {
	if source == types.NoTypeID || target == types.NoTypeID {
		return types.NoTypeID
	}
	if _, ok := tc.numericInfo(source); !ok {
		return types.NoTypeID
	}
	if _, ok := tc.numericInfo(target); !ok {
		return types.NoTypeID
	}
	return target
}
