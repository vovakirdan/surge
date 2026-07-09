package buildpipeline

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/driver"
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
	if !crossingBackendGuardApplies(req.Backend) {
		return
	}
	spans := collectOnCrossingSpans(diagRes.Builder)
	for _, mod := range diagRes.DependencyAnalyses() {
		spans = append(spans, collectOnCrossingSpans(mod.Builder)...)
	}
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
		id := ast.ExprID(i)
		expr := builder.Exprs.Get(id)
		if expr == nil || expr.Kind != ast.ExprOn {
			continue
		}
		// `spawn on dst { ... }` shares the ExprOn node with the Spawn flag set;
		// it is guarded separately (FUT7015), so skip it here.
		if data, ok := builder.Exprs.On(id); ok && data != nil && data.Spawn {
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
