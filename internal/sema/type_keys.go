package sema

import (
	"surge/internal/symbols"
	"surge/internal/types"
)

type typeKeyCandidate struct {
	key   symbols.TypeKey
	alias types.TypeID
	base  types.TypeID
}

func (tc *typeChecker) typeKeyCandidates(id types.TypeID) []typeKeyCandidate {
	key := tc.typeKeyForType(id)
	candidates := []typeKeyCandidate{{key: key, base: id}}
	candidates = tc.appendFamilyFallback(candidates, id, key, types.NoTypeID)
	if aliasBase := tc.aliasBaseType(id); aliasBase != types.NoTypeID {
		baseKey := tc.typeKeyForType(aliasBase)
		if baseKey != "" {
			cand := typeKeyCandidate{
				key:   baseKey,
				alias: id,
				base:  aliasBase,
			}
			candidates = append(candidates, cand)
			candidates = tc.appendFamilyFallback(candidates, aliasBase, baseKey, id)
		}
	}
	return candidates
}

func (tc *typeChecker) appendFamilyFallback(c []typeKeyCandidate, base types.TypeID, key symbols.TypeKey, alias types.TypeID) []typeKeyCandidate {
	fallback := tc.familyKeyForType(base)
	if fallback == "" || fallback == key {
		return c
	}
	for _, cand := range c {
		if cand.key == fallback && cand.alias == alias && cand.base == base {
			return c
		}
	}
	return append(c, typeKeyCandidate{
		key:   fallback,
		alias: alias,
		base:  base,
	})
}

func (tc *typeChecker) aliasBaseType(id types.TypeID) types.TypeID {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID
	}
	current := id
	for {
		tt, ok := tc.types.Lookup(current)
		if !ok || tt.Kind != types.KindAlias {
			if current != id {
				return current
			}
			return types.NoTypeID
		}
		target, ok := tc.types.AliasTarget(current)
		if !ok || target == types.NoTypeID || target == current {
			if current != id {
				return current
			}
			return types.NoTypeID
		}
		current = target
	}
}

func compatibleAliasFallback(left, right typeKeyCandidate) bool {
	switch {
	case left.alias == types.NoTypeID && right.alias == types.NoTypeID:
		return true
	case left.alias != types.NoTypeID && right.alias != types.NoTypeID:
		return left.alias == right.alias
	case left.alias != types.NoTypeID:
		return left.base != right.base
	case right.alias != types.NoTypeID:
		return right.base != left.base
	default:
		return false
	}
}

func (tc *typeChecker) adjustAliasUnaryResult(res types.TypeID, cand typeKeyCandidate) types.TypeID {
	if res == types.NoTypeID {
		return res
	}
	if cand.alias != types.NoTypeID && cand.base == res {
		return cand.alias
	}
	return res
}

func (tc *typeChecker) familyKeyForType(id types.TypeID) symbols.TypeKey {
	if id == types.NoTypeID || tc.types == nil {
		return ""
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return ""
	}
	switch tt.Kind {
	case types.KindInt:
		return symbols.TypeKey("int")
	case types.KindUint:
		return symbols.TypeKey("uint")
	case types.KindFloat:
		return symbols.TypeKey("float")
	default:
		return ""
	}
}

func (tc *typeChecker) adjustAliasBinaryResult(res types.TypeID, left, right typeKeyCandidate) types.TypeID {
	if res == types.NoTypeID {
		return res
	}
	if left.alias != types.NoTypeID && right.alias != types.NoTypeID && left.alias == right.alias && left.base == res {
		return left.alias
	}
	return res
}
