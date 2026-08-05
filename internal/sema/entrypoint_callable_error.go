package sema

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"surge/internal/diag"
	"surge/internal/types"
)

// EntrypointCallableError is a post-merge semantic failure with a source
// diagnostic. The driver consumes it before HIR rather than exposing an
// infrastructure error to the user.
type EntrypointCallableError struct {
	diagnostic *diag.Diagnostic
	cause      error
}

func (e *EntrypointCallableError) Error() string {
	if e == nil || e.cause == nil {
		return "entrypoint callable resolution failed"
	}
	return e.cause.Error()
}

func (e *EntrypointCallableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Diagnostic returns a detached diagnostic for the driver finalization seam.
func (e *EntrypointCallableError) Diagnostic() *diag.Diagnostic {
	if e == nil || e.diagnostic == nil {
		return nil
	}
	clone := *e.diagnostic
	clone.Notes = slices.Clone(e.diagnostic.Notes)
	clone.Fixes = slices.Clone(e.diagnostic.Fixes)
	return &clone
}

func newEntrypointCallableError(
	request *EntrypointCallableRequest,
	cause error,
	candidates []CallableCandidate,
	typesIn *types.Interner,
) error {
	if request == nil {
		return cause
	}
	code := diag.SemaEntrypointParamNoFromArgv
	contract := "FromArgv"
	argument := "value: &string"
	if request.Role == EntrypointParamFromStdin {
		code = diag.SemaEntrypointParamNoFromStdin
		contract = "FromStdin"
		argument = "text: string"
	}
	typeLabel := request.TypeLabel
	if typeLabel == "" {
		typeLabel = "parameter type"
	}
	paramLabel := request.ParamName
	if paramLabel == "" {
		paramLabel = "_"
	}
	required := fmt.Sprintf("public fn %s.%s(%s) -> Erring<%s, Error>", typeLabel, request.Method, argument, typeLabel)
	diagnostic := &diag.Diagnostic{
		Severity: diag.SevError,
		Code:     code,
		Primary:  request.Site,
		Message:  fmt.Sprintf("parameter %q of type %q does not provide the exact public %s parser", paramLabel, typeLabel, contract),
		Notes: []diag.Note{
			{Span: request.Site, Msg: "required signature: " + required},
		},
	}
	if request.CanDefineHere {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: request.Site,
			Msg:  fmt.Sprintf("help: add this exact public static method to %s", typeLabel),
		})
	} else {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: request.Site,
			Msg:  "the compiler cannot prove that this parameter type is locally extensible here",
		})
	}
	var resolution *DeferredCallableResolutionError
	if errors.As(cause, &resolution) && resolution.Reason != "" {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{Span: request.Site, Msg: friendlyEntrypointResolutionReason(resolution.Reason)})
	}
	parserCandidates := entrypointParserCandidates(request, candidates, typesIn)
	for i := range parserCandidates {
		candidate := &parserCandidates[i]
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: candidate.Source,
			Msg:  entrypointCandidateMismatch(request, candidate, typesIn),
		})
		if len(diagnostic.Notes) >= 7 {
			break
		}
	}
	return &EntrypointCallableError{diagnostic: diagnostic, cause: cause}
}

func friendlyEntrypointResolutionReason(reason string) string {
	switch reason {
	case "ambiguous equally valid implementations":
		return "multiple equally valid public parsers are visible; keep exactly one"
	case "matching implementation is not accessible from this source":
		return "a matching parser exists but is not public"
	case "matching declaration has no materializable body":
		return "a matching declaration has neither a body nor an intrinsic implementation"
	default:
		return "no declaration matches the required public parser signature exactly"
	}
}

func entrypointParserCandidates(
	request *EntrypointCallableRequest,
	candidates []CallableCandidate,
	typesIn *types.Interner,
) []CallableCandidate {
	out := make([]CallableCandidate, 0, 2)
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.Name != request.Method || candidate.ReceiverType == types.NoTypeID {
			continue
		}
		bindings := make(map[types.TypeID]types.TypeID, len(candidate.TemplateParams))
		for _, param := range candidate.TemplateParams {
			bindings[param] = types.NoTypeID
		}
		if matchDeferredReceiver(typesIn, candidate.ReceiverType, request.Receiver, bindings) {
			out = append(out, cloneCallableCandidate(candidate))
		}
	}
	slices.SortFunc(out, func(a, b CallableCandidate) int {
		return strings.Compare(callableCandidateKey(&a), callableCandidateKey(&b))
	})
	return out
}

func entrypointCandidateMismatch(request *EntrypointCallableRequest, candidate *CallableCandidate, typesIn *types.Interner) string {
	prefix := "candidate " + entrypointCandidateSignature(candidate)
	switch {
	case candidate.HasSelf:
		return prefix + " is an instance method; the parser must be static"
	case !candidate.Public:
		return prefix + " is not public"
	case candidate.Async:
		return prefix + " is async"
	case len(candidate.ParamTypes) != len(request.Args):
		return prefix + " has the wrong parameter count"
	case !entrypointCandidateParamsEqual(request.Args, candidate.ParamTypes, typesIn):
		return prefix + " has incompatible parameter types"
	case !callableABITypeEqual(typesIn, request.ExpectedResult, candidate.ResultType):
		return prefix + " has an incompatible result type"
	case !candidate.HasBody && !candidate.Intrinsic && !candidate.Builtin:
		return prefix + " has no implementation"
	default:
		return prefix + " conflicts with another equally valid parser"
	}
}

func entrypointCandidateParamsEqual(expected, actual []types.TypeID, typesIn *types.Interner) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i := range expected {
		if !callableABITypeEqual(typesIn, expected[i], actual[i]) {
			return false
		}
	}
	return true
}

func entrypointCandidateSignature(candidate *CallableCandidate) string {
	if candidate == nil {
		return "parser"
	}
	params := make([]string, len(candidate.Params))
	for i := range candidate.Params {
		params[i] = string(candidate.Params[i])
	}
	result := string(candidate.Result)
	if result == "" {
		result = "nothing"
	}
	return fmt.Sprintf("`%s(%s) -> %s`", candidate.Name, strings.Join(params, ", "), result)
}
