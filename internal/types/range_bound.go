package types

// A `Range<T>` is a runtime-owned handle naming a heap object that carries two
// BOUND WORDS, and those words are the whole of what a crossing has to reason
// about. This file answers the one question both sides of the crossing wall ask
// about them, from the type graph alone -- the way ContainsDynamicArray does,
// and for the same reason: sema asks it to decide whether to refuse, the LLVM
// backend asks it to decide whether it can emit the walk, and two near-copies
// of a rule about a runtime layout is how the two sides come to disagree about
// what crosses.

// RangeBoundType reports whether id is a `Range<T>` and yields T, the type of
// the two bound words the runtime object holds.
//
// It is not taken on the name alone: the family is marked from its declaration
// in core, exactly as IsRuntimeHandleType records it, and the name then tells
// Range from its two siblings Task and Channel.
func (in *Interner) RangeBoundType(id TypeID) (TypeID, bool) {
	if in == nil || id == NoTypeID || in.Strings == nil {
		return NoTypeID, false
	}
	resolved := resolveAliasAndOwn(in, id)
	if !in.IsRuntimeHandleType(resolved) {
		return NoTypeID, false
	}
	info, ok := in.StructInfo(resolved)
	if !ok || info == nil || len(info.TypeArgs) != 1 {
		return NoTypeID, false
	}
	if in.Strings.MustLookup(info.Name) != "Range" {
		return NoTypeID, false
	}
	return info.TypeArgs[0], true
}

// RangeBoundsAreArbitraryPrecision reports whether id is a `Range<T>` whose two
// bound words hold one of the three arbitrary-precision scalars -- `int`,
// `uint`, `float`.
//
// Those three are exactly the set the runtime object's bound byte can name, and
// exactly the set whose heap halves export retain, release and unshare. So they
// are exactly the ranges rt_range_free can give back and rt_range_unshare can
// make private, which is what a crossing gate needs to know.
//
// It is NOT the question "does this hold a counted block": `int` and `uint` are
// on this list and are not counted scalars today, and a range whose bound is
// neither -- a shape no constructor builds, but one the type graph can spell --
// is off it. Widening what counts as counted is a different lane's business and
// nothing here touches it; this predicate only says which bound words the
// runtime has a lifecycle for.
func (in *Interner) RangeBoundsAreArbitraryPrecision(id TypeID) bool {
	bound, ok := in.RangeBoundType(id)
	if !ok {
		return false
	}
	tt, found := in.Lookup(resolveAliasAndOwn(in, bound))
	if !found || tt.Width != WidthAny {
		return false
	}
	switch tt.Kind {
	case KindInt, KindUint, KindFloat:
		return true
	default:
		return false
	}
}
