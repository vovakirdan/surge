package sema

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"surge/internal/symbols"
	"surge/internal/types"
)

// DeferredCallableResolutionError is a deterministic post-merge semantic
// failure.  Later diagnostic plumbing may translate it to a source diagnostic;
// mono must never guess a different callable after this point.
type DeferredCallableResolutionError struct {
	UseID      DeferredUseID
	Method     string
	Reason     string
	Candidates []string
}

func (e *DeferredCallableResolutionError) Error() string {
	if e == nil {
		return "deferred callable resolution failed"
	}
	message := fmt.Sprintf("deferred callable %s (%s): %s", e.UseID, e.Method, e.Reason)
	if len(e.Candidates) > 0 {
		message += "; candidates: " + strings.Join(e.Candidates, ", ")
	}
	return message
}

type callableMatch struct {
	candidate   CallableCandidate
	typeArgs    []types.TypeID
	specificity callableSpecificity
}

type callableSpecificity struct {
	receiverDistance      int
	receiverTemplateArity int
}

func resolveDeferredCallable(
	useID DeferredUseID,
	request DeferredCallableRequest,
	candidates []CallableCandidate,
	typesIn *types.Interner,
) (DeferredCallableResolution, error) {
	if typesIn == nil || request.Receiver == types.NoTypeID || request.Method == "" {
		return DeferredCallableResolution{}, &DeferredCallableResolutionError{UseID: useID, Method: request.Method, Reason: "incomplete concrete call shape"}
	}
	if request.Kind == DeferredCloneCall && typesIn.IsCopy(resolveCallableAlias(typesIn, request.Receiver)) {
		return DeferredCallableResolution{Outcome: DeferredCallableBuiltinCopy, CalleeKey: "builtin/copy"}, nil
	}

	ordered := make([]CallableCandidate, len(candidates))
	for i := range candidates {
		ordered[i] = cloneCallableCandidate(&candidates[i])
	}
	sort.SliceStable(ordered, func(i, j int) bool { return callableCandidateKey(&ordered[i]) < callableCandidateKey(&ordered[j]) })

	shapeMatches := make([]callableMatch, 0, 2)
	for i := range ordered {
		candidate := &ordered[i]
		if candidate.Name != request.Method || candidate.ReceiverType == types.NoTypeID {
			continue
		}
		args, specificity, ok := matchDeferredCandidate(request, candidate, typesIn)
		if !ok {
			continue
		}
		shapeMatches = append(shapeMatches, callableMatch{candidate: *candidate, typeArgs: args, specificity: specificity})
	}
	if len(shapeMatches) > 1 {
		best := shapeMatches[0].specificity
		for i := 1; i < len(shapeMatches); i++ {
			if compareCallableSpecificity(shapeMatches[i].specificity, best) < 0 {
				best = shapeMatches[i].specificity
			}
		}
		preferred := shapeMatches[:0]
		for i := range shapeMatches {
			if compareCallableSpecificity(shapeMatches[i].specificity, best) == 0 {
				preferred = append(preferred, shapeMatches[i])
			}
		}
		shapeMatches = preferred
	}

	matches := make([]callableMatch, 0, len(shapeMatches))
	inaccessible := make([]string, 0, 2)
	invalid := make([]string, 0, 2)
	for i := range shapeMatches {
		match := shapeMatches[i]
		key := callableCandidateKey(&match.candidate)
		if !callableCandidateAccessible(request, &match.candidate) {
			inaccessible = append(inaccessible, key)
			continue
		}
		if !match.candidate.HasBody && !match.candidate.Intrinsic && !match.candidate.Builtin {
			invalid = append(invalid, key)
			continue
		}
		matches = append(matches, match)
	}

	// Imported aliases of one declaration collapse by canonical body identity.
	unique := matches[:0]
	for i := range matches {
		if len(unique) > 0 && callableCandidateKey(&unique[len(unique)-1].candidate) == callableCandidateKey(&matches[i].candidate) {
			if !callableMatchesEquivalent(&unique[len(unique)-1], &matches[i]) {
				return DeferredCallableResolution{}, &DeferredCallableResolutionError{
					UseID: useID, Method: request.Method,
					Reason:     "canonical body identity maps to inconsistent callable records",
					Candidates: []string{callableCandidateKey(&matches[i].candidate)},
				}
			}
			continue
		}
		unique = append(unique, matches[i])
	}
	matches = unique
	if len(matches) == 1 {
		match := matches[0]
		paramTypes, resultType, ok := instantiateCallableSignature(&match.candidate, match.typeArgs, typesIn)
		if !ok {
			return DeferredCallableResolution{}, &DeferredCallableResolutionError{
				UseID: useID, Method: request.Method, Reason: "selected implementation has an invalid concrete signature",
				Candidates: []string{callableCandidateKey(&match.candidate)},
			}
		}
		return DeferredCallableResolution{
			Outcome:      DeferredCallableResolved,
			Callee:       match.candidate.Symbol,
			CalleeKey:    callableCandidateKey(&match.candidate),
			TemplateArgs: slices.Clone(match.typeArgs),
			ParamTypes:   paramTypes,
			ResultType:   resultType,
		}, nil
	}
	if len(matches) > 1 {
		keys := make([]string, len(matches))
		for i := range matches {
			keys[i] = callableCandidateKey(&matches[i].candidate)
		}
		return DeferredCallableResolution{}, &DeferredCallableResolutionError{UseID: useID, Method: request.Method, Reason: "ambiguous equally valid implementations", Candidates: keys}
	}
	if len(inaccessible) > 0 {
		return DeferredCallableResolution{}, &DeferredCallableResolutionError{UseID: useID, Method: request.Method, Reason: "matching implementation is not accessible from this source", Candidates: slices.Compact(inaccessible)}
	}
	if len(invalid) > 0 {
		return DeferredCallableResolution{}, &DeferredCallableResolutionError{UseID: useID, Method: request.Method, Reason: "matching declaration has no materializable body", Candidates: slices.Compact(invalid)}
	}
	return DeferredCallableResolution{}, &DeferredCallableResolutionError{UseID: useID, Method: request.Method, Reason: "no exact implementation"}
}

