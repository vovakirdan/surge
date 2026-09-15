package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (b *returnOriginBody) tagCall(id ast.ExprID, env returnOriginEnv, targets returnOriginTargets) (returnOriginExprResult, error) {
	u := b.function.unit
	call, _ := u.Builder.Exprs.Call(id)
	if call == nil {
		return returnOriginExprResult{}, fmt.Errorf("return origins: missing tag call %d in %s", id, u.SourceKey)
	}
	if u.Builder.Exprs.Get(call.Target) == nil || u.Sema.ExprTypes[call.Target] == types.NoTypeID {
		return returnOriginExprResult{}, fmt.Errorf("return origins: expression %d is not typed in %s", call.Target, u.SourceKey)
	}
	_, _, reason := b.analyzer.tagPayload(b.function, id)
	children := make([]ast.ExprID, 0, len(call.Args)+1)
	if reason != "" {
		// Only a proved declaration target can cease being a value read.
		children = append(children, call.Target)
	}
	for _, arg := range call.Args {
		children = append(children, arg.Value)
	}
	return b.constructorChildren(id, children, env, targets, reason)
}

// The source reader has no current-use prerequisite. A root or edge supplies
// original tag arguments; the finalized-use reader checks current instances.
func (a *returnOriginAnalyzer) tagPayload(fn *returnOriginFunction, id ast.ExprID) (symbols.SymbolID, []types.TypeID, string) {
	u := fn.unit
	call, ok := u.Builder.Exprs.Call(id)
	if !ok || call == nil {
		return 0, nil, "tag constructor lacks its original call"
	}
	selected := u.Symbols.ExprSymbols[id]
	sym := u.Symbols.Table.Symbols.Get(selected)
	target := u.Builder.Exprs.Get(call.Target)
	if sym == nil || sym.Kind != symbols.SymbolTag || target == nil || target.Kind != ast.ExprIdent ||
		u.Symbols.ExprSymbols[call.Target] != selected || u.Sema.ExprTypes[call.Target] == types.NoTypeID {
		return 0, nil, "tag constructor lacks its original declaration target"
	}
	canonical := selected
	if len(u.Publication.RootToLocalSymbols) != 0 {
		canonical = symbols.NoSymbolID
		for root, locals := range u.Publication.RootToLocalSymbols {
			if slices.Contains(locals, selected) {
				if canonical.IsValid() || !root.IsValid() {
					return 0, nil, "tag constructor has ambiguous canonical declaration aliases"
				}
				canonical = root
			}
		}
	}
	if !canonical.IsValid() {
		return 0, nil, "tag constructor lacks its canonical declaration alias"
	}
	var owner *returnOriginUnitIndex
	var original *symbols.Symbol
	var tag *ast.TagItem
	// An imported copy of a tag carries no local declaration record, so the
	// declaration is found in each owner's own vocabulary, over every unit.
	for _, unit := range a.units {
		locals := unit.Publication.RootToLocalSymbols[canonical]
		if len(unit.Publication.RootToLocalSymbols) == 0 {
			locals = nil
			if unit == u {
				locals = []symbols.SymbolID{canonical}
			}
		}
		file := unit.Builder.Files.Get(unit.FileID)
		for _, local := range locals {
			candidate := unit.Symbols.Table.Symbols.Get(local)
			if file == nil || candidate == nil || candidate.Kind != symbols.SymbolTag ||
				candidate.Decl.ASTFile != unit.FileID || candidate.Decl.SourceFile != file.Span.File ||
				!slices.Contains(unit.Symbols.ItemSymbols[candidate.Decl.Item], local) {
				continue
			}
			item, found := unit.Builder.Items.Tag(candidate.Decl.Item)
			if !found || item == nil || item.Name != candidate.Name || item.NameSpan != candidate.Span ||
				unit.Symbols.Table.Scopes.Get(candidate.Scope) == nil {
				continue
			}
			if owner != nil {
				return 0, nil, "tag constructor has multiple original owners"
			}
			owner, original, tag = unit, candidate, item
		}
	}
	if owner == nil || original.Name != sym.Name || original.Signature == nil || sym.Signature == nil ||
		canonicalSignatureIdentity(original.Signature) != canonicalSignatureIdentity(sym.Signature) {
		return 0, nil, "tag constructor lacks its exact owning source declaration"
	}
	sig := original.Signature
	if sig.HasSelf || call.HasNamedArgs() || len(sig.Params) != len(tag.Payload) || len(call.Args) != len(tag.Payload) ||
		slices.Contains(sig.Variadic, true) || slices.Contains(sig.Defaults, true) || len(sig.Variadic) != len(sig.Params) || len(sig.Defaults) != len(sig.Params) {
		return 0, nil, "tag constructor needs exact positional payload slots"
	}
	in := u.Sema.TypeInterner
	info, found := in.UnionInfo(u.Sema.ExprTypes[id])
	if !found || info == nil || len(info.Members) != 1 || info.Members[0].Kind != types.UnionMemberTag ||
		info.Members[0].TagName != original.Name || len(info.Members[0].TagArgs) != len(tag.Payload) {
		return 0, nil, "tag constructor lacks its original typed payload result"
	}
	payload := info.Members[0].TagArgs
	for i, sourceType := range tag.Payload {
		if typeKeyForTypeExpr(owner.Builder, sourceType) != sig.Params[i] || payload[i] == types.NoTypeID || u.Sema.ExprTypes[call.Args[i].Value] != payload[i] {
			return 0, nil, "tag constructor disagrees with its source payload slots"
		}
		if _, present := in.Lookup(payload[i]); !present {
			return 0, nil, "tag constructor has an absent payload descriptor"
		}
		if _, implicit := u.Sema.ImplicitConversions[call.Args[i].Value]; implicit {
			return 0, nil, "tag payload conversion needs its resolved origin transfer"
		}
	}
	if !slices.Equal(original.TypeParams, tag.Generics) || !slices.Equal(sym.TypeParams, tag.Generics) {
		return 0, nil, "tag constructor disagrees with its original generic declaration"
	}
	if len(tag.Generics) == 0 {
		declared, present := in.UnionInfo(original.Type)
		if !present || declared == nil || len(call.TypeArgs) != 0 {
			return 0, nil, "tag constructor lacks its typed declaration payload"
		}
		matches := 0
		for _, member := range declared.Members {
			if member.Kind == types.UnionMemberTag && member.TagName == original.Name {
				if !slices.Equal(member.TagArgs, payload) {
					return 0, nil, "tag constructor disagrees with its typed declaration payload"
				}
				matches++
			}
		}
		for _, typ := range payload {
			if types.ContainsGenericParam(in, typ) {
				return 0, nil, "tag declaration payload still requires generic substitution"
			}
		}
		if matches != 1 {
			return 0, nil, "tag constructor lacks a unique typed declaration payload"
		}
		return canonical, nil, ""
	}
	params := owner.Builder.Items.GetTypeParamIDs(tag.TypeParamsStart, tag.TypeParamsCount)
	if len(params) != len(tag.Generics) || len(original.TypeParamSymbols) != len(params) {
		return 0, nil, "tag constructor lacks its original type parameter declarations"
	}
	for i, id := range params {
		param := owner.Builder.Items.TypeParam(id)
		if param == nil || param.IsConst || original.TypeParamSymbols[i].IsConst || param.Name != tag.Generics[i] ||
			original.TypeParamSymbols[i].Name != param.Name || original.TypeParamSymbols[i].Span != param.NameSpan || slices.Index(tag.Generics, param.Name) != i {
			return 0, nil, "tag constructor disagrees with its owning generic slots"
		}
	}
	args, reason := fn.originalInstantiation(id, InstantiationTag, canonical)
	if reason != "" {
		return 0, nil, reason
	}
	if len(args) != len(params) {
		return 0, nil, "tag constructor lacks its original generic arguments"
	}
	for i, sourceType := range tag.Payload {
		path, present := owner.Builder.Types.Path(sourceType)
		if !present || path == nil || len(path.Segments) != 1 || len(path.Segments[0].Generics) != 0 {
			return 0, nil, "tag payload needs its original nested type substitution"
		}
		slot := slices.Index(tag.Generics, path.Segments[0].Name)
		if slot < 0 || args[slot] != payload[i] {
			return 0, nil, "tag constructor disagrees with its original bound payload"
		}
	}
	return canonical, args, ""
}

