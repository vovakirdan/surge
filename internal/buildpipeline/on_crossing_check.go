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
	mainStrings := diagRes.Builder.StringsInterner
	var findings []crossingGuardFinding
	for _, form := range []sema.CrossingLoweringKind{
		sema.CrossingLoweringOnPlacement,
		sema.CrossingLoweringOnFarHandle,
	} {
		findings = append(findings, collectCrossingGuardFindings(
			req, diagRes.Sema, mainStrings, form,
			diag.FutOnBackendUnavailable, onCrossingUnavailableMsg)...)
		for _, mod := range diagRes.DependencyAnalyses() {
			var modStrings *source.Interner
			if mod.Builder != nil {
				modStrings = mod.Builder.StringsInterner
			}
			for _, sr := range mod.Sema {
				findings = append(findings, collectCrossingGuardFindings(
					req, sr, modStrings, form,
					diag.FutOnBackendUnavailable, onCrossingUnavailableMsg)...)
			}
		}
	}
	for _, finding := range dedupeCrossingGuardFindings(findings) {
		diagRes.Bag.Add(&diag.Diagnostic{
			Severity: diag.SevError,
			Code:     finding.Code,
			Message:  finding.Message,
			Primary:  finding.Span,
		})
	}
}
