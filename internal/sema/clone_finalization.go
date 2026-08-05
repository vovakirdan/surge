package sema

import (
	"fmt"
	"slices"
	"sort"

	"surge/internal/ast"
	"surge/internal/symbols"
)

// FinalizeDirectCloneBindings answers every direct `clone(&value)` against the
// merged callable catalog. Selection is program-wide and the use site's lexical
// view is applied only to the winner, so one type never clones two ways.
func (r *Result) FinalizeDirectCloneBindings() error {
	if r == nil || len(r.DirectCloneRequests) == 0 {
		return nil
	}
	requests := cloneDirectCloneRequests(r.DirectCloneRequests)
	sort.SliceStable(requests, func(i, j int) bool {
		return compareDirectCloneRequests(&requests[i], &requests[j]) < 0
	})
	selector := newCloneCanonicalSelector(r.CallableCandidates, r.TypeInterner)
	bindings := make([]DirectCloneBinding, 0, len(requests))
	for i := range requests {
		request := &requests[i]
		hook, err := selector.Resolve(
			request.Receiver,
			CloneUseView{AccessModule: request.AccessModule, SourceKey: request.SourceKey},
			request.Site,
			request.TypeLabel,
		)
		if err != nil {
			return err
		}
		binding := DirectCloneBinding{
			Use: request.Use, Callee: hook.Callee, CalleeKey: hook.BodyKey,
			TemplateArgs: slices.Clone(hook.TemplateArgs), Site: request.Site, SourceKey: request.SourceKey,
		}
		r.recordDirectCloneReachability(request, &binding)
		if len(bindings) > 0 && sameDirectCloneUse(&bindings[len(bindings)-1], &binding) {
			if !directCloneBindingsEqual(&bindings[len(bindings)-1], &binding) {
				return fmt.Errorf("clone use %s:%d has conflicting bindings", binding.SourceKey, binding.Use)
			}
			continue
		}
		bindings = append(bindings, binding)
	}
	r.DirectCloneBindings = bindings
	return nil
}

// recordDirectCloneReachability keeps the selected body alive. Without the edge
// from the enclosing function, reachability never reaches the implementation and
// the backend emits a call into nothing.
func (r *Result) recordDirectCloneReachability(request *DirectCloneRequest, binding *DirectCloneBinding) {
	if !request.Owner.IsValid() || !binding.Callee.IsValid() {
		return
	}
	if len(binding.TemplateArgs) == 0 {
		if r.FunctionCallEdges == nil {
			r.FunctionCallEdges = make(map[symbols.SymbolID]map[symbols.SymbolID]struct{})
		}
		if r.FunctionCallEdges[request.Owner] == nil {
			r.FunctionCallEdges[request.Owner] = make(map[symbols.SymbolID]struct{})
		}
		r.FunctionCallEdges[request.Owner][binding.Callee] = struct{}{}
		return
	}
	r.InstantiationGraph.recordRoot(&InstantiationRoot{
		Kind: InstantiationFunction, Template: binding.Callee, TemplateArgs: binding.TemplateArgs,
		Witness: InstantiationWitness{
			Site: request.Site, SourceKey: request.SourceKey, Caller: request.Owner, Reason: "clone implementation",
		},
	})
}

func sameDirectCloneUse(left, right *DirectCloneBinding) bool {
	return left.SourceKey == right.SourceKey && left.Use == right.Use
}

func directCloneBindingsEqual(left, right *DirectCloneBinding) bool {
	return sameDirectCloneUse(left, right) && left.Callee == right.Callee &&
		left.CalleeKey == right.CalleeKey && slices.Equal(left.TemplateArgs, right.TemplateArgs)
}

// publishDirectCloneBindings projects the decisions owned by one source file
// into that file's symbol vocabulary. It returns the complete map so the caller
// can install it only after every symbol has localized.
func publishDirectCloneBindings(
	authority *Result,
	publication FinalizationPublication,
) (map[ast.ExprID]symbols.SymbolID, error) {
	out := make(map[ast.ExprID]symbols.SymbolID, len(authority.DirectCloneBindings))
	for i := range authority.DirectCloneBindings {
		binding := &authority.DirectCloneBindings[i]
		if binding.SourceKey != publication.SourceKey {
			continue
		}
		callee := binding.Callee
		if len(publication.RootToLocalSymbols) > 0 {
			local, err := publication.localCloneCallable(callee, binding.CalleeKey)
			if err != nil {
				return nil, err
			}
			callee = local
		}
		out[binding.Use] = callee
	}
	return out, nil
}

// localCloneCallable resolves a merged callable into one file's vocabulary.
//
// A locally declared body is matched by its canonical identity. A clone winner
// is routinely declared in a module the consuming file only imports, though, and
// then no local declaration carries the body. The symbol aliases recorded for
// one merged symbol are an equivalence class built from equal module symbol
// identity, so every member names that same declaration; the lowest is taken to
// keep the choice stable across runs.
func (p FinalizationPublication) localCloneCallable(
	root symbols.SymbolID,
	bodyKey string,
) (symbols.SymbolID, error) {
	if symbol, err := p.localCallable(root, bodyKey); err == nil {
		return symbol, nil
	}
	aliases := p.RootToLocalSymbols[root]
	if len(aliases) == 0 {
		return 0, fmt.Errorf("no local alias for clone implementation %d body %q", root, bodyKey)
	}
	lowest := aliases[0]
	for _, alias := range aliases[1:] {
		if alias < lowest {
			lowest = alias
		}
	}
	return lowest, nil
}
