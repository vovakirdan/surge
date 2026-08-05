package sema

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// DeferredCallableRequirement is the exact contract member approved while
// checking a generic body. Attributes are part of the requirement identity.
type DeferredCallableRequirement struct {
	Contracts []symbols.SymbolID
	Name      string
	Params    []types.TypeID
	Result    types.TypeID
	Attrs     []string
	Public    bool
	Async     bool
}

// CallableCandidate is a detached, mergeable semantic description of one
// callable body. Deferred resolution never reconstructs candidates from the
// post-merge symbol table.
type CallableCandidate struct {
	Symbol      symbols.SymbolID
	BodyKey     string
	Name        string
	ReceiverKey symbols.TypeKey
	Params      []symbols.TypeKey
	Result      symbols.TypeKey
	// ReceiverType is the declared dispatch owner (the extern receiver), while
	// ParamTypes[0] is the self ABI for instance methods. ParamTypes,
	// ResultType, and TemplateParams are the exact shared-interner descriptors
	// approved by sema. The post-merge resolver must not parse TypeKey strings
	// or revisit a symbol table to reconstruct the callable it is selecting.
	ReceiverType   types.TypeID
	ParamTypes     []types.TypeID
	ResultType     types.TypeID
	TemplateParams []types.TypeID
	// ReceiverTemplateArity is the declaration-ordered prefix contributed by
	// the generic receiver. Explicit method type arguments bind only the
	// remaining suffix.
	ReceiverTemplateArity int
	TypeParams            []string
	Defaults              []bool
	Variadic              []bool
	Attrs                 []string
	HasSelf               bool
	HasBody               bool
	Public                bool
	FilePrivate           bool
	Builtin               bool
	Async                 bool
	Intrinsic             bool
	ModulePath            string
	Source                source.Span
	SourceKey             string
}

func localDeferredUseID(kind DeferredCallableKind, site source.Span, ordinal uint32) DeferredUseID {
	return DeferredUseID(fmt.Sprintf("local/%d/%d/%d/%d/%d", site.File, site.Start, site.End, kind, ordinal))
}

func canonicalDeferredUseID(sourceKey string, kind DeferredCallableKind, site source.Span, ordinal uint32) DeferredUseID {
	return DeferredUseID(fmt.Sprintf("%s/%d:%d/%d/%d", sourceKey, site.Start, site.End, kind, ordinal))
}

func cloneDeferredCallableRequirement(req *DeferredCallableRequirement) DeferredCallableRequirement {
	if req == nil {
		return DeferredCallableRequirement{}
	}
	cloned := *req
	cloned.Contracts = slices.Clone(req.Contracts)
	cloned.Params = slices.Clone(req.Params)
	cloned.Attrs = slices.Clone(req.Attrs)
	return cloned
}

func cloneCallableCandidate(candidate *CallableCandidate) CallableCandidate {
	cloned := *candidate
	cloned.Params = slices.Clone(candidate.Params)
	cloned.ParamTypes = slices.Clone(candidate.ParamTypes)
	cloned.TemplateParams = slices.Clone(candidate.TemplateParams)
	cloned.TypeParams = slices.Clone(candidate.TypeParams)
	cloned.Defaults = slices.Clone(candidate.Defaults)
	cloned.Variadic = slices.Clone(candidate.Variadic)
	cloned.Attrs = slices.Clone(candidate.Attrs)
	return cloned
}

