package sema

import "surge/internal/types"

// IsCopyType reports whether values of the given type can be implicitly copied,
// matching the semantics used by the type checker (builtin Copy + @copy types).
func (r *Result) IsCopyType(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil || id == types.NoTypeID {
		return false
	}
	resolved := resolveAlias(r.TypeInterner, id)
	if r.TypeInterner.IsCopy(resolved) {
		return true
	}
	if r.CopyTypes != nil {
		if _, ok := r.CopyTypes[resolved]; ok {
			return true
		}
	}
	return false
}

func resolveAlias(in *types.Interner, id types.TypeID) types.TypeID {
	if in == nil {
		return id
	}
	seen := 0
	for id != types.NoTypeID && seen < 32 {
		tt, ok := in.Lookup(id)
		if !ok || tt.Kind != types.KindAlias {
			return id
		}
		target, ok := in.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
		seen++
	}
	return id
}
