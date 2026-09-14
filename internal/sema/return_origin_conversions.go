package sema

import (
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// This uses the already typed destination, but proves the actual expression's
// sources before retaining it. No interner entry or checker type is narrowed.
func (b *returnOriginBody) bindCallable(value returnOriginValue, binding symbols.SymbolID, annotation ast.TypeID, expr ast.ExprID, previous returnOriginValue, assignment bool) returnOriginValue {
	if !value.normal {
		return value
	}
	u := b.function.unit
	typ := u.Sema.BindingTypes[binding]
	if returnOriginFnInfo(u.Sema.TypeInterner, typ) == nil && len(value.callables) == 0 {
		return value
	}
	span := u.Builder.Exprs.Get(expr).Span
	if b.callableType(typ, span) == nil {
		return value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	}
	if _, converted := u.Sema.ImplicitConversions[expr]; converted {
		b.pending(span, "callable binding conversion needs its selected operation contract")
		return value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	}
	if !annotation.IsValid() && !assignment {
		return value // An inferred copy preserves its actual alternatives.
	}
	var expected returnOriginCallable
	var valid bool
	fixed := annotation.IsValid()
	if fixed {
		expected, valid = b.declaredCallable(typ, annotation, span)
	} else {
		for _, old := range previous.callables {
			fixed = fixed || old.bodyKey == ""
		}
		expected, valid = b.assignmentCallablePromise(typ, previous, u.Symbols.Table.Symbols.Get(binding).Span)
	}
	if !valid || !b.checkCallableDestination(value, expected, span) {
		return value.join(returnOriginValueOf(returnOrigin{kind: returnOriginUnknown}))
	}
	if fixed {
		value = value.clone()
		value.callables = []returnOriginCallable{expected}
	}
	return value
}

func (b *returnOriginBody) assignmentCallablePromise(typ types.TypeID, previous returnOriginValue, span source.Span) (returnOriginCallable, bool) {
	if b.callableType(typ, span) == nil {
		return returnOriginCallable{}, false
	}
	for _, old := range previous.callables {
		if old.typ != typ {
			continue
		}
		if old.bodyKey == "" {
			return cloneReturnOriginCallables([]returnOriginCallable{old})[0], true
		}
		if fn := b.callableFunction(old); fn != nil {
			contract := b.functionCallableType(fn, span)
			if contract != nil {
				return returnOriginCallable{typ: typ, slots: slices.Clone(contract.slots), promise: span, contract: contract}, true
			}
		}
	}
	b.pending(span, "assigned callable lost its original destination promise")
	return returnOriginCallable{}, false
}

// A provisional recursive bottom remains private. Only the final collection
// walk emits diagnostics, after every actual body summary has stabilized.
func (b *returnOriginBody) checkCallableDestination(actual returnOriginValue, expected returnOriginCallable, span source.Span) bool {
	possible := b.callableValueSources(actual, span)
	valid := true
	mismatch := false
	for _, root := range possible.roots {
		if root.kind != returnOriginParam || root.expired {
			b.pending(span, "callable conversion has an unresolved actual source")
			valid = false
		} else if !slices.Contains(expected.slots, root.param) {
			mismatch = true
		}
	}
	nestedValid, nestedMismatch := b.checkCallableNestedPromises(actual, expected, span)
	valid, mismatch = valid && nestedValid, mismatch || nestedMismatch
	if valid && mismatch && b.analyzer.collect {
		const message = "callable return sources are not permitted by the destination promise"
		for _, old := range b.analyzer.report.Diagnostics {
			if old.Primary == span && old.Message == message {
				return true
			}
		}
		b.analyzer.report.Diagnostics = append(b.analyzer.report.Diagnostics, diag.Diagnostic{
			Code: diag.SemaError, Severity: diag.SevError, Primary: span, Message: message,
			Notes: []diag.Note{{Span: expected.promise, Msg: "the destination permits only the sources in this promise"}},
			Help:  []diag.Note{{Span: span, Msg: "use a compatible callable or widen the destination promise"}},
		})
	}
	return valid
}
