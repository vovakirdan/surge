package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
)

func (tc *typeChecker) crossingBodyOwnsCurrentExpression() bool {
	if tc == nil || len(tc.crossingBodyAsyncDepth) == 0 {
		return false
	}
	return tc.asyncBlockDepth == tc.crossingBodyAsyncDepth[len(tc.crossingBodyAsyncDepth)-1]
}

func (tc *typeChecker) refuseCrossingBodySuspension(span source.Span, operation string) {
	if !tc.crossingBodyOwnsCurrentExpression() {
		return
	}
	if len(tc.onCrossingStack) > 0 && !tc.onCrossingStack[len(tc.onCrossingStack)-1].isFar {
		return
	}
	form := "on"
	if len(tc.onCrossingStack) > 0 && tc.onCrossingStack[len(tc.onCrossingStack)-1].isSpawn {
		form = "spawn on"
	}
	tc.report(diag.FutCrossingBodySuspendUnsupported, span,
		"`%s` body cannot suspend at %s until crossing bodies have a transported state-machine frame; move this suspension into an async function outside the crossing body",
		form, operation)
}
