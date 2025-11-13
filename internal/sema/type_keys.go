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
	if aliasBase := tc.aliasBaseType(id); aliasBase != types.NoTypeID {
		baseKey := tc.typeKeyForType(aliasBase)
		if baseKey != "" {
			candidates = append(candidates, typeKeyCandidate{
				key:   baseKey,
				alias: id,
				base:  aliasBase,
			})
		}
	}
	return candidates
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
	if left.alias == types.NoTypeID && right.alias == types.NoTypeID {
		return true
	}
	if left.alias != types.NoTypeID && right.alias != types.NoTypeID {
		return left.alias == right.alias
	}
	return false
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

func (tc *typeChecker) adjustAliasBinaryResult(res types.TypeID, left, right typeKeyCandidate) types.TypeID {
	if res == types.NoTypeID {
		return res
	}
	if left.alias != types.NoTypeID && right.alias != types.NoTypeID && left.alias == right.alias && left.base == res {
		return left.alias
	}
	return res
}
