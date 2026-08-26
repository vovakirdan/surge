package sema

import (
	"fmt"
	"sort"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// canonicalizeFinalizationRequestSources stamps a portable source identity on
// every request that is answered after the module graphs merge.
func canonicalizeFinalizationRequestSources(result *Result, resolve func(source.FileID) (string, error)) error {
	if err := canonicalizeEntrypointCallableSources(result, resolve); err != nil {
		return err
	}
	if err := canonicalizeDirectCloneSources(result, resolve); err != nil {
		return err
	}
	return canonicalizeCloneObligationSources(result, resolve)
}

// mergeFinalizationRequests carries one file's unanswered requests into the
// merged authority that will answer them.
func mergeFinalizationRequests(dst, src *Result, mapping map[symbols.SymbolID]symbols.SymbolID) {
	mergeEntrypointCallableRequests(dst, src, mapping)
	mergeDirectCloneRequests(dst, src, mapping)
	mergeCloneObligations(dst, src, mapping)
}

// copyFinalizationRequests shares one finalized authority's requests and
// answers with another per-file result.
func copyFinalizationRequests(dst, src *Result) {
	dst.EntrypointCallableRequests = cloneEntrypointCallableRequests(src.EntrypointCallableRequests)
	dst.EntrypointCallableBindings = cloneEntrypointCallableBindings(src.EntrypointCallableBindings)
	dst.DirectCloneRequests = cloneDirectCloneRequests(src.DirectCloneRequests)
	dst.DirectCloneBindings = cloneDirectCloneBindings(src.DirectCloneBindings)
	dst.CloneObligations = cloneCloneObligations(src.CloneObligations)
}

// FinalizationPublication describes one owning per-file semantic result. The
// symbol map translates merged/root symbols back into that file's vocabulary.
// An absent map means both results already share a symbol table.
type FinalizationPublication struct {
	SourceKey          string
	RootToLocalSymbols map[symbols.SymbolID][]symbols.SymbolID
	LocalCallables     []FinalizationCallableIdentity
}

// FinalizationCallableIdentity preserves allocation-independent callable
// identity in the symbol vocabulary that receives final decisions.
type FinalizationCallableIdentity struct {
	Symbol    symbols.SymbolID
	BodyKey   string
	SourceKey string
}

// LocalSymbols returns deterministic local counterparts for a merged symbol.
func (p FinalizationPublication) LocalSymbols(root symbols.SymbolID) []symbols.SymbolID {
	return append([]symbols.SymbolID(nil), p.RootToLocalSymbols[root]...)
}

// PublishFinalizationDecisions projects post-merge decisions into the exact
// per-file Result that owns their source expressions. The destination changes
// only after every symbol in this publication has localized successfully.
func PublishFinalizationDecisions(dst, authority *Result, publication FinalizationPublication) error {
	if dst == nil || authority == nil {
		return nil
	}
	if publication.SourceKey == "" {
		return fmt.Errorf("missing canonical source identity")
	}
	bindings, err := publishEntrypointCallableBindings(authority, publication)
	if err != nil {
		return err
	}
	cloneSymbols, err := publishDirectCloneBindings(authority, publication)
	if err != nil {
		return err
	}
	dst.EntrypointCallableBindings = bindings
	if len(cloneSymbols) > 0 && dst.CloneSymbols == nil {
		dst.CloneSymbols = make(map[ast.ExprID]symbols.SymbolID, len(cloneSymbols))
	}
	for use, callee := range cloneSymbols {
		dst.CloneSymbols[use] = callee
	}
	return nil
}

func publishEntrypointCallableBindings(
	authority *Result,
	publication FinalizationPublication,
) ([]EntrypointCallableBinding, error) {
	out := make([]EntrypointCallableBinding, 0, 1)
	for i := range authority.EntrypointCallableBindings {
		if authority.EntrypointCallableBindings[i].SourceKey != publication.SourceKey {
			continue
		}
		binding := authority.EntrypointCallableBindings[i]
		if len(publication.RootToLocalSymbols) > 0 {
			entrypointKey, err := authorityCallableBodyKey(
				authority.CallableCandidates,
				binding.Entrypoint,
				binding.SourceKey,
			)
			if err != nil {
				return nil, err
			}
			binding.Entrypoint, err = publication.localCallable(binding.Entrypoint, entrypointKey)
			if err != nil {
				return nil, err
			}
			binding.Callee, err = publication.localCallable(binding.Callee, binding.CalleeKey)
			if err != nil {
				return nil, err
			}
		}
		binding.TemplateArgs = append([]types.TypeID(nil), binding.TemplateArgs...)
		binding.ParamTypes = append([]types.TypeID(nil), binding.ParamTypes...)
		out = append(out, binding)
	}
	return out, nil
}

func authorityCallableBodyKey(
	candidates []CallableCandidate,
	symbol symbols.SymbolID,
	sourceKey string,
) (string, error) {
	keys := make(map[string]struct{})
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Symbol == symbol && candidate.SourceKey == sourceKey && candidate.BodyKey != "" {
			keys[candidate.BodyKey] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	if len(ordered) == 1 {
		return ordered[0], nil
	}
	if len(ordered) == 0 {
		return "", fmt.Errorf("no authority callable for entrypoint %d in %s", symbol, sourceKey)
	}
	return "", fmt.Errorf("ambiguous authority callable for entrypoint %d in %s: %v", symbol, sourceKey, ordered)
}

func (p FinalizationPublication) localCallable(
	root symbols.SymbolID,
	bodyKey string,
) (symbols.SymbolID, error) {
	aliases, ok := p.RootToLocalSymbols[root]
	if !ok || len(aliases) == 0 {
		return 0, fmt.Errorf("no local aliases for root callable %d body %q", root, bodyKey)
	}
	aliasSet := make(map[symbols.SymbolID]struct{}, len(aliases))
	for _, alias := range aliases {
		aliasSet[alias] = struct{}{}
	}
	matches := make(map[symbols.SymbolID]struct{})
	for i := range p.LocalCallables {
		candidate := &p.LocalCallables[i]
		if candidate.BodyKey != bodyKey {
			continue
		}
		if _, alias := aliasSet[candidate.Symbol]; alias {
			matches[candidate.Symbol] = struct{}{}
		}
	}
	ordered := make([]symbols.SymbolID, 0, len(matches))
	for symbol := range matches {
		ordered = append(ordered, symbol)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	if len(ordered) == 1 {
		return ordered[0], nil
	}
	if len(ordered) == 0 {
		return 0, fmt.Errorf("no local callable for root %d body %q", root, bodyKey)
	}
	return 0, fmt.Errorf("ambiguous local callable for root %d body %q: %v", root, bodyKey, ordered)
}