func compareCallableSpecificity(left, right callableSpecificity) int {
	if left.receiverDistance != right.receiverDistance {
		return left.receiverDistance - right.receiverDistance
	}
	return left.receiverTemplateArity - right.receiverTemplateArity
}

func callableMatchesEquivalent(left, right *callableMatch) bool {
	if left == nil || right == nil {
		return left == right
	}
	a, b := &left.candidate, &right.candidate
	return a.Symbol == b.Symbol && a.BodyKey == b.BodyKey && a.Name == b.Name &&
		a.ReceiverKey == b.ReceiverKey && a.Result == b.Result && a.ReceiverType == b.ReceiverType &&
		a.ResultType == b.ResultType && a.ReceiverTemplateArity == b.ReceiverTemplateArity &&
		a.HasSelf == b.HasSelf && a.HasBody == b.HasBody &&
		a.Public == b.Public && a.FilePrivate == b.FilePrivate && a.Builtin == b.Builtin &&
		a.Async == b.Async && a.Intrinsic == b.Intrinsic && a.ModulePath == b.ModulePath &&
		a.Source == b.Source && a.SourceKey == b.SourceKey &&
		slices.Equal(a.Params, b.Params) && slices.Equal(a.ParamTypes, b.ParamTypes) &&
		slices.Equal(a.TemplateParams, b.TemplateParams) && slices.Equal(a.TypeParams, b.TypeParams) &&
		slices.Equal(a.Defaults, b.Defaults) && slices.Equal(a.Variadic, b.Variadic) &&
		slices.Equal(a.Attrs, b.Attrs) && slices.Equal(left.typeArgs, right.typeArgs)
}

func instantiateCallableSignature(candidate *CallableCandidate, typeArgs []types.TypeID, typesIn *types.Interner) ([]types.TypeID, types.TypeID, bool) {
	if candidate == nil {
		return nil, types.NoTypeID, false
	}
	params := slices.Clone(candidate.ParamTypes)
	result := candidate.ResultType
	if len(candidate.TemplateParams) == 0 {
		return params, result, result != types.NoTypeID
	}
	subst, err := callableCandidateSubstitution(typesIn, candidate.TemplateParams, typeArgs)
	if err != nil || subst == nil {
		return nil, types.NoTypeID, false
	}
	for i := range params {
		params[i], err = subst.typeID(params[i])
		if err != nil || params[i] == types.NoTypeID || types.ContainsGenericParam(typesIn, params[i]) {
			return nil, types.NoTypeID, false
		}
	}
	result, err = subst.typeID(result)
	if err != nil || result == types.NoTypeID || types.ContainsGenericParam(typesIn, result) {
		return nil, types.NoTypeID, false
	}
	return params, result, true
}

