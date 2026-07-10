package buildpipeline

import (
	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/sema"
	"surge/internal/source"
)

const onCrossingUnavailableMsg = "`on` placement crossing cannot be executed: no available backend supports cross-shard transport"

// addOnCrossingBackendErrors guards accepted `on dst { ... }` placement
// crossings that reach an executable backend. Until a backend explicitly records
// crossing transport support, accepted crossing code reports FUT7014
// deterministically rather than crashing or silently dropping the crossing.
func addOnCrossingBackendErrors(req *CompileRequest, diagRes *driver.DiagnoseResult) {
	if req == nil || diagRes == nil || diagRes.Bag == nil || diagRes.Builder == nil {
		return
	}
	spans := collectOnCrossingSpans(req, diagRes.Sema)
	for _, mod := range diagRes.DependencyAnalyses() {
		for _, sr := range mod.Sema {
			spans = append(spans, collectOnCrossingSpans(req, sr)...)
		}
	}
	for _, sp := range dedupeSpans(spans) {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutOnBackendUnavailable,
			Message:  onCrossingUnavailableMsg,
			Primary:  sp,
		})
	}
}

// collectOnCrossingSpans returns the spans of sema-accepted `on` crossing
// records whose form is not backend-supported.
func collectOnCrossingSpans(req *CompileRequest, semaRes *sema.Result) []source.Span {
	if req == nil || semaRes == nil {
		return nil
	}
	var spans []source.Span
	seen := make(map[source.Span]struct{})
	for idx := range semaRes.CrossingLowering {
		info := &semaRes.CrossingLowering[idx]
		switch info.Kind {
		case sema.CrossingLoweringOnPlacement, sema.CrossingLoweringOnFarHandle:
		default:
			continue
		}
		backendBlocked := crossingBackendGuardAppliesForRequest(req, info.Kind)
		if !backendBlocked && crossingRecordExecutable(semaRes, info) {
			continue
		}
		if _, dup := seen[info.Span]; dup {
			continue
		}
		seen[info.Span] = struct{}{}
		spans = append(spans, info.Span)
	}
	return spans
}
