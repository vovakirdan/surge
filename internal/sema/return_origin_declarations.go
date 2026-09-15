package sema

import (
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func returnOriginFnInfo(in *types.Interner, id types.TypeID) *types.FnInfo {
	seen := make(map[types.TypeID]bool)
	for !seen[id] {
		seen[id] = true
		if target, ok := in.AliasTarget(id); ok {
			id = target
			continue
		}
		info, _ := in.FnInfo(id)
		return info
	}
	return nil
}

// Builtin is a canonical callable namespace, not the file owning its AST.
// Keep that existing identity, but require the exact original declaration and
// published symbol mapping before attaching its physical source to a reader.
func (u *returnOriginUnitIndex) owningCallableIdentity(fn *ast.FnItem, id symbols.SymbolID, info *types.FnInfo) (FinalizationCallableIdentity, error) {
	identity, err := u.callableIdentity(id, "")
	if err != nil {
		return identity, err
	}
	sym := u.Symbols.Table.Symbols.Get(id)
	file := u.Builder.Files.Get(u.FileID)
	if file == nil || fn.NameSpan.File != file.Span.File || sym == nil ||
		sym.Decl.ASTFile != u.FileID || sym.Decl.SourceFile != fn.NameSpan.File ||
		sym.Span != fn.NameSpan || sym.Signature == nil || sym.Signature.HasBody != fn.Body.IsValid() {
		return identity, fmt.Errorf("return origins: callable %d lacks its original source declaration in %s", id, u.SourceKey)
	}
	for _, candidate := range u.authority.CallableCandidates {
		if candidate.BodyKey != identity.BodyKey || candidate.SourceKey != identity.SourceKey {
			continue
		}
		if len(u.Publication.RootToLocalSymbols) == 0 {
			if candidate.Symbol != id {
				continue
			}
		} else if !slices.Contains(u.Publication.RootToLocalSymbols[candidate.Symbol], id) {
			continue
		}
		if candidate.Source != fn.NameSpan || candidate.DeclKeyword != fn.FnKeywordSpan ||
			candidate.HasBody != fn.Body.IsValid() || !candidate.ReturnSources.Equal(info.ReturnSources()) ||
			!slices.Equal(candidate.ParamTypes, info.Params) || candidate.ResultType != info.Result {
			return identity, fmt.Errorf("return origins: callable %d disagrees with its original typed source in %s", id, u.SourceKey)
		}
		if identity.SourceKey != u.SourceKey && (identity.SourceKey != "builtin" || !candidate.Builtin || sym.Flags&symbols.SymbolFlagBuiltin == 0) {
			return identity, fmt.Errorf("return origins: callable %d has no certified canonical/owning source relation in %s", id, u.SourceKey)
		}
		return identity, nil
	}
	return identity, fmt.Errorf("return origins: callable %d lacks mapped original declaration authority in %s", id, u.SourceKey)
}

func (b *returnOriginBody) declaredFunctionSources(fn *returnOriginFunction, span source.Span, view ...*returnOriginSignature) ([]uint32, bool) {
	syntax := symbols.FunctionReturnSourceSyntax(fn.unit.Builder, fn.item)
	if len(view) == 0 || view[0] == nil {
		binding := returnOriginView(fn)
		view = []*returnOriginSignature{{params: fn.info.Params, result: fn.info.Result, binding: &binding}}
	}
	return b.declaredSources(fn.unit, fn.symbol, ast.NoTypeID, syntax, fn.info, span, view...)
}

// Eligibility belongs to the original declaration, not to an interned concrete
// FnInfo. In particular, an owned specialization may discharge a conditional
// generic promise without legalizing an originally invalid owned declaration.
func (b *returnOriginBody) declaredSources(u *returnOriginUnitIndex, owner symbols.SymbolID, typeExpr ast.TypeID, syntax symbols.ReturnSourceSyntax, info *types.FnInfo, span source.Span, view ...*returnOriginSignature) ([]uint32, bool) {
	params, result := returnOriginSignatureTypes(info, view...)
	binding := returnOriginView(b.function)
	if len(view) != 0 && view[0] != nil && view[0].binding != nil {
		binding = *view[0].binding
		params, result = info.Params, info.Result
	}
	if !syntax.Sources().Equal(info.ReturnSources()) {
		b.pending(span, "callable type lost its original declaration promise")
		return nil, false
	}
	if !info.ReturnSources().IsAllInputs() {
		var original *ReturnSourceDeclarationRequest
		for i := range u.Sema.ReturnSourceDeclarations {
			request := &u.Sema.ReturnSourceDeclarations[i]
			if request.Owner != owner || request.TypeExpr != typeExpr || request.Syntax.Span() != syntax.Span() {
				continue
			}
			if (request.SourceKey != "" && request.SourceKey != u.SourceKey) ||
				u.Symbols.Table.Scopes.Get(request.Scope) == nil ||
				!request.Syntax.Sources().Equal(syntax.Sources()) ||
				!slices.Equal(request.Syntax.Params(), syntax.Params()) || request.Syntax.Result() != syntax.Result() {
				b.pending(span, "return-source request does not match its owning declaration")
				return nil, false
			}
			if original != nil && (!slices.Equal(original.Params(), request.Params()) || original.Result() != request.Result() || original.Scope != request.Scope) {
				b.pending(span, "return-source declaration has ambiguous original typed roots")
				return nil, false
			}
			original = request
		}
		if original == nil {
			b.pending(span, "return-source promise lacks its original typed declaration")
			return nil, false
		}
		if !slices.Equal(original.Params(), info.Params) || original.Result() != info.Result {
			b.pending(span, "return-source promise lost its original typed roots")
			return nil, false
		}
		validation := binding.validatePromise(*original)
		if validation.Status != ReturnSourcesValid {
			if validation.Status == ReturnSourcesInvalid {
				b.returnSourceDiagnostic(validation.Span, validation.Reason, syntax.Markers())
			} else {
				b.pending(span, "return-source promise requires a concrete generic instance")
			}
			return nil, false
		}
	}
	if state, known := binding.bearing(result, nil); known && state == ReturnSourcesInvalid {
		return nil, true
	}
	var slots []uint32
	for i, param := range params {
		slot, err := safecast.Conv[uint32](i)
		if err != nil {
			b.pending(span, "return-source slot exceeds the canonical index range")
			return nil, false
		}
		if !info.ReturnSources().IsAllInputs() && !slices.Contains(info.ReturnSources().Slots(), slot) {
			continue
		}
		state, known := binding.bearing(param, nil)
		if !known {
			b.pending(span, "return-source input lacks original symbolic type authority")
			return nil, false
		}
		switch state {
		case ReturnSourcesValid:
			slots = append(slots, slot)
		case ReturnSourcesDeferred:
			slots = append(slots, slot)
		}
	}
	return slots, true
}

func (b *returnOriginBody) opaqueReturnSources(callee *returnOriginFunction, info *types.FnInfo, slots []uint32, valid bool, span source.Span, view ...*returnOriginSignature) returnOriginValue {
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	if !valid {
		return unknown
	}
	_, result := returnOriginSignatureTypes(info, view...)
	binding := returnOriginView(b.function)
	if len(view) != 0 && view[0] != nil && view[0].binding != nil {
		binding, result = *view[0].binding, info.Result
	}
	if len(slots) == 0 {
		// A certified core handle constructor keeps only what its handle word cannot hold.
		if subjects, fresh := returnOriginFreshHandleResidual(callee, result); fresh {
			value := returnOriginValueOf()
			for _, subject := range subjects {
				value = value.join(b.requireOpaqueState(binding, subject, span))
			}
			return value
		}
		return b.requireOpaqueState(binding, result, span)
	}
	if binding.shape(result) == returnOriginShapeUnknown {
		b.pending(span, "opaque result needs original borrowed-content authority")
		return unknown
	}
	value := returnOriginValueOf()
	for _, slot := range slots {
		value = value.join(returnOriginValueOf(returnOrigin{kind: returnOriginParam, param: slot}))
	}
	return value
}

func (b *returnOriginBody) checkDeclaredReturn(value returnOriginValue, allowed []uint32, span source.Span) {
	if !b.analyzer.collect || !value.normal {
		return
	}
	fn := b.function
	params := fn.unit.Builder.Items.GetFnParamIDs(fn.item)
	for _, root := range value.roots {
		// Local/captured/unknown origins were checked at scope exit. They must
		// never disappear into the formal subset relation or its diagnostic.
		if root.kind != returnOriginParam || root.expired || slices.Contains(allowed, root.param) {
			continue
		}
		if int64(root.param) >= int64(len(params)) {
			b.pending(span, "returned input has no original formal declaration")
			continue
		}
		param := fn.unit.Builder.Items.FnParam(params[root.param])
		name, _ := fn.unit.Builder.StringsInterner.Lookup(param.Name)
		b.returnSourceDiagnostic(span, fmt.Sprintf("returned reference may come from parameter '%s', which is not marked @return_source", name),
			symbols.FunctionReturnSourceSyntax(fn.unit.Builder, fn.item).Markers())
	}
}

func (b *returnOriginBody) returnSourceDiagnostic(span source.Span, message string, markers []symbols.ReturnSourceMarker) {
	if !b.analyzer.collect {
		return
	}
	for _, old := range b.analyzer.report.Diagnostics {
		if old.Primary == span && old.Message == message {
			return
		}
	}
	d := diag.Diagnostic{Severity: diag.SevError, Code: diag.SemaError, Primary: span, Message: message,
		Help: []diag.Note{{Span: span, Msg: "return a reference from the marked inputs, or mark every input that the result can borrow from"}}}
	for _, marker := range markers {
		d.Notes = append(d.Notes, diag.Note{Span: marker.Span, Msg: "this marker limits the possible input sources of returned references"})
	}
	b.analyzer.report.Diagnostics = append(b.analyzer.report.Diagnostics, d)
}

func (a *returnOriginAnalyzer) checkGenericPromise(fn *returnOriginFunction, view *returnOriginSignature, use ConcreteInstantiationUse) string {
	body := &returnOriginBody{analyzer: a, function: fn}
	allowed, valid := body.declaredFunctionSources(fn, fn.item.NameSpan, view)
	if !valid {
		return "generic use has an unresolved original return-source promise"
	}
	if !fn.item.Body.IsValid() {
		body.opaqueReturnSources(fn, fn.info, allowed, true, fn.item.NameSpan, view)
		if returnOriginTypeShape(fn.unit.Sema.TypeInterner, view.result, nil) == returnOriginRefFree &&
			!returnOriginCallHasUnprovedEffects(fn.unit.Sema.TypeInterner, view.effects) {
			return ""
		}
		return "generic opaque use requires its type-dependent effect transfer"
	}
	a.useRequirements(fn, use)
	value := a.summaries[fn.key].value.clone()
	for _, root := range value.roots {
		if root.kind != returnOriginParam || root.expired || int64(root.param) >= int64(len(view.params)) {
			return "generic result contains an unproved source"
		}
	}
	shape := returnOriginTypeShape(fn.unit.Sema.TypeInterner, view.result, nil)
	if view.binding != nil {
		shape = view.binding.shape(fn.info.Result)
	}
	if shape == returnOriginShapeUnknown {
		return "generic result requires concrete borrowed-content facts"
	}
	if shape != returnOriginRefFree && !fn.info.ReturnSources().IsAllInputs() {
		value.roots = slices.DeleteFunc(value.roots, func(root returnOrigin) bool {
			if view.binding != nil {
				return view.binding.shape(fn.info.Params[root.param]) == returnOriginRefFree
			}
			return returnOriginTypeShape(fn.unit.Sema.TypeInterner, view.params[root.param], nil) == returnOriginRefFree
		})
		body.checkDeclaredReturn(value, allowed, use.Site)
	}
	return ""
}
