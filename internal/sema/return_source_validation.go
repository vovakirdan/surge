package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ReturnSourceDeclarationRequest retains typed roots before instantiation.
// The finalizer associates SourceKey/TemplateKey with the original owner, then
// validates concrete uses against this request rather than a bare FnInfo.
type ReturnSourceDeclarationRequest struct {
	Syntax       symbols.ReturnSourceSyntax
	SourceKey    string
	TemplateKey  string
	Owner        symbols.SymbolID
	TypeExpr     ast.TypeID
	Scope        symbols.ScopeID
	TypeParamEnv uint32
	params       []types.TypeID
	result       types.TypeID
}

// NewReturnSourceDeclarationRequest owns a copy of the original typed inputs.
func NewReturnSourceDeclarationRequest(syntax symbols.ReturnSourceSyntax, params []types.TypeID, result types.TypeID) ReturnSourceDeclarationRequest {
	return ReturnSourceDeclarationRequest{Syntax: syntax, params: slices.Clone(params), result: result}
}

// Params returns a detached copy of the original, possibly generic inputs.
func (r ReturnSourceDeclarationRequest) Params() []types.TypeID { return slices.Clone(r.params) }

// Result returns the original, possibly generic result.
func (r ReturnSourceDeclarationRequest) Result() types.TypeID { return r.result }

// ReturnSourceInstantiationRequest pairs concrete types with their source promise.
type ReturnSourceInstantiationRequest struct {
	Declaration  ReturnSourceDeclarationRequest
	Scope        symbols.ScopeID
	TypeParamEnv uint32
	params       []types.TypeID
	result       types.TypeID
}

// Params returns a detached copy of the concrete inputs.
func (r ReturnSourceInstantiationRequest) Params() []types.TypeID { return slices.Clone(r.params) }

// Result returns the concrete result.
func (r ReturnSourceInstantiationRequest) Result() types.TypeID { return r.result }

// ReturnSourceValidationStatus separates a proved promise from an open one.
type ReturnSourceValidationStatus uint8

const (
	// ReturnSourcesValid means this declaration or concrete use is eligible.
	ReturnSourcesValid ReturnSourceValidationStatus = iota
	// ReturnSourcesDeferred requires the original declaration and concrete types.
	ReturnSourcesDeferred
	// ReturnSourcesInvalid identifies an ineligible explicit source promise.
	ReturnSourcesInvalid
)

// ReturnSourceValidation describes an eligibility decision at its source marker.
type ReturnSourceValidation struct {
	Status ReturnSourceValidationStatus
	Slot   uint32
	Span   source.Span
	Reason string
	// Code names the rule an Invalid decision broke; zero otherwise.
	Code diag.Code
}

// ValidateDeclaredReturnSources checks the declaration before substitution.
// It does not infer body origins or compare function-value conversions.
func ValidateDeclaredReturnSources(in *types.Interner, request ReturnSourceDeclarationRequest) ReturnSourceValidation {
	markers := request.Syntax.Markers()
	if len(markers) == 0 {
		return ReturnSourceValidation{Status: ReturnSourcesValid}
	}
	deferred := false
	for _, marker := range markers {
		if marker.ArgumentCount != 0 {
			return invalidReturnSource(marker, diag.SemaReturnSourceArgument, "@return_source does not accept arguments")
		}
		if int64(marker.Slot) >= int64(len(request.params)) {
			return invalidReturnSource(marker, diag.SemaReturnSourceMissingParam, "@return_source parameter is missing from the function signature")
		}
		switch returnSourceBearing(in, request.params[marker.Slot], nil) {
		case ReturnSourcesInvalid:
			return invalidReturnSource(marker, diag.SemaReturnSourceOwnedParam, "@return_source requires a reference-bearing parameter")
		case ReturnSourcesDeferred:
			deferred = true
		}
	}
	switch returnSourceBearing(in, request.result, nil) {
	case ReturnSourcesInvalid:
		return invalidReturnSource(markers[0], diag.SemaReturnSourceOwnedResult, "@return_source requires a reference-bearing result")
	case ReturnSourcesDeferred:
		deferred = true
	}
	if deferred {
		return ReturnSourceValidation{Status: ReturnSourcesDeferred, Span: request.Syntax.Span()}
	}
	return ReturnSourceValidation{Status: ReturnSourcesValid}
}

// ValidateInstantiatedReturnSources applies a conditional generic promise.
// A ref-free specialization of a valid generic declaration has no borrowed
// result to constrain. A directly invalid owned declaration stays invalid.
func ValidateInstantiatedReturnSources(in *types.Interner, request ReturnSourceDeclarationRequest, params []types.TypeID, result types.TypeID) ReturnSourceValidation {
	original := ValidateDeclaredReturnSources(in, request)
	if original.Status == ReturnSourcesInvalid || request.Syntax.Sources().IsAllInputs() {
		return original
	}
	if len(params) != len(request.params) {
		return ReturnSourceValidation{Status: ReturnSourcesInvalid, Span: request.Syntax.Span(), Reason: "return-source instantiation has a different parameter count", Code: diag.SemaError}
	}
	if returnSourceBearing(in, result, nil) == ReturnSourcesInvalid {
		if original.Status == ReturnSourcesDeferred {
			return ReturnSourceValidation{Status: ReturnSourcesValid}
		}
		return invalidReturnSource(request.Syntax.Markers()[0], diag.SemaReturnSourceOwnedResult, "@return_source requires a reference-bearing result")
	}
	concrete := NewReturnSourceDeclarationRequest(request.Syntax, params, result)
	return ValidateDeclaredReturnSources(in, concrete)
}

func invalidReturnSource(marker symbols.ReturnSourceMarker, code diag.Code, reason string) ReturnSourceValidation {
	return ReturnSourceValidation{Status: ReturnSourcesInvalid, Slot: marker.Slot, Span: marker.Span, Reason: reason, Code: code}
}

// Only references and the existing intrinsic union/tag payload exceptions carry
// a borrow. A function returning a reference is itself an owned function value.
func returnSourceBearing(in *types.Interner, id types.TypeID, seen map[types.TypeID]bool) ReturnSourceValidationStatus {
	if in == nil || id == types.NoTypeID {
		return ReturnSourcesDeferred
	}
	if seen[id] {
		return ReturnSourcesInvalid
	}
	typ, ok := in.Lookup(id)
	if !ok {
		return ReturnSourcesDeferred
	}
	if typ.Kind == types.KindReference {
		return ReturnSourcesValid
	}
	if typ.Kind == types.KindGenericParam {
		return ReturnSourcesDeferred
	}
	if seen == nil {
		seen = make(map[types.TypeID]bool)
	}
	seen[id] = true
	defer delete(seen, id)
	if typ.Kind == types.KindAlias {
		info, found := in.AliasInfo(id)
		if !found || info == nil {
			return ReturnSourcesDeferred
		}
		return returnSourceBearing(in, info.Target, seen)
	}
	if typ.Kind != types.KindUnion {
		return ReturnSourcesInvalid
	}
	info, found := in.UnionInfo(id)
	if !found || info == nil {
		return ReturnSourcesDeferred
	}
	state := ReturnSourcesInvalid
	for _, member := range info.Members {
		payloads := append([]types.TypeID{member.Type}, member.TagArgs...)
		for _, payload := range payloads {
			if payload == types.NoTypeID {
				continue
			}
			next := returnSourceBearing(in, payload, seen)
			if next == ReturnSourcesValid {
				return next
			}
			if next == ReturnSourcesDeferred {
				state = next
			}
		}
	}
	return state
}