func (tc *typeChecker) rememberCallableCandidate(symID symbols.SymbolID, fn *ast.FnItem) {
	if tc == nil || tc.result == nil || fn == nil || !symID.IsValid() {
		return
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		return
	}
	attrs := make([]string, 0, len(tc.symbolAttrs[symID]))
	for _, info := range tc.symbolAttrs[symID] {
		if info.Spec.Name != "" {
			attrs = append(attrs, info.Spec.Name)
		}
	}
	sort.Strings(attrs)
	attrs = slices.Compact(attrs)
	typeParams := make([]string, 0, len(sym.TypeParams))
	for _, name := range sym.TypeParams {
		if text := tc.lookupName(name); text != "" {
			typeParams = append(typeParams, text)
		}
	}
	modulePath := sym.ModulePath
	if modulePath == "" {
		modulePath = tc.modulePath
	}
	var paramTypes []types.TypeID
	resultType := types.NoTypeID
	if fnInfo, ok := tc.types.FnInfo(sym.Type); ok && fnInfo != nil {
		paramTypes = slices.Clone(fnInfo.Params)
		resultType = fnInfo.Result
	}
	receiverType := types.NoTypeID
	if sym.Receiver.IsValid() {
		receiverType = tc.resolveTypeExprWithScope(sym.Receiver, sym.Scope)
	}
	if receiverType == types.NoTypeID && sym.ReceiverKey != "" {
		receiverType = tc.typeFromKey(sym.ReceiverKey)
	}
	if receiverType == types.NoTypeID && sym.Signature.HasSelf && len(paramTypes) > 0 {
		// Defensive fallback for synthesized callables without a receiver AST.
		receiverType = tc.valueType(paramTypes[0])
	}
	receiverTemplateArity := len(tc.receiverTypeArgs(receiverType))
	templateParams := slices.Clone(tc.result.InstantiationTemplateParams[symID])
	if receiverTemplateArity > len(templateParams) {
		receiverTemplateArity = len(templateParams)
	}
	candidate := CallableCandidate{
		Symbol:                symID,
		Name:                  tc.symbolName(sym.Name),
		ReceiverKey:           sym.ReceiverKey,
		Params:                slices.Clone(sym.Signature.Params),
		Result:                sym.Signature.Result,
		ReceiverType:          receiverType,
		ParamTypes:            slices.Clone(paramTypes),
		ResultType:            resultType,
		TemplateParams:        templateParams,
		ReceiverTemplateArity: receiverTemplateArity,
		TypeParams:            typeParams,
		Defaults:              slices.Clone(sym.Signature.Defaults),
		Variadic:              slices.Clone(sym.Signature.Variadic),
		Attrs:                 attrs,
		HasSelf:               sym.Signature.HasSelf,
		HasBody:               sym.Signature.HasBody,
		Public:                sym.Flags&symbols.SymbolFlagPublic != 0,
		FilePrivate:           sym.Flags&symbols.SymbolFlagFilePrivate != 0,
		Builtin:               sym.Flags&symbols.SymbolFlagBuiltin != 0,
		Async:                 fn.Flags&ast.FnModifierAsync != 0,
		ModulePath:            modulePath,
		Source:                sym.Span,
	}
	candidate.Intrinsic = slices.Contains(candidate.Attrs, "intrinsic")
	for i := range tc.result.CallableCandidates {
		if tc.result.CallableCandidates[i].Symbol == symID {
			tc.result.CallableCandidates[i] = candidate
			return
		}
	}
	tc.result.CallableCandidates = append(tc.result.CallableCandidates, candidate)
}

func canonicalCallableBodyKey(candidate *CallableCandidate) string {
	if candidate == nil {
		return ""
	}
	return fmt.Sprintf(
		"%s|%s:%d:%d|%s|%s|(%s)->%s|tp=%s|rtp=%d|attrs=%s|async=%t|intrinsic=%t",
		candidate.ModulePath,
		candidate.SourceKey,
		candidate.Source.Start,
		candidate.Source.End,
		candidate.ReceiverKey,
		candidate.Name,
		joinTypeKeys(candidate.Params),
		candidate.Result,
		strings.Join(candidate.TypeParams, ","),
		candidate.ReceiverTemplateArity,
		strings.Join(candidate.Attrs, ","),
		candidate.Async,
		candidate.Intrinsic,
	)
}

func joinTypeKeys(keys []symbols.TypeKey) string {
	parts := make([]string, len(keys))
	for i := range keys {
		parts[i] = string(keys[i])
	}
	return strings.Join(parts, ",")
}
