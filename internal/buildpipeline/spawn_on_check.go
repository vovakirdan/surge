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
	channelOnUnavailableMsg     = "`channel_on(...)` cannot be executed: this backend has no remote channel transport"
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

	forms := []struct {
		form    sema.CrossingLoweringKind
		generic diag.Code
		message string
	}{
		{sema.CrossingLoweringSpawnOn, diag.FutSpawnOnBackendUnavailable, spawnOnUnavailableMsg},
		{sema.CrossingLoweringFarTaskAwait, diag.FutFarTaskAwaitBackendUnavailable, farTaskAwaitUnavailableMsg},
		{sema.CrossingLoweringFarTaskCancel, diag.FutFarTaskCancelBackendUnavailable, farTaskCancelUnavailableMsg},
		{sema.CrossingLoweringChannelCreate, diag.FutChannelOnBackendUnavailable, channelOnUnavailableMsg},
	}
	mainStrings := diagRes.Builder.StringsInterner
	for _, entry := range forms {
		findings := collectCrossingGuardFindings(
			req, diagRes.Sema, mainStrings, entry.form, entry.generic, entry.message)
		for _, mod := range modules {
			var modStrings *source.Interner
			if mod.Builder != nil {
				modStrings = mod.Builder.StringsInterner
			}
			for _, sr := range mod.Sema {
				findings = append(findings, collectCrossingGuardFindings(
					req, sr, modStrings, entry.form, entry.generic, entry.message)...)
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
}
