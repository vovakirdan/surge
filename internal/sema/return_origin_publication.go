package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The publication was captured before merging changed symbol vocabularies.
// A local numeric ID or a matching source name alone is not a body identity.
func (u *returnOriginUnitIndex) callableIdentity(id symbols.SymbolID, owner string) (FinalizationCallableIdentity, error) {
	var found FinalizationCallableIdentity
	for _, identity := range u.Publication.LocalCallables {
		if identity.Symbol != id || (owner != "" && identity.SourceKey != owner) {
			continue
		}
		if identity.BodyKey == "" || identity.SourceKey == "" {
			return found, fmt.Errorf("return origins: incomplete local callable identity in %s", u.SourceKey)
		}
		if found.BodyKey != "" && (found.BodyKey != identity.BodyKey || found.SourceKey != identity.SourceKey) {
			return found, fmt.Errorf("return origins: ambiguous local callable %d in %s", id, u.SourceKey)
		}
		found = identity
	}
	if found.BodyKey == "" {
		return found, fmt.Errorf("return origins: no local callable identity for %d in %s", id, u.SourceKey)
	}
	for _, candidate := range u.authority.CallableCandidates {
		if candidate.BodyKey != found.BodyKey || candidate.SourceKey != found.SourceKey {
			continue
		}
		if len(u.Publication.RootToLocalSymbols) == 0 {
			if candidate.Symbol == id {
				return found, nil
			}
		} else if slices.Contains(u.Publication.RootToLocalSymbols[candidate.Symbol], id) {
			return found, nil
		}
	}
	return found, fmt.Errorf("return origins: local callable %d has no matching published authority in %s", id, u.SourceKey)
}

func (u *returnOriginUnitIndex) addExternFunctions(block *ast.ExternBlock) error {
	for offset := range block.MembersCount {
		id := ast.ExternMemberID(uint32(block.MembersStart) + offset)
		member := u.Builder.Items.ExternMember(id)
		if member == nil {
			return fmt.Errorf("return origins: missing extern member in %s", u.SourceKey)
		}
		if member.Kind != ast.ExternMemberFn {
			continue
		}
		fn := u.Builder.Items.FnByPayload(member.Fn)
		if fn == nil {
			return fmt.Errorf("return origins: missing extern function in %s", u.SourceKey)
		}
		if err := u.addFunction(fn, u.Symbols.ExternSyms[id], u.externScopes[id]); err != nil {
			return err
		}
	}
	return nil
}

// A deferred method target is selector syntax, not a separately evaluated
// function value. The typed call record proves that distinction; its actual
// receiver and arguments still go through the ordinary expression transfer.
func (u *returnOriginUnitIndex) deferredMethod(id ast.ExprID, call *ast.ExprCallData) (*DeferredCallableEdge, error) {
	use := u.Sema.DeferredCallableUses[DeferredUseRef{Expr: id, Kind: DeferredMethodCall}]
	if use == "" {
		return nil, nil
	}
	member, ok := u.Builder.Exprs.Member(call.Target)
	if !ok || member == nil {
		return nil, fmt.Errorf("return origins: deferred method %s has no member target", use)
	}
	var found *DeferredCallableEdge
	for _, edge := range u.Sema.InstantiationGraph.DeferredCallables() {
		if edge.UseID != use || edge.Kind != DeferredMethodCall {
			continue
		}
		if found != nil || edge.Witness.SourceKey != u.SourceKey ||
			edge.Witness.Site != u.Builder.Exprs.Get(id).Span ||
			edge.ExpectedResult != u.Sema.ExprTypes[id] || len(edge.Args) != len(call.Args) {
			return nil, fmt.Errorf("return origins: inconsistent deferred method evidence for %s", use)
		}
		if actual := u.Sema.ExprTypes[member.Target]; !edge.StaticReceiver && actual != types.NoTypeID && actual != edge.Receiver {
			return nil, fmt.Errorf("return origins: inconsistent deferred receiver for %s", use)
		}
		copy := edge
		found = &copy
	}
	if found == nil {
		return nil, fmt.Errorf("return origins: missing deferred method evidence for %s", use)
	}
	return found, nil
}

// A reference-free result says nothing about writes through the inputs.
// Keep reference-content/callable/generic effects open until their transfer
// is implemented; the early borrow checker remains independently mandatory.
func returnOriginCallHasUnprovedEffects(in *types.Interner, params []types.TypeID) bool {
	for _, param := range params {
		if target, ok := in.AliasTarget(param); ok {
			param = target
		}
		info, ok := in.Lookup(param)
		if !ok {
			return true
		}
		if info.Kind == types.KindReference {
			if returnOriginTypeShape(in, info.Elem, nil) != returnOriginRefFree {
				return true
			}
			continue
		}
		if returnOriginTypeShape(in, param, nil) != returnOriginRefFree {
			return true
		}
	}
	return false
}

func returnOriginIsReference(in *types.Interner, id types.TypeID) bool {
	seen := make(map[types.TypeID]bool)
	for !seen[id] {
		seen[id] = true
		if target, ok := in.AliasTarget(id); ok {
			id = target
			continue
		}
		info, ok := in.Lookup(id)
		return ok && info.Kind == types.KindReference
	}
	return false
}