// Retained calls after an abrupt exit are checked here even when body flow
// never visits them. The source proof cannot substitute for current authority.
func (a *returnOriginAnalyzer) checkTagUse(use ConcreteInstantiationUse) string {
	if reason := a.genericInstanceKind(use.Callee, use.CalleeTemplate, use.TemplateArgs, InstantiationTag); reason != "" {
		return reason
	}
	caller := a.functionForTemplate(use.CallerTemplate)
	if caller == nil || caller.unit.SourceKey != use.SourceKey || use.Site.File != caller.item.Span.File ||
		use.Site.Start < caller.item.Span.Start || use.Site.End > caller.item.Span.End {
		return "generic tag use disagrees with its owning caller"
	}
	var id ast.ExprID
	for expr, typ := range caller.unit.Sema.ExprTypes {
		if node := caller.unit.Builder.Exprs.Get(expr); node != nil && node.Span == use.Site {
			if id.IsValid() || node.Kind != ast.ExprCall || typ == types.NoTypeID {
				return "generic tag use disagrees with its original typed call"
			}
			id = expr
		}
	}
	if !id.IsValid() {
		return "generic tag use lacks its original typed call"
	}
	tag, args, reason := a.tagPayload(caller, id)
	if reason != "" {
		return reason
	}
	if tag != use.CalleeTemplate {
		return "generic tag use disagrees with its selected source declaration"
	}
	if use.Caller != (InstanceKey{}) {
		if reason := a.genericInstance(use.Caller, use.CallerTemplate, use.CallerTemplateArgs); reason != "" {
			return "generic tag caller: " + reason
		}
		return "generic tag caller requires its exact type-dependent payload transfer"
	}
	if len(caller.candidate.TemplateParams) != 0 || len(use.CallerTemplateArgs) != 0 ||
		!slices.Contains(caller.unit.authority.InstantiationClosure.LiveCallables, use.CallerTemplate) || !slices.Equal(args, use.TemplateArgs) {
		return "generic tag use disagrees with its original bound arguments"
	}
	return ""
}
