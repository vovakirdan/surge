package sema

import (
	"slices"

	"surge/internal/source"
)

// Body inference governs the top-level actual result. Nested callable slots
// describe promises made to a caller, so their original declared types apply.
func (b *returnOriginBody) checkCallableNestedPromises(actual returnOriginValue, expected returnOriginCallable, span source.Span) (bool, bool) {
	if expected.contract == nil {
		b.pending(span, "callable destination lost its original nested promises")
		return false, false
	}
	valid, mismatch := true, false
	for _, alternative := range actual.callables {
		contract := alternative.contract
		if alternative.bodyKey != "" {
			fn := b.callableFunction(alternative)
			if fn != nil {
				contract = b.functionCallableType(fn, span)
			}
		}
		if contract == nil {
			b.pending(span, "callable source lost its original nested promises")
			valid = false
			continue
		}
		complete, compatible := b.callableNestedRelation(contract, expected.contract, span)
		valid = valid && complete
		mismatch = mismatch || !compatible
	}
	return valid, mismatch
}

func (b *returnOriginBody) callablePromiseRelation(actual, expected *returnOriginCallableType, span source.Span) (bool, bool) {
	if actual == nil || expected == nil {
		if actual == expected {
			return true, true // Neither ordinary type carries a callable promise.
		}
		b.pending(span, "callable conversion has inconsistent original function-type structure")
		return false, false
	}
	complete, compatible := b.callableNestedRelation(actual, expected, span)
	for _, slot := range actual.slots {
		compatible = compatible && slices.Contains(expected.slots, slot)
	}
	return complete, compatible
}

func (b *returnOriginBody) callableNestedRelation(actual, expected *returnOriginCallableType, span source.Span) (bool, bool) {
	if len(actual.params) != len(expected.params) {
		b.pending(span, "callable conversion has inconsistent original parameter arity")
		return false, false
	}
	valid, compatible := true, true
	for i := range actual.params {
		// The actual consumer must accept every callable the expected consumer
		// promises to accept: reverse only this new source relation.
		complete, fits := b.callablePromiseRelation(expected.params[i], actual.params[i], span)
		valid, compatible = valid && complete, compatible && fits
	}
	complete, fits := b.callablePromiseRelation(actual.result, expected.result, span)
	return valid && complete, compatible && fits
}
