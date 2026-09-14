package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// originalInstantiation reads a typed request in its source body's vocabulary.
// An unused original body still owns its root/edge; current instance reachability
// is a separate obligation. Function and tag readers validate their own callee
// declarations and signatures before using the returned detached argument list.
func (fn *returnOriginFunction) originalInstantiation(id ast.ExprID, kind InstantiationTemplateKind, callee symbols.SymbolID) ([]types.TypeID, string) {
	if fn == nil || fn.unit == nil || fn.item == nil || !fn.item.Body.IsValid() || fn.candidate == nil ||
		!callee.IsValid() || (kind != InstantiationFunction && kind != InstantiationTag) {
		return nil, "generic original call lacks its owning body or template kind"
	}
	u := fn.unit
	authority := u.authority
	if authority == nil || authority.InstantiationIdentity == nil || authority.TypeInterner == nil {
		return nil, "generic original call lacks canonical identity authority"
	}
	identity := *authority.InstantiationIdentity
	if identity.ResolveTemplate == nil || identity.ResolveSource == nil || identity.Types.Types != authority.TypeInterner {
		return nil, "generic original call lacks canonical identity resolvers"
	}
	node := u.Builder.Exprs.Get(id)
	call, called := u.Builder.Exprs.Call(id)
	result, typed := u.Sema.ExprTypes[id]
	if node == nil || node.Kind != ast.ExprCall || !called || call == nil || !typed || result == types.NoTypeID ||
		node.Span.File != fn.item.Span.File || node.Span.Start < fn.item.Span.Start || node.Span.End > fn.item.Span.End {
		return nil, "generic original call lacks its owning typed expression"
	}
	if _, present := authority.TypeInterner.Lookup(result); !present {
		return nil, "generic original call has an unknown result descriptor"
	}
	for other := range u.Sema.ExprTypes {
		if other != id {
			if expression := u.Builder.Exprs.Get(other); expression != nil && expression.Span == node.Span {
				return nil, "generic original call has ambiguous typed source expressions"
			}
		}
	}
	local := u.Symbols.ExprSymbols[id]
	selected := u.Symbols.Table.Symbols.Get(local)
	mapped := local == callee
	if len(u.Publication.RootToLocalSymbols) != 0 {
		matches := 0
		for canonical, locals := range u.Publication.RootToLocalSymbols {
			if slices.Contains(locals, local) {
				matches++
				mapped = canonical == callee
			}
		}
		mapped = mapped && matches == 1
	}
	if !local.IsValid() || selected == nil || !mapped ||
		(kind == InstantiationFunction && selected.Kind != symbols.SymbolFunction) ||
		(kind == InstantiationTag && selected.Kind != symbols.SymbolTag) {
		return nil, "generic original call disagrees with its selected canonical template"
	}
	caller := fn.candidate
	owner, err := u.callableIdentity(fn.symbol, fn.canonicalSourceKey)
	if err != nil || owner.BodyKey != fn.key || caller.BodyKey != fn.key || owner.SourceKey != fn.canonicalSourceKey ||
		caller.SourceKey != fn.canonicalSourceKey || caller.Source != fn.item.NameSpan || !caller.HasBody {
		return nil, "generic original call lacks its published caller declaration"
	}
	callerKey, callerErr := identity.ResolveTemplate(caller.Symbol)
	calleeKey, calleeErr := identity.ResolveTemplate(callee)
	sourceKey, sourceErr := identity.ResolveSource(node.Span.File)
	if callerErr != nil || calleeErr != nil || sourceErr != nil || callerKey == "" || calleeKey == "" || sourceKey != u.SourceKey {
		return nil, "generic original call disagrees with canonical source identities"
	}
	var root *InstantiationRoot
	var edge *InstantiationEdge
	for _, original := range authority.InstantiationGraph.Roots() {
		if original.Witness.Site == node.Span {
			if root != nil {
				return nil, "generic original call has ambiguous source requests"
			}
			copy := original
			root = &copy
		}
	}
	for _, original := range authority.InstantiationGraph.Edges() {
		if original.Witness.Site == node.Span {
			if edge != nil || root != nil {
				return nil, "generic original call has ambiguous source requests"
			}
			copy := original
			edge = &copy
		}
	}
	var args []types.TypeID
	var witness InstantiationWitness
	if len(caller.TemplateParams) == 0 {
		if root == nil || edge != nil || root.Kind != kind || root.Template != callee || root.Witness.Caller != caller.Symbol {
			return nil, "generic original call lacks its unique nongeneric-caller root"
		}
		args, witness = root.TemplateArgs, root.Witness
	} else {
		if edge == nil || root != nil || edge.Kind != kind || edge.Caller != caller.Symbol || edge.Callee != callee ||
			edge.Witness.Caller != caller.Symbol || int(edge.CallerTemplateArity) != len(caller.TemplateParams) || validateInstantiationBindings(edge) != nil {
			return nil, "generic original call lacks its unique template-caller edge"
		}
		for _, binding := range edge.CallerBindings {
			info, found := authority.TypeInterner.TypeParamInfo(binding.Param)
			var owner symbols.SymbolID
			if found && info != nil {
				owner, found = u.canonicalParameterOwner(symbols.SymbolID(info.Owner))
			}
			if !found || info == nil || owner != binding.Owner || info.Index != binding.ParamIndex ||
				caller.TemplateParams[binding.ArgIndex] != binding.Param {
				return nil, "generic original call disagrees with its exact caller parameter bindings"
			}
		}
		args, witness = edge.CalleeTemplateArgs, edge.Witness
	}
	canonical, err := canonicalInstantiationWitness(&witness, identity)
	if err != nil || witness.SourceKey != sourceKey || canonical.SourceKey != sourceKey || canonical.CallerKey != callerKey || len(args) == 0 {
		return nil, "generic original call has an inconsistent source witness"
	}
	keys := identity.Types
	// Only exact original TypeIDs bind generic arguments. An equal owner/name or
	// parameter index cannot repair a foreign descriptor or create a new type.
	keys.ResolveTypeParam = func(id types.TypeID, _ types.TypeParamInfo) (string, error) {
		slot := slices.Index(caller.TemplateParams, id)
		if slot < 0 {
			return "", fmt.Errorf("type parameter %d is outside the original caller", id)
		}
		return fmt.Sprintf("caller-arg/%d", slot), nil
	}
	if _, err := keys.TypeArgsKey(args); err != nil {
		return nil, "generic original call arguments lack exact original type authority"
	}
	return slices.Clone(args), ""
}
