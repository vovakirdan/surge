package sema

import (
	"fmt"

	"surge/internal/symbols"
	"surge/internal/types"
)

// FinalizationPublication describes one owning per-file semantic result. The
// symbol map translates merged/root symbols back into that file's vocabulary.
// An absent map means both results already share a symbol table.
type FinalizationPublication struct {
	SourceKey          string
	RootToLocalSymbols map[symbols.SymbolID][]symbols.SymbolID
}

// LocalSymbols returns deterministic local counterparts for a merged symbol.
// Multiple aliases are preserved so a feature can select by canonical source
// identity rather than depending on allocation order.
func (p FinalizationPublication) LocalSymbols(root symbols.SymbolID) []symbols.SymbolID {
	return append([]symbols.SymbolID(nil), p.RootToLocalSymbols[root]...)
}

// LocalSymbol translates only unambiguous winners. Symbols without exactly
// one local counterpart remain in the merged vocabulary.
func (p FinalizationPublication) LocalSymbol(root symbols.SymbolID) symbols.SymbolID {
	if locals := p.RootToLocalSymbols[root]; len(locals) == 1 {
		return locals[0]
	}
	return root
}

// PublishFinalizationDecisions projects post-merge decisions into the exact
// per-file Result that owns their source expressions. Add new typed decisions
// here rather than teaching the driver feature semantics.
func PublishFinalizationDecisions(dst, authority *Result, publication FinalizationPublication) error {
	if dst == nil || authority == nil {
		return nil
	}
	if publication.SourceKey == "" {
		return fmt.Errorf("missing canonical source identity")
	}
	dst.EntrypointCallableBindings = publishEntrypointCallableBindings(authority.EntrypointCallableBindings, publication)
	return nil
}

func publishEntrypointCallableBindings(
	bindings []EntrypointCallableBinding,
	publication FinalizationPublication,
) []EntrypointCallableBinding {
	out := make([]EntrypointCallableBinding, 0, 1)
	for i := range bindings {
		if bindings[i].SourceKey != publication.SourceKey {
			continue
		}
		binding := bindings[i]
		binding.Entrypoint = publication.LocalSymbol(binding.Entrypoint)
		binding.Callee = publication.LocalSymbol(binding.Callee)
		binding.TemplateArgs = append([]types.TypeID(nil), binding.TemplateArgs...)
		binding.ParamTypes = append([]types.TypeID(nil), binding.ParamTypes...)
		out = append(out, binding)
	}
	return out
}
