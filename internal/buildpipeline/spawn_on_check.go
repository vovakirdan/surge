package buildpipeline

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/driver"
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
	if !crossingBackendGuardApplies(req.Backend) {
		return
	}

	modules := diagRes.DependencyAnalyses()

	spawnSpans := collectSpawnOnSpans(diagRes.Builder)
	for _, mod := range modules {
		spawnSpans = append(spawnSpans, collectSpawnOnSpans(mod.Builder)...)
	}
	for _, sp := range spawnSpans {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutSpawnOnBackendUnavailable,
			Message:  spawnOnUnavailableMsg,
			Primary:  sp,
		})
	}

	var awaitSpans, cancelSpans []source.Span
	if diagRes.Sema != nil {
		awaitSpans = append(awaitSpans, diagRes.Sema.FarTaskAwaitSpans...)
		cancelSpans = append(cancelSpans, diagRes.Sema.FarTaskCancelSpans...)
	}
	for _, mod := range modules {
		for _, sr := range mod.Sema {
			awaitSpans = append(awaitSpans, sr.FarTaskAwaitSpans...)
			cancelSpans = append(cancelSpans, sr.FarTaskCancelSpans...)
		}
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

// collectSpawnOnSpans returns the span of every `spawn on` remote-spawn
// expression in the module's AST (ExprOn nodes with the Spawn flag), deduped by
// span so each construct is guarded exactly once.
func collectSpawnOnSpans(builder *ast.Builder) []source.Span {
	if builder == nil || builder.Exprs == nil || builder.Exprs.Arena == nil {
		return nil
	}
	var spans []source.Span
	seen := make(map[source.Span]struct{})
	count := builder.Exprs.Arena.Len()
	for i := uint32(1); i <= count; i++ {
		id := ast.ExprID(i)
		expr := builder.Exprs.Get(id)
		if expr == nil || expr.Kind != ast.ExprOn {
			continue
		}
		data, ok := builder.Exprs.On(id)
		if !ok || data == nil || !data.Spawn {
			continue
		}
		if _, dup := seen[expr.Span]; dup {
			continue
		}
		seen[expr.Span] = struct{}{}
		spans = append(spans, expr.Span)
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