func matchDeferredCandidate(request DeferredCallableRequest, candidate *CallableCandidate, typesIn *types.Interner) ([]types.TypeID, callableSpecificity, bool) {
	noSpecificity := callableSpecificity{}
	if candidate == nil || len(candidate.ParamTypes) == 0 && candidate.HasSelf || candidate.ResultType == types.NoTypeID {
		return nil, noSpecificity, false
	}
	if request.StaticReceiver == candidate.HasSelf || candidate.ReceiverTemplateArity < 0 || candidate.ReceiverTemplateArity > len(candidate.TemplateParams) {
		return nil, noSpecificity, false
	}
	if request.Kind == DeferredCloneCall && !validDeferredCloneShape(candidate, typesIn) {
		return nil, noSpecificity, false
	}
	bindings := make(map[types.TypeID]types.TypeID, len(candidate.TemplateParams))
	for _, param := range candidate.TemplateParams {
		bindings[param] = types.NoTypeID
	}
	if len(request.ExplicitTypeArgs) > 0 {
		methodArity := len(candidate.TemplateParams) - candidate.ReceiverTemplateArity
		if len(request.ExplicitTypeArgs) != methodArity {
			return nil, noSpecificity, false
		}
		for i, arg := range request.ExplicitTypeArgs {
			bindings[candidate.TemplateParams[candidate.ReceiverTemplateArity+i]] = arg
		}
	}
	paramOffset := 0
	if candidate.HasSelf {
		if !matchDeferredReceiver(typesIn, candidate.ParamTypes[0], request.Receiver, bindings) {
			return nil, noSpecificity, false
		}
		paramOffset = 1
	} else if !matchDeferredReceiver(typesIn, candidate.ReceiverType, request.Receiver, bindings) {
		return nil, noSpecificity, false
	}
	if len(candidate.ParamTypes)-paramOffset != len(request.Args) {
		return nil, noSpecificity, false
	}
	for i := range request.Args {
		if !matchCallableType(typesIn, candidate.ParamTypes[i+paramOffset], request.Args[i], bindings, true, 0) {
			return nil, noSpecificity, false
		}
	}
	typeArgs := make([]types.TypeID, len(candidate.TemplateParams))
	for i, param := range candidate.TemplateParams {
		typeArgs[i] = bindings[param]
		if typeArgs[i] == types.NoTypeID || types.ContainsGenericParam(typesIn, typeArgs[i]) {
			return nil, noSpecificity, false
		}
	}
	paramTypes, result, ok := instantiateCallableSignature(candidate, typeArgs, typesIn)
	if !ok {
		return nil, noSpecificity, false
	}
	concreteReceiver, ok := instantiateCallableReceiver(candidate, typeArgs, typesIn)
	if !ok {
		return nil, noSpecificity, false
	}
	receiverDistance, ok := callableReceiverDistance(typesIn, request.Receiver, concreteReceiver)
	if !ok {
		return nil, noSpecificity, false
	}
	specificity := callableSpecificity{
		receiverDistance: receiverDistance, receiverTemplateArity: candidate.ReceiverTemplateArity,
	}
	if !callableTypesEqual(typesIn, result, request.ExpectedResult) {
		return nil, noSpecificity, false
	}
	if request.Kind == DeferredCloneCall && !callableTypesEqual(typesIn, result, request.Receiver) {
		return nil, noSpecificity, false
	}
	if !callableRequirementMatches(request.Requirement, candidate, paramTypes, result, typesIn) {
		return nil, noSpecificity, false
	}
	return typeArgs, specificity, true
}

func instantiateCallableReceiver(candidate *CallableCandidate, typeArgs []types.TypeID, typesIn *types.Interner) (types.TypeID, bool) {
	if candidate == nil {
		return types.NoTypeID, false
	}
	receiver := candidate.ReceiverType
	if len(candidate.TemplateParams) == 0 {
		return receiver, receiver != types.NoTypeID
	}
	subst, err := callableCandidateSubstitution(typesIn, candidate.TemplateParams, typeArgs)
	if err != nil || subst == nil {
		return types.NoTypeID, false
	}
	receiver, err = subst.typeID(receiver)
	return receiver, err == nil && receiver != types.NoTypeID && !types.ContainsGenericParam(typesIn, receiver)
}

