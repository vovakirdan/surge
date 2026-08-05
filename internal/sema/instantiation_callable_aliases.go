package sema

import "surge/internal/symbols"

// CanonicalizeInstantiationCallableAliases extends a symbol remap so repeated
// allocation aliases of one canonical callable body share the destination's
// owning symbol before graphs and entrypoint requests are merged.
func CanonicalizeInstantiationCallableAliases(
	dst, src *Result,
	mapping map[symbols.SymbolID]symbols.SymbolID,
) map[symbols.SymbolID]symbols.SymbolID {
	if dst == nil || src == nil {
		return mapping
	}
	return canonicalCallableAliasRemap(dst.CallableCandidates, src.CallableCandidates, mapping)
}

func canonicalCallableAliasRemap(
	dst, src []CallableCandidate,
	mapping map[symbols.SymbolID]symbols.SymbolID,
) map[symbols.SymbolID]symbols.SymbolID {
	remap := make(map[symbols.SymbolID]symbols.SymbolID, len(mapping)+len(src))
	for local, root := range mapping {
		remap[local] = root
	}
	owners := make(map[string]CallableCandidate, len(dst)+len(src))
	for i := range dst {
		key := callableCandidateKey(&dst[i])
		if key != "" {
			if _, exists := owners[key]; !exists {
				owners[key] = dst[i]
			}
		}
	}
	for i := range src {
		candidate := cloneCallableCandidate(&src[i])
		original := candidate.Symbol
		candidate.Symbol = remapInstantiationSymbol(candidate.Symbol, remap)
		key := callableCandidateKey(&candidate)
		owner, exists := owners[key]
		if exists && callableCandidateRecordsEquivalent(&owner, &candidate) {
			remap[original] = owner.Symbol
			continue
		}
		if !exists && key != "" {
			owners[key] = candidate
		}
	}
	return remap
}
