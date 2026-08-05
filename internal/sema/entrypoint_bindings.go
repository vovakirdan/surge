package sema

import (
	"fmt"
	"slices"
	"sort"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type EntrypointCallableRole uint8

const (
	EntrypointReturnToInt EntrypointCallableRole = iota + 1
	EntrypointParamFromArgv
	EntrypointParamFromStdin
)

type EntrypointCallableOutcome uint8

const (
	EntrypointCallableUser EntrypointCallableOutcome = iota + 1
	EntrypointCallableBuiltin
)

// EntrypointCallableRequest is recorded during per-file checking and resolved
// once against the merged callable catalog. ParamIndex is meaningful only for
// EntrypointParamFromArgv and EntrypointParamFromStdin.
type EntrypointCallableRequest struct {
	Entrypoint     symbols.SymbolID
	Role           EntrypointCallableRole
	ParamIndex     uint32
	ParamName      string
	TypeLabel      string
	Receiver       types.TypeID
	Args           []types.TypeID
	ExpectedResult types.TypeID
	Method         string
	AccessModule   string
	Site           source.Span
	SourceKey      string
}

type EntrypointCallableBinding struct {
	Entrypoint     symbols.SymbolID
	Role           EntrypointCallableRole
	ParamIndex     uint32
	Outcome        EntrypointCallableOutcome
	Callee         symbols.SymbolID
	CalleeKey      string
	TemplateArgs   []types.TypeID
	ParamTypes     []types.TypeID
	Receiver       types.TypeID
	ExpectedResult types.TypeID
	Site           source.Span
	SourceKey      string
}

func (tc *typeChecker) recordEntrypointCallableRequest(request EntrypointCallableRequest) {
	if tc == nil || tc.result == nil || !request.Entrypoint.IsValid() || request.Receiver == types.NoTypeID || request.Method == "" {
		return
	}
	request.Args = slices.Clone(request.Args)
	tc.result.EntrypointCallableRequests = append(tc.result.EntrypointCallableRequests, request)
}

func canonicalizeEntrypointCallableSources(result *Result, resolve func(source.FileID) (string, error)) error {
	for i := range result.EntrypointCallableRequests {
		request := &result.EntrypointCallableRequests[i]
		key, err := resolve(request.Site.File)
		if err != nil {
			return fmt.Errorf("entrypoint callable source: %w", err)
		}
		if key == "" {
			return fmt.Errorf("entrypoint callable has empty canonical source identity")
		}
		request.SourceKey = key
	}
	return nil
}

func mergeEntrypointCallableRequests(dst, src *Result, mapping map[symbols.SymbolID]symbols.SymbolID) {
	for i := range src.EntrypointCallableRequests {
		request := src.EntrypointCallableRequests[i]
		request.Entrypoint = remapInstantiationSymbol(request.Entrypoint, mapping)
		request.Args = slices.Clone(request.Args)
		dst.EntrypointCallableRequests = append(dst.EntrypointCallableRequests, request)
	}
}

func cloneEntrypointCallableRequests(input []EntrypointCallableRequest) []EntrypointCallableRequest {
	out := make([]EntrypointCallableRequest, len(input))
	for i := range input {
		out[i] = input[i]
		out[i].Args = slices.Clone(input[i].Args)
	}
	return out
}

func cloneEntrypointCallableBindings(input []EntrypointCallableBinding) []EntrypointCallableBinding {
	out := make([]EntrypointCallableBinding, len(input))
	for i := range input {
		out[i] = input[i]
		out[i].TemplateArgs = slices.Clone(input[i].TemplateArgs)
		out[i].ParamTypes = slices.Clone(input[i].ParamTypes)
	}
	return out
}

// FinalizeEntrypointCallables resolves every synthetic startup call before
// mono/MIR. It also feeds selected bodies into the same reachability closure.
func (r *Result) FinalizeEntrypointCallables() error {
	if r == nil || len(r.EntrypointCallableRequests) == 0 {
		return nil
	}
	requests := cloneEntrypointCallableRequests(r.EntrypointCallableRequests)
	sort.SliceStable(requests, func(i, j int) bool { return compareEntrypointRequests(&requests[i], &requests[j]) < 0 })
	bindings := make([]EntrypointCallableBinding, 0, len(requests))
	for i := range requests {
		request := &requests[i]
		binding := EntrypointCallableBinding{
			Entrypoint: request.Entrypoint, Role: request.Role, ParamIndex: request.ParamIndex,
			Receiver: request.Receiver, ExpectedResult: request.ExpectedResult,
			Site: request.Site, SourceKey: request.SourceKey,
		}
		useID := DeferredUseID(fmt.Sprintf("entry/%d/%d/%d/%s", request.Role, request.ParamIndex, request.Site.Start, request.SourceKey))
		callRequest := DeferredCallableRequest{
			Kind: DeferredMethodCall, Receiver: request.Receiver, Method: request.Method,
			Args: request.Args, ExpectedResult: request.ExpectedResult,
			StaticReceiver: request.Role == EntrypointParamFromArgv || request.Role == EntrypointParamFromStdin,
			AccessModule:   request.AccessModule, SourceKey: request.SourceKey,
		}
		if request.Role == EntrypointParamFromArgv || request.Role == EntrypointParamFromStdin {
			callRequest.Requirement = DeferredCallableRequirement{
				Name: request.Method, Params: slices.Clone(request.Args), Result: request.ExpectedResult, Public: true,
			}
		}
		resolution, err := resolveDeferredCallable(useID, callRequest, r.CallableCandidates, r.TypeInterner)
		if err != nil {
			if request.Role == EntrypointParamFromArgv || request.Role == EntrypointParamFromStdin {
				return newEntrypointCallableError(request, err, r.CallableCandidates, r.TypeInterner)
			}
			return fmt.Errorf("entrypoint callable: %w", err)
		}
		if resolution.Outcome != DeferredCallableResolved || !resolution.Callee.IsValid() {
			return fmt.Errorf("entrypoint callable %s did not resolve to a callable", useID)
		}
		candidate, ok := resolvedEntrypointCandidate(&resolution, r.CallableCandidates)
		if !ok {
			return fmt.Errorf("entrypoint callable %s resolved without a catalog candidate", useID)
		}
		binding.Outcome = EntrypointCallableUser
		if candidate.Builtin || candidate.Intrinsic {
			binding.Outcome = EntrypointCallableBuiltin
		}
		binding.Callee = resolution.Callee
		binding.CalleeKey = resolution.CalleeKey
		binding.TemplateArgs = slices.Clone(resolution.TemplateArgs)
		binding.ParamTypes = slices.Clone(resolution.ParamTypes)
		if binding.Outcome == EntrypointCallableUser {
			r.recordEntrypointReachability(request, &binding)
		}
		if len(bindings) > 0 && sameEntrypointBindingKey(&bindings[len(bindings)-1], &binding) {
			if !entrypointBindingsEqual(&bindings[len(bindings)-1], &binding) {
				return fmt.Errorf("entrypoint %d has conflicting callable bindings", binding.Entrypoint)
			}
			continue
		}
		bindings = append(bindings, binding)
	}
	r.EntrypointCallableBindings = bindings
	return nil
}

func resolvedEntrypointCandidate(resolution *DeferredCallableResolution, candidates []CallableCandidate) (*CallableCandidate, bool) {
	if resolution == nil {
		return nil, false
	}
	for i := range candidates {
		candidate := &candidates[i]
		if callableCandidateKey(candidate) == resolution.CalleeKey && candidate.Symbol == resolution.Callee {
			return candidate, true
		}
	}
	return nil, false
}

func (r *Result) recordEntrypointReachability(request *EntrypointCallableRequest, binding *EntrypointCallableBinding) {
	if len(binding.TemplateArgs) == 0 {
		if r.FunctionCallEdges == nil {
			r.FunctionCallEdges = make(map[symbols.SymbolID]map[symbols.SymbolID]struct{})
		}
		if r.FunctionCallEdges[request.Entrypoint] == nil {
			r.FunctionCallEdges[request.Entrypoint] = make(map[symbols.SymbolID]struct{})
		}
		r.FunctionCallEdges[request.Entrypoint][binding.Callee] = struct{}{}
		return
	}
	r.InstantiationGraph.recordRoot(&InstantiationRoot{
		Kind: InstantiationFunction, Template: binding.Callee, TemplateArgs: binding.TemplateArgs,
		Witness: InstantiationWitness{Site: request.Site, SourceKey: request.SourceKey, Caller: request.Entrypoint, Reason: "entrypoint callable"},
	})
}

func compareEntrypointRequests(left, right *EntrypointCallableRequest) int {
	if left.Entrypoint != right.Entrypoint {
		if left.Entrypoint < right.Entrypoint {
			return -1
		}
		return 1
	}
	if left.Role != right.Role {
		return int(left.Role) - int(right.Role)
	}
	if left.ParamIndex < right.ParamIndex {
		return -1
	}
	if left.ParamIndex > right.ParamIndex {
		return 1
	}
	return compareSpanOffsets(left.Site, right.Site)
}

func sameEntrypointBindingKey(left, right *EntrypointCallableBinding) bool {
	return left.Entrypoint == right.Entrypoint && left.Role == right.Role && left.ParamIndex == right.ParamIndex
}

func entrypointBindingsEqual(left, right *EntrypointCallableBinding) bool {
	return sameEntrypointBindingKey(left, right) && left.Outcome == right.Outcome && left.Callee == right.Callee &&
		left.CalleeKey == right.CalleeKey && slices.Equal(left.TemplateArgs, right.TemplateArgs) &&
		slices.Equal(left.ParamTypes, right.ParamTypes)
}
