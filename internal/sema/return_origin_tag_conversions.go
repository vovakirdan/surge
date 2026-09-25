package sema

import (
	"slices"

	"surge/internal/symbols"
	"surge/internal/types"
)

// canonicalTagSymbol maps a unit-local tag symbol to the root symbol of its
// declaration through the publication's root-to-local table: declaration
// identity, never a name.
func canonicalTagSymbol(u *returnOriginUnitIndex, selected symbols.SymbolID) (symbols.SymbolID, string) {
	canonical := selected
	if len(u.Publication.RootToLocalSymbols) != 0 {
		canonical = symbols.NoSymbolID
		for root, locals := range u.Publication.RootToLocalSymbols {
			if slices.Contains(locals, selected) {
				if canonical.IsValid() || !root.IsValid() {
					return 0, "tag constructor has ambiguous canonical declaration aliases"
				}
				canonical = root
			}
		}
	}
	if !canonical.IsValid() {
		return 0, "tag constructor lacks its canonical declaration alias"
	}
	return canonical, ""
}

// checkTagConversionUse certifies the finalized use of an implicit Some/Success
// wrap (N-TAGCONV). found reports whether the checker recorded such a wrap at the
// use's site. Only the INSTANCE is certified: the constructor is the exact one the
// checker selected (ImplicitConversion.Callee, mapped to its declaration), and its
// one type argument is the wrapped value's type. The value transfer stays expr's:
// the wrapper holds the payload itself, with every root and loan it carries.
func (a *returnOriginAnalyzer) checkTagConversionUse(caller *returnOriginFunction, use ConcreteInstantiationUse) (string, bool) {
	u := caller.unit
	var conversion ImplicitConversion
	matches := 0
	for _, candidate := range u.Sema.ImplicitConversions {
		if candidate.Span == use.Site && (candidate.Kind == ImplicitConversionSome || candidate.Kind == ImplicitConversionSuccess) {
			conversion, matches = candidate, matches+1
		}
	}
	switch {
	case matches == 0:
		return "", false
	case matches > 1:
		return "generic tag conversion has ambiguous implicit conversion records", true
	}
	sym := u.Symbols.Table.Symbols.Get(conversion.Callee)
	if sym == nil || sym.Kind != symbols.SymbolTag {
		return "generic tag conversion lacks its selected tag constructor", true
	}
	canonical, reason := canonicalTagSymbol(u, conversion.Callee)
	if reason != "" {
		return reason, true
	}
	if canonical != use.CalleeTemplate {
		return "generic tag conversion disagrees with its selected source declaration", true
	}
	if use.Caller != (InstanceKey{}) || len(caller.candidate.TemplateParams) != 0 || len(use.CallerTemplateArgs) != 0 {
		return "generic tag conversion in a generic caller needs its exact type-dependent payload transfer", true
	}
	if !slices.Contains(u.authority.InstantiationClosure.LiveCallables, use.CallerTemplate) {
		return "generic tag use disagrees with its original bound arguments", true
	}
	// The instance is Tag<Source>, and the target union has that member with that payload.
	in := u.Sema.TypeInterner
	info, present := in.UnionInfo(returnOriginResolveAlias(in, conversion.Target))
	members := 0
	if present && info != nil {
		for _, member := range info.Members {
			if member.Kind == types.UnionMemberTag && member.TagName == sym.Name {
				members++
				if !slices.Equal(member.TagArgs, []types.TypeID{conversion.Source}) {
					members = -1
					break
				}
			}
		}
	}
	if members != 1 || !slices.Equal(use.TemplateArgs, []types.TypeID{conversion.Source}) {
		return "generic tag conversion disagrees with its constructor instance", true
	}
	return "", true
}
