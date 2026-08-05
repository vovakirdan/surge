package sema

import (
	"fmt"
	"slices"
	"sort"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// CloneUseView is the lexical vantage point of one clone use site. It is applied
// only after the program-wide canonical implementation has been chosen, so a
// visibility failure never degrades into a different implementation.
type CloneUseView struct {
	AccessModule string
	SourceKey    string
}

// GlobalCloneHook is the single canonical `__clone` implementation of one
// concrete type. It is selected from the whole-program callable catalog and is
// therefore identical for every use site in the program.
type GlobalCloneHook struct {
	Receiver     types.TypeID
	Callee       symbols.SymbolID
	BodyKey      string
	TemplateArgs []types.TypeID
	ParamTypes   []types.TypeID
	ResultType   types.TypeID
	Public       bool
	FilePrivate  bool
	Builtin      bool
	ModulePath   string
	SourceKey    string
	Decl         source.Span
	// DeclKeyword is where a `pub` modifier belongs, which is not where Decl
	// points. See CallableCandidate.DeclKeyword.
	DeclKeyword source.Span
}

func (h *GlobalCloneHook) visibleFrom(view CloneUseView) bool {
	if h == nil {
		return false
	}
	if h.Builtin {
		return true
	}
	if h.FilePrivate && h.SourceKey != view.SourceKey {
		return false
	}
	return h.Public || h.ModulePath == view.AccessModule
}

type cloneSelectionKind uint8

const (
	// cloneSelectionResolved names exactly one canonical implementation.
	cloneSelectionResolved cloneSelectionKind = iota + 1
	// cloneSelectionAbsent means no declaration named `__clone` claims this type.
	cloneSelectionAbsent
	// cloneSelectionShapeRejected means a `__clone` is declared for this type but
	// none of the declarations has the required `fn(self: &T) -> T` shape.
	cloneSelectionShapeRejected
	// cloneSelectionConflict means several equally specific bodies remain.
	cloneSelectionConflict
	// cloneSelectionUnmaterializable means the winner has neither a body nor an
	// intrinsic implementation. A weaker candidate is never substituted.
	cloneSelectionUnmaterializable
)

type cloneSelection struct {
	kind     cloneSelectionKind
	hook     GlobalCloneHook
	rivals   []GlobalCloneHook
	internal error
}

// cloneCanonicalSelector answers "which body clones this type" for the whole
// program. The answer is memoized per concrete receiver because it does not
// depend on who is asking.
type cloneCanonicalSelector struct {
	candidates []CallableCandidate
	types      *types.Interner
	cache      map[types.TypeID]cloneSelection
}

func newCloneCanonicalSelector(candidates []CallableCandidate, typesIn *types.Interner) *cloneCanonicalSelector {
	hooks := make([]CallableCandidate, 0, 4)
	for i := range candidates {
		if candidates[i].Name != "__clone" || candidates[i].ReceiverType == types.NoTypeID {
			continue
		}
		hooks = append(hooks, cloneCallableCandidate(&candidates[i]))
	}
	sort.SliceStable(hooks, func(i, j int) bool {
		return callableCandidateKey(&hooks[i]) < callableCandidateKey(&hooks[j])
	})
	return &cloneCanonicalSelector{candidates: hooks, types: typesIn, cache: make(map[types.TypeID]cloneSelection)}
}

// Select chooses the canonical implementation for one concrete type without
// consulting any use site.
func (s *cloneCanonicalSelector) Select(receiver types.TypeID) cloneSelection {
	if s == nil {
		return cloneSelection{kind: cloneSelectionAbsent}
	}
	if cached, ok := s.cache[receiver]; ok {
		return cached
	}
	selection := s.selectUncached(receiver)
	s.cache[receiver] = selection
	return selection
}

func (s *cloneCanonicalSelector) selectUncached(receiver types.TypeID) cloneSelection {
	if s.types == nil || receiver == types.NoTypeID {
		return cloneSelection{kind: cloneSelectionAbsent}
	}
	request := DeferredCallableRequest{
		Kind: DeferredCloneCall, Receiver: receiver, Method: "__clone", ExpectedResult: receiver,
	}
	matches := make([]callableMatch, 0, 2)
	declared := 0
	for i := range s.candidates {
		candidate := &s.candidates[i]
		args, specificity, ok := matchDeferredCandidate(&request, candidate, s.types)
		if !ok {
			if cloneCandidateClaimsReceiver(candidate, receiver, s.types) {
				declared++
			}
			continue
		}
		matches = append(matches, callableMatch{candidate: *candidate, typeArgs: args, specificity: specificity})
	}
	if len(matches) == 0 {
		if declared > 0 {
			return cloneSelection{kind: cloneSelectionShapeRejected}
		}
		return cloneSelection{kind: cloneSelectionAbsent}
	}
	matches = reduceToMostSpecific(matches)
	unique, err := dedupeCanonicalBodies(matches)
	if err != nil {
		return cloneSelection{internal: err}
	}
	hooks := make([]GlobalCloneHook, 0, len(unique))
	for i := range unique {
		hook, ok := newGlobalCloneHook(receiver, &unique[i], s.types)
		if !ok {
			return cloneSelection{internal: fmt.Errorf(
				"canonical __clone body %q has no concrete signature for the selected receiver",
				callableCandidateKey(&unique[i].candidate),
			)}
		}
		hooks = append(hooks, hook)
	}
	if len(hooks) > 1 {
		return cloneSelection{kind: cloneSelectionConflict, hook: hooks[0], rivals: hooks}
	}
	winner := &unique[0].candidate
	if !winner.HasBody && !winner.Intrinsic && !winner.Builtin {
		return cloneSelection{kind: cloneSelectionUnmaterializable, hook: hooks[0]}
	}
	return cloneSelection{kind: cloneSelectionResolved, hook: hooks[0]}
}

// Resolve applies one use site's lexical view to the already-chosen canonical
// implementation. It never widens the search after a visibility failure.
func (s *cloneCanonicalSelector) Resolve(
	receiver types.TypeID,
	view CloneUseView,
	site source.Span,
	typeLabel string,
) (GlobalCloneHook, error) {
	selection := s.Select(receiver)
	if selection.internal != nil {
		return GlobalCloneHook{}, selection.internal
	}
	if typeLabel == "" && s != nil {
		typeLabel = types.Label(s.types, receiver)
	}
	switch selection.kind {
	case cloneSelectionResolved:
		if !selection.hook.visibleFrom(view) {
			return GlobalCloneHook{}, newCloneNotVisibleError(&selection.hook, view, site, typeLabel)
		}
		return selection.hook, nil
	case cloneSelectionConflict:
		return GlobalCloneHook{}, newCloneConflictError(selection.rivals, site, typeLabel)
	case cloneSelectionShapeRejected, cloneSelectionUnmaterializable:
		return GlobalCloneHook{}, newCloneInvalidSignatureError(site, typeLabel)
	default:
		return GlobalCloneHook{}, newCloneNotClonableError(site, typeLabel)
	}
}

func resolveDeferredCloneCall(
	request *DeferredCallableRequest,
	clones *cloneCanonicalSelector,
) (DeferredCallableResolution, error) {
	hook, err := clones.Resolve(
		request.Receiver,
		CloneUseView{AccessModule: request.AccessModule, SourceKey: request.SourceKey},
		request.Site,
		"",
	)
	if err != nil {
		return DeferredCallableResolution{}, err
	}
	return DeferredCallableResolution{
		Outcome:      DeferredCallableResolved,
		Callee:       hook.Callee,
		CalleeKey:    hook.BodyKey,
		TemplateArgs: slices.Clone(hook.TemplateArgs),
		ParamTypes:   slices.Clone(hook.ParamTypes),
		ResultType:   hook.ResultType,
	}, nil
}

func reduceToMostSpecific(matches []callableMatch) []callableMatch {
	if len(matches) <= 1 {
		return matches
	}
	best := matches[0].specificity
	for i := 1; i < len(matches); i++ {
		if compareCallableSpecificity(matches[i].specificity, best) < 0 {
			best = matches[i].specificity
		}
	}
	preferred := matches[:0]
	for i := range matches {
		if compareCallableSpecificity(matches[i].specificity, best) == 0 {
			preferred = append(preferred, matches[i])
		}
	}
	return preferred
}

// dedupeCanonicalBodies collapses imported aliases of one declaration. The input
// is ordered by canonical body key, so equal bodies are adjacent.
func dedupeCanonicalBodies(matches []callableMatch) ([]callableMatch, error) {
	unique := matches[:0]
	for i := range matches {
		if len(unique) > 0 && callableCandidateKey(&unique[len(unique)-1].candidate) == callableCandidateKey(&matches[i].candidate) {
			if !callableMatchesEquivalent(&unique[len(unique)-1], &matches[i]) {
				return nil, fmt.Errorf(
					"canonical __clone body %q maps to inconsistent callable records",
					callableCandidateKey(&matches[i].candidate),
				)
			}
			continue
		}
		unique = append(unique, matches[i])
	}
	return unique, nil
}

func newGlobalCloneHook(receiver types.TypeID, match *callableMatch, typesIn *types.Interner) (GlobalCloneHook, bool) {
	paramTypes, resultType, ok := instantiateCallableSignature(&match.candidate, match.typeArgs, typesIn)
	if !ok {
		return GlobalCloneHook{}, false
	}
	return GlobalCloneHook{
		Receiver:     receiver,
		Callee:       match.candidate.Symbol,
		BodyKey:      callableCandidateKey(&match.candidate),
		TemplateArgs: slices.Clone(match.typeArgs),
		ParamTypes:   paramTypes,
		ResultType:   resultType,
		Public:       match.candidate.Public,
		FilePrivate:  match.candidate.FilePrivate,
		Builtin:      match.candidate.Builtin,
		ModulePath:   match.candidate.ModulePath,
		SourceKey:    match.candidate.SourceKey,
		Decl:         match.candidate.Source,
		DeclKeyword:  match.candidate.DeclKeyword,
	}, true
}

// cloneCandidateClaimsReceiver reports whether a declaration names this type as
// its clone receiver, regardless of whether the rest of its signature is valid.
// It separates "no `__clone` at all" from "a `__clone` with the wrong shape".
func cloneCandidateClaimsReceiver(candidate *CallableCandidate, receiver types.TypeID, typesIn *types.Interner) bool {
	bindings := make(map[types.TypeID]types.TypeID, len(candidate.TemplateParams))
	for _, param := range candidate.TemplateParams {
		bindings[param] = types.NoTypeID
	}
	return matchDeferredReceiver(typesIn, candidate.ReceiverType, receiver, bindings)
}
