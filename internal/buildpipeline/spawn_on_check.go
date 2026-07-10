package buildpipeline

import (
	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/sema"
	"surge/internal/source"
)

const (
	spawnOnUnavailableMsg       = "`spawn on` remote spawn cannot be executed: no available backend supports cross-shard transport"
	farTaskAwaitUnavailableMsg  = "`far Task<T>.await()` cannot be executed: no available backend supports remote task transport"
	farTaskCancelUnavailableMsg = "`far Task<T>.cancel()` cannot be executed: no available backend supports remote task transport"
)

// addSpawnOnBackendErrors guards accepted `spawn on dst { ... }` remote spawns
// and `far Task<T>` await/cancel operations that reach an executable backend.
// Until a backend explicitly records remote transport support, accepted remote
// spawn/task lifecycle code reports FUT7015/FUT7016/FUT7017 deterministically.
func addSpawnOnBackendErrors(req *CompileRequest, diagRes *driver.DiagnoseResult) {
	if req == nil || diagRes == nil || diagRes.Bag == nil || diagRes.Builder == nil {
		return
	}
	modules := diagRes.DependencyAnalyses()

	spawnSpans := collectCrossingSpans(req, diagRes.Sema, sema.CrossingLoweringSpawnOn)
	awaitSpans := collectCrossingSpans(req, diagRes.Sema, sema.CrossingLoweringFarTaskAwait)
	cancelSpans := collectCrossingSpans(req, diagRes.Sema, sema.CrossingLoweringFarTaskCancel)
	for _, mod := range modules {
		for _, sr := range mod.Sema {
			spawnSpans = append(spawnSpans, collectCrossingSpans(req, sr, sema.CrossingLoweringSpawnOn)...)
			awaitSpans = append(awaitSpans, collectCrossingSpans(req, sr, sema.CrossingLoweringFarTaskAwait)...)
			cancelSpans = append(cancelSpans, collectCrossingSpans(req, sr, sema.CrossingLoweringFarTaskCancel)...)
		}
	}
	for _, sp := range dedupeSpans(spawnSpans) {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutSpawnOnBackendUnavailable,
			Message:  spawnOnUnavailableMsg,
			Primary:  sp,
		})
	}
	for _, sp := range dedupeSpans(awaitSpans) {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutFarTaskAwaitBackendUnavailable,
			Message:  farTaskAwaitUnavailableMsg,
			Primary:  sp,
		})
	}
	for _, sp := range dedupeSpans(cancelSpans) {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutFarTaskCancelBackendUnavailable,
			Message:  farTaskCancelUnavailableMsg,
			Primary:  sp,
		})
	}
}

// collectCrossingSpans returns sema-accepted crossing spans for a form when the
// current backend has no capability for that form.
func collectCrossingSpans(req *CompileRequest, semaRes *sema.Result, form sema.CrossingLoweringKind) []source.Span {
	if req == nil || semaRes == nil || req.Backend == "" {
		return nil
	}
	backendBlocked := crossingBackendGuardAppliesForRequest(req, form)
	var spans []source.Span
	seen := make(map[source.Span]struct{})
	for idx := range semaRes.CrossingLowering {
		info := &semaRes.CrossingLowering[idx]
		if info.Kind != form {
			continue
		}
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

// dedupeSpans returns the input spans with duplicates removed, preserving order,
// so a construct recorded once per occurrence is guarded exactly once.
func dedupeSpans(in []source.Span) []source.Span {
	if len(in) == 0 {
		return nil
	}
	var out []source.Span
	seen := make(map[source.Span]struct{}, len(in))
	for _, sp := range in {
		if _, dup := seen[sp]; dup {
			continue
		}
		seen[sp] = struct{}{}
		out = append(out, sp)
	}
	return out
}