func validDeferredCloneShape(candidate *CallableCandidate, typesIn *types.Interner) bool {
	if candidate == nil || candidate.Name != "__clone" || !candidate.HasSelf || len(candidate.ParamTypes) != 1 || len(candidate.Defaults) != 1 || candidate.Defaults[0] || len(candidate.Variadic) != 1 || candidate.Variadic[0] {
		return false
	}
	self, ok := typesIn.Lookup(candidate.ParamTypes[0])
	return ok && self.Kind == types.KindReference && !self.Mutable
}

func callableRequirementMatches(
	requirement DeferredCallableRequirement,
	candidate *CallableCandidate,
	paramTypes []types.TypeID,
	resultType types.TypeID,
	typesIn *types.Interner,
) bool {
	if candidate == nil {
		return false
	}
	hasRequirement := len(requirement.Contracts) > 0 || requirement.Name != "" ||
		requirement.Result != types.NoTypeID || len(requirement.Attrs) > 0 || requirement.Public || requirement.Async
	if !hasRequirement {
		return true
	}
	if requirement.Name != "" && requirement.Name != candidate.Name {
		return false
	}
	if requirement.Async != candidate.Async {
		return false
	}
	// Visibility is a minimum guarantee, not part of the call ABI: a public
	// implementation may satisfy a private contract member, but not vice
	// versa. Likewise, contract attributes are requirements. Implementation
	// details such as @intrinsic may be added by the concrete callable.
	if requirement.Public && !candidate.Public {
		return false
	}
	for _, attr := range requirement.Attrs {
		if !slices.Contains(candidate.Attrs, attr) {
			return false
		}
	}
	if requirement.Result == types.NoTypeID || !callableABITypeEqual(typesIn, requirement.Result, resultType) {
		return false
	}

	actualParams := paramTypes
	if candidate.HasSelf && len(paramTypes) == len(requirement.Params)+1 {
		// Contract members may omit the implicit self parameter. This mirrors
		// the alignment used by normal contract satisfaction while retaining
		// the exact concrete ABI for every parameter that is declared.
		actualParams = paramTypes[1:]
	}
	if len(actualParams) != len(requirement.Params) {
		return false
	}
	for i := range requirement.Params {
		if !callableABITypeEqual(typesIn, requirement.Params[i], actualParams[i]) {
			return false
		}
	}
	return true
}

func callableABITypeEqual(typesIn *types.Interner, expected, actual types.TypeID) bool {
	return matchCallableType(typesIn, expected, actual, nil, false, 0)
}

func callableCandidateAccessible(request DeferredCallableRequest, candidate *CallableCandidate) bool {
	if candidate.Builtin {
		return true
	}
	if request.Kind == DeferredCloneCall {
		// clone is a language value operation. Its canonical implementation
		// belongs to the type even when the hook is not exported from the
		// declaring module; ordinary method calls still obey visibility.
		return true
	}
	if candidate.FilePrivate && candidate.SourceKey != request.SourceKey {
		return false
	}
	return candidate.Public || candidate.ModulePath == request.AccessModule
}

func callableCandidateKey(candidate *CallableCandidate) string {
	if candidate == nil {
		return ""
	}
	if candidate.BodyKey != "" {
		return candidate.BodyKey
	}
	return fmt.Sprintf("%s|%s:%d:%d|%s", candidate.ModulePath, candidate.SourceKey, candidate.Source.Start, candidate.Source.End, candidate.Name)
}

func callableCandidateSubstitution(typesIn *types.Interner, params, args []types.TypeID) (*instantiationSubstitution, error) {
	if len(params) == 0 {
		return nil, nil
	}
	if len(params) != len(args) {
		return nil, fmt.Errorf("callable substitution arity mismatch")
	}
	bindings := make([]InstantiationParamBinding, len(params))
	for i, param := range params {
		info, ok := typesIn.TypeParamInfo(param)
		if !ok || info == nil {
			return nil, fmt.Errorf("callable template parameter type#%d has no metadata", param)
		}
		bindings[i] = InstantiationParamBinding{Param: param, Owner: symbols.SymbolID(info.Owner), ParamIndex: info.Index, ArgIndex: uint32(i)}
	}
	return newInstantiationSubstitution(typesIn, bindings, args)
}
