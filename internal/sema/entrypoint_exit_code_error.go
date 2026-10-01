package sema

import (
	"errors"
	"fmt"

	"surge/internal/diag"
	"surge/internal/types"
)

// newEntrypointExitCodeError reports an @entrypoint result that is neither
// `nothing` nor `int` and has no ExitCode implementation the generated entry can
// call: exactly one visible `fn __exit_code(self: &T) -> int`.
func newEntrypointExitCodeError(
	request *EntrypointCallableRequest,
	cause error,
	candidates []CallableCandidate,
	typesIn *types.Interner,
) error {
	typeLabel := request.TypeLabel
	if typeLabel == "" {
		typeLabel = "result type"
	}
	required := fmt.Sprintf("fn __exit_code(self: &%s) -> int", typeLabel)
	diagnostic := &diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.SemaEntrypointReturnNotConvertible,
		Primary:  request.Site,
		Message: fmt.Sprintf("@entrypoint return type must be 'nothing', 'int', or implement the ExitCode contract; %q does not",
			typeLabel),
		Notes: []diag.Note{
			{Span: request.Site, Msg: "the generated entry exits with the code `__exit_code()` returns; required: " + required},
		},
	}
	var resolution *DeferredCallableResolutionError
	switch {
	case errors.Is(cause, errExitCodeSelfByValue):
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{Span: request.Site,
			Msg: "the ExitCode contract borrows the result: declare the receiver as `self: &" + typeLabel + "`"})
	case errors.As(cause, &resolution) && resolution.Reason != "":
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{Span: request.Site, Msg: friendlyExitCodeResolutionReason(resolution.Reason)})
	}
	if request.CanDefineHere {
		diagnostic.Help = append(diagnostic.Help, diag.Note{
			Span: request.Site,
			Msg:  fmt.Sprintf("add `%s` in an `extern<%s>` block, or return `int`", required, typeLabel),
		})
	}
	found := entrypointParserCandidates(request, candidates, typesIn)
	for i := range found {
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: found[i].Source,
			Msg:  exitCodeCandidateMismatch(&found[i], typesIn),
		})
		if len(diagnostic.Notes) >= 7 {
			break
		}
	}
	legacy := *request
	legacy.Method = "__to"
	conversions := entrypointParserCandidates(&legacy, candidates, typesIn)
	for i := range conversions {
		candidate := &conversions[i]
		if !candidate.HasSelf || len(candidate.ParamTypes) != 2 ||
			!callableABITypeEqual(typesIn, typesIn.Builtins().Int, candidate.ResultType) {
			continue
		}
		diagnostic.Notes = append(diagnostic.Notes, diag.Note{
			Span: candidate.Source,
			Msg:  "`" + candidate.Name + "(self, int) -> int` is a conversion, not an exit code; rename it to `__exit_code(self: &T) -> int`",
		})
		break
	}
	return &EntrypointCallableError{diagnostic: diagnostic, cause: cause}
}

func friendlyExitCodeResolutionReason(reason string) string {
	switch reason {
	case "ambiguous equally valid implementations":
		return "multiple equally valid `__exit_code` implementations are visible; keep exactly one"
	case "matching implementation is not accessible from this source":
		return "a matching `__exit_code` exists but is not visible here; make it `pub`"
	case "matching declaration has no materializable body":
		return "a matching `__exit_code` declaration has no body"
	default:
		return "no `__exit_code` declaration matches the ExitCode contract exactly"
	}
}

func exitCodeCandidateMismatch(candidate *CallableCandidate, typesIn *types.Interner) string {
	prefix := "candidate " + entrypointCandidateSignature(candidate)
	switch {
	case !candidate.HasSelf:
		return prefix + " is static; `__exit_code` must take `self: &T`"
	case candidate.Async:
		return prefix + " is async"
	case len(candidate.ParamTypes) != 1:
		return prefix + " has parameters besides `self`"
	case !exitCodeSelfIsSharedBorrow(typesIn, candidate.ParamTypes):
		return prefix + " does not take `self: &T`"
	case !callableABITypeEqual(typesIn, typesIn.Builtins().Int, candidate.ResultType):
		return prefix + " does not return `int`"
	case !candidate.HasBody && !candidate.Intrinsic && !candidate.Builtin:
		return prefix + " has no implementation"
	default:
		return prefix + " conflicts with another equally valid implementation"
	}
}
