package buildpipeline

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/source"
)

const (
	spawnOnUnavailableMsg       = "`spawn on` remote spawn cannot be lowered yet; the Phase 4 transport backend is unavailable"
	farTaskAwaitUnavailableMsg  = "`far Task<T>.await()` cannot be lowered yet; the Phase 4 transport backend is unavailable"
	farTaskCancelUnavailableMsg = "`far Task<T>.cancel()` cannot be lowered yet; the Phase 4 transport backend is unavailable"
)

// addSpawnOnBackendErrors guards accepted `spawn on dst { ... }` remote spawns
// and `far Task<T>` await/cancel operations that reach lowering. Epic 11 Block 3
// delivers the surface compile-only: no backend can execute remote spawn or
// remote task lifecycle until the Phase 4 transport epic, so any such construct
// that type-checks and reaches a backend is reported deterministically
// (FUT7015 / FUT7016 / FUT7017) rather than crashing or silently dropping it.
// This mirrors addOnCrossingBackendErrors (FUT7014).
func addSpawnOnBackendErrors(req *CompileRequest, diagRes *driver.DiagnoseResult) {
	if req == nil || diagRes == nil || diagRes.Bag == nil || diagRes.Builder == nil {
		return
	}
	// No current backend implements the remote-spawn transport.
	if req.Backend != BackendVM && req.Backend != BackendLLVM {
		return
	}

	for _, sp := range collectSpawnOnSpans(diagRes.Builder) {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutSpawnOnBackendUnavailable,
			Message:  spawnOnUnavailableMsg,
			Primary:  sp,
		})
	}

	if diagRes.Sema == nil {
		return
	}
	for _, sp := range dedupeSpans(diagRes.Sema.FarTaskAwaitSpans) {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutFarTaskAwaitBackendUnavailable,
			Message:  farTaskAwaitUnavailableMsg,
			Primary:  sp,
		})
	}
	for _, sp := range dedupeSpans(diagRes.Sema.FarTaskCancelSpans) {
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
