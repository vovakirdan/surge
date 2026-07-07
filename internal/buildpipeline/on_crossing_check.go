package buildpipeline

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/source"
)

const onCrossingUnavailableMsg = "`on` placement crossing cannot be lowered yet; the Phase 4 transport backend is unavailable"

// addOnCrossingBackendErrors guards accepted `on dst { ... }` placement
// crossings that reach lowering. Epic 11 delivers the surface compile-only:
// no backend can execute a crossing until the Phase 4 transport epic, so any
// crossing that type-checks and reaches a backend is reported deterministically
// (FUT7014) rather than crashing or silently dropping the crossing. This mirrors
// addBlockingVMErrors / FutBlockingNotSupported (7008).
func addOnCrossingBackendErrors(req *CompileRequest, diagRes *driver.DiagnoseResult) {
	if req == nil || diagRes == nil || diagRes.Bag == nil || diagRes.Builder == nil {
		return
	}
	// No current backend implements the crossing transport.
	if req.Backend != BackendVM && req.Backend != BackendLLVM {
		return
	}
	spans := collectOnCrossingSpans(diagRes.Builder)
	for _, sp := range spans {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     diag.FutOnBackendUnavailable,
			Message:  onCrossingUnavailableMsg,
			Primary:  sp,
		})
	}
}

// collectOnCrossingSpans returns the span of every `on` crossing expression in
// the module's AST.
func collectOnCrossingSpans(builder *ast.Builder) []source.Span {
	if builder == nil || builder.Exprs == nil || builder.Exprs.Arena == nil {
		return nil
	}
	var spans []source.Span
	// Arena IDs are 1-based (Get(0) == nil). Dedupe by span so a given crossing
	// is guarded exactly once even if the arena is visited more than once.
	seen := make(map[source.Span]struct{})
	count := builder.Exprs.Arena.Len()
	for i := uint32(1); i <= count; i++ {
		expr := builder.Exprs.Get(ast.ExprID(i))
		if expr == nil || expr.Kind != ast.ExprOn {
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
