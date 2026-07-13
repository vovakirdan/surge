package buildpipeline

import (
	"fmt"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// crossingGuardFinding is one guarded crossing with its named cause: the
// diagnostic points at the most actionable thing the user can change, and
// the generic backend-unavailable message survives only where the crossing
// shape is fine and the backend genuinely lacks the transport.
type crossingGuardFinding struct {
	Code    diag.Code
	Span    source.Span
	Message string
}

// classifyCrossingGuard returns the guard finding for one sema-accepted
// crossing record, or ok=false when the record is executable as-is. Cause
// order is deliberate: the sync-context and payload causes hold on every
// backend (the same program fails everywhere), so they outrank the
// backend-capability message.
func classifyCrossingGuard(
	req *CompileRequest,
	semaRes *sema.Result,
	strings *source.Interner,
	info *sema.CrossingLoweringInfo,
	genericCode diag.Code,
	genericMsg string,
) (crossingGuardFinding, bool) {
	if req == nil || semaRes == nil || info == nil || req.Backend == "" {
		return crossingGuardFinding{}, false
	}
	backendBlocked := crossingBackendGuardAppliesForRequest(req, info.Kind)
	if !backendBlocked && crossingRecordExecutable(semaRes, info) {
		return crossingGuardFinding{}, false
	}
	if !info.SuspendCapable {
		return crossingGuardFinding{
			Code: diag.FutCrossingSyncContext,
			Span: info.Span,
			Message: fmt.Sprintf(
				"%s suspends until its reply arrives, which needs an `async` context; "+
					"make the enclosing function `async`", crossingFormLabel(info.Kind)),
		}, true
	}
	if finding, ok := classifyCrossingPayload(semaRes, strings, info); ok {
		return finding, true
	}
	return crossingGuardFinding{Code: genericCode, Span: info.Span, Message: genericMsg}, true
}

// classifyCrossingPayload names the capture or payload that keeps an
// otherwise-executable crossing off the transport.
func classifyCrossingPayload(
	semaRes *sema.Result,
	strings *source.Interner,
	info *sema.CrossingLoweringInfo,
) (crossingGuardFinding, bool) {
	label := func(t types.TypeID) string { return types.Label(semaRes.TypeInterner, t) }
	switch info.Kind {
	case sema.CrossingLoweringSpawnOn, sema.CrossingLoweringOnPlacement,
		sema.CrossingLoweringOnFarHandle:
		for i := range info.Captures {
			capture := &info.Captures[i]
			if capture.Verdict == sema.CrossingCaptureFarHandle &&
				semaRes.IsDirectFarTaskType(capture.Type) {
				return crossingGuardFinding{
					Code: diag.FutCrossingPayloadNotShippable,
					Span: capture.Span,
					Message: fmt.Sprintf(
						"capture `%s` carries a `far Task` lease into the crossing; "+
							"await or cancel it from the task that holds it", capture.Name),
				}, true
			}
		}
		if !semaRes.IsCopyType(info.PayloadType) {
			hint := "return plain-copy data from the block"
			if info.Kind == sema.CrossingLoweringOnFarHandle {
				hint = "unwrap it inside the block before `ret` " +
					"(e.g. `let v = ch.recv(); ret compare v { ... };`)"
			}
			return crossingGuardFinding{
				Code: diag.FutCrossingPayloadNotShippable,
				Span: info.Span,
				Message: fmt.Sprintf(
					"the crossing result `%s` cannot ride the reply: it is not plain-copy "+
						"data%s; %s", label(info.PayloadType),
					nonCopyDetail(semaRes, strings, info.PayloadType), hint),
			}, true
		}
	case sema.CrossingLoweringFarTaskAwait:
		if !semaRes.IsCopyType(info.PayloadType) {
			return crossingGuardFinding{
				Code: diag.FutCrossingPayloadNotShippable,
				Span: info.Span,
				Message: fmt.Sprintf(
					"the awaited result `%s` is not plain-copy data%s and cannot cross "+
						"back to the caller yet", label(info.PayloadType),
					nonCopyDetail(semaRes, strings, info.PayloadType)),
			}, true
		}
	case sema.CrossingLoweringChannelCreate:
		if !semaRes.IsCopyType(info.PayloadType) {
			return crossingGuardFinding{
				Code: diag.FutCrossingPayloadNotShippable,
				Span: info.Span,
				Message: fmt.Sprintf(
					"channel element `%s` must be plain-copy data to cross shards%s; "+
						"a channel whose values cannot cross must stay local",
					label(info.PayloadType), nonCopyDetail(semaRes, strings, info.PayloadType)),
			}, true
		}
	}
	return crossingGuardFinding{}, false
}

// nonCopyDetail renders the exact offending field path when the payload is a
// struct whose component owns heap memory, e.g. " (field `meta.name` owns
// heap memory)".
func nonCopyDetail(semaRes *sema.Result, strings *source.Interner, t types.TypeID) string {
	path := semaRes.NonCopyCulpritPath(strings, t)
	if path == "" {
		return ""
	}
	return fmt.Sprintf(" (field `%s` owns heap memory)", path)
}

func crossingFormLabel(kind sema.CrossingLoweringKind) string {
	switch kind {
	case sema.CrossingLoweringOnPlacement:
		return "`on <placement>`"
	case sema.CrossingLoweringOnFarHandle:
		return "`on <far handle>`"
	case sema.CrossingLoweringSpawnOn:
		return "`spawn on`"
	case sema.CrossingLoweringFarTaskAwait:
		return "`far Task<T>.await()`"
	case sema.CrossingLoweringFarTaskCancel:
		return "`far Task<T>.cancel()`"
	case sema.CrossingLoweringChannelCreate:
		return "`channel_on(...)`"
	case sema.CrossingLoweringChannelShare:
		return "`share()`"
	case sema.CrossingLoweringChannelSelect:
		return "remote `select`"
	default:
		return "this crossing"
	}
}

// collectCrossingGuardFindings walks one module's crossing records for one
// form and returns its classified findings.
func collectCrossingGuardFindings(
	req *CompileRequest,
	semaRes *sema.Result,
	strings *source.Interner,
	form sema.CrossingLoweringKind,
	genericCode diag.Code,
	genericMsg string,
) []crossingGuardFinding {
	if semaRes == nil {
		return nil
	}
	var findings []crossingGuardFinding
	for idx := range semaRes.CrossingLowering {
		info := &semaRes.CrossingLowering[idx]
		if info.Kind != form {
			continue
		}
		if finding, ok := classifyCrossingGuard(req, semaRes, strings, info, genericCode, genericMsg); ok {
			findings = append(findings, finding)
		}
	}
	return findings
}

// dedupeCrossingGuardFindings drops repeated (code, span) findings while
// preserving order, mirroring the span dedupe the guards always had.
func dedupeCrossingGuardFindings(in []crossingGuardFinding) []crossingGuardFinding {
	if len(in) == 0 {
		return nil
	}
	type key struct {
		code diag.Code
		span source.Span
	}
	var out []crossingGuardFinding
	seen := make(map[key]struct{}, len(in))
	for _, finding := range in {
		k := key{code: finding.Code, span: finding.Span}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, finding)
	}
	return out
}
