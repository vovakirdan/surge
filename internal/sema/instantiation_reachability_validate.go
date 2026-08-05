package sema

import (
	"fmt"
	"slices"
	"sort"
)

func validateDeferredCallableEdge(edge *DeferredCallableEdge) error {
	if edge == nil || edge.UseID == "" || !edge.Caller.IsValid() || edge.Receiver == 0 || edge.ExpectedResult == 0 || edge.Method == "" {
		return fmt.Errorf("deferred callable edge is incomplete")
	}
	dummy := InstantiationEdge{
		Caller: edge.Caller, CallerTemplateArity: edge.CallerTemplateArity, CallerBindings: edge.CallerBindings,
	}
	if err := validateInstantiationBindings(&dummy); err != nil {
		return fmt.Errorf("deferred callable %s: %w", edge.UseID, err)
	}
	return nil
}

func sortResolvedDeferredCalls(calls []ResolvedDeferredCall) {
	sort.SliceStable(calls, func(i, j int) bool {
		left, right := &calls[i], &calls[j]
		if cmp := compareInstanceKey(left.Caller, right.Caller); cmp != 0 {
			return cmp < 0
		}
		if left.UseID != right.UseID {
			return left.UseID < right.UseID
		}
		if left.CalleeKey != right.CalleeKey {
			return left.CalleeKey < right.CalleeKey
		}
		return left.Outcome < right.Outcome
	})
}

func compactResolvedDeferredCalls(calls []ResolvedDeferredCall) ([]ResolvedDeferredCall, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	out := make([]ResolvedDeferredCall, 0, len(calls))
	for i := range calls {
		call := &calls[i]
		if len(out) > 0 {
			previous := &out[len(out)-1]
			if previous.Caller == call.Caller && previous.UseID == call.UseID {
				if !resolvedDeferredCallsEqual(previous, call) {
					return nil, fmt.Errorf("deferred callable %s in %s resolved inconsistently to %s and %s", call.UseID, call.Caller.TemplateKey, previous.CalleeKey, call.CalleeKey)
				}
				continue
			}
		}
		out = append(out, cloneResolvedDeferredCall(call))
	}
	return out, nil
}

func resolvedDeferredCallsEqual(left, right *ResolvedDeferredCall) bool {
	return left.UseID == right.UseID && left.Caller == right.Caller && left.CallerTemplate == right.CallerTemplate &&
		left.Kind == right.Kind && left.Outcome == right.Outcome && left.Callee == right.Callee && left.CalleeKey == right.CalleeKey &&
		left.CalleeResultType == right.CalleeResultType && left.Receiver == right.Receiver && left.ExpectedResult == right.ExpectedResult &&
		left.StaticReceiver == right.StaticReceiver && left.Site == right.Site &&
		left.SourceKey == right.SourceKey && left.Reason == right.Reason &&
		slices.Equal(left.CallerTemplateArgs, right.CallerTemplateArgs) &&
		slices.Equal(left.CalleeTemplateArgs, right.CalleeTemplateArgs) && slices.Equal(left.CalleeParamTypes, right.CalleeParamTypes) &&
		slices.Equal(left.Args, right.Args)
}
