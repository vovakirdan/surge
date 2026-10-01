package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/source"
)

// indexUse answers the concrete use behind a generic `__index` or `__index_set`
// selected at an index node. A finalized use at the site is the authority, checked
// against its caller and expression exactly as the use checker checks it.
//
// A non-generic caller that the finalized closure never reached (an imported
// function no live body calls) has no finalized use at all: the closure is a
// reachability fixpoint, and checkGenericUses skips the roots of such a caller.
// Its index is then answered by the original request the checker recorded for
// that very site, as an ordinary generic call's is (originalInstantiation): the
// unique root whose caller is this body and whose template is the selected
// declaration, with concrete arguments. A live caller without a finalized use,
// a generic caller, or a site with no or several roots keeps the refusal.
func (a *returnOriginAnalyzer) indexUse(fn, caller *returnOriginFunction, id ast.ExprID, span source.Span) (use ConcreteInstantiationUse, reason string) {
	u := caller.unit
	authority := u.authority
	closure := authority.InstantiationClosure
	if closure == nil || authority.InstantiationIdentity == nil {
		return ConcreteInstantiationUse{}, "generic index lacks its finalized concrete use"
	}
	var found *ConcreteInstantiationUse
	for i := range closure.UseSites {
		site := &closure.UseSites[i]
		if site.SourceKey == u.SourceKey && site.Site == span {
			if found != nil {
				return ConcreteInstantiationUse{}, "generic index has ambiguous finalized concrete uses"
			}
			found = site
		}
	}
	if found != nil {
		callee, owner, expr, context := a.genericUseContext(*found)
		if context != "" {
			return ConcreteInstantiationUse{}, context
		}
		if callee != fn || owner != caller || expr != id {
			return ConcreteInstantiationUse{}, "generic index disagrees with its selected caller and expression"
		}
		return *found, ""
	}
	c := caller.candidate
	if c == nil || len(c.TemplateParams) != 0 || !c.Symbol.IsValid() || slices.Contains(closure.LiveCallables, c.Symbol) ||
		!caller.item.Body.IsValid() || span.File != caller.item.Span.File || span.Start < caller.item.Span.Start || span.End > caller.item.Span.End {
		return ConcreteInstantiationUse{}, "generic index lacks its finalized concrete use"
	}
	var original *ConcreteInstantiationUse
	roots := authority.InstantiationGraph.Roots()
	for i := range roots {
		root := &roots[i]
		if root.Witness.Site != span {
			continue
		}
		witness, err := canonicalInstantiationWitness(&root.Witness, *authority.InstantiationIdentity)
		if err != nil || original != nil || root.Kind != InstantiationFunction || root.Witness.Caller != c.Symbol ||
			witness.SourceKey != u.SourceKey || !u.templateNames(root.Template, fn.candidate.Symbol) ||
			len(root.TemplateArgs) != len(fn.candidate.TemplateParams) || !returnOriginConcreteArgs(authority.TypeInterner, root.TemplateArgs) {
			return ConcreteInstantiationUse{}, "generic index in an unreached caller lacks its unique original root"
		}
		original = &ConcreteInstantiationUse{CallerTemplate: root.Witness.Caller, CalleeTemplate: root.Template, Kind: root.Kind,
			TemplateArgs: slices.Clone(root.TemplateArgs), Site: span, SourceKey: witness.SourceKey}
	}
	if original == nil {
		return ConcreteInstantiationUse{}, "generic index lacks its finalized concrete use"
	}
	return *original, ""
}
