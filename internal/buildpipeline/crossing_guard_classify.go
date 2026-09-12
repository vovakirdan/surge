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
	case sema.CrossingLoweringChannelCreate:
		// The same predicate the guard asks (crossing_transport.go), so the
		// refusal and the diagnostic cannot disagree on a shape: an element
		// whose counted blocks live in storage the sender keeps and no walk
		// reaches -- a map's table, a nested channel's ring -- is refused here,
		// not left to fail later without a code. An array element is not one:
		// its buffer is walked element by element where the send relinquishes
		// it.
		if semaRes.CountedBlockStaysShared(info.PayloadType) {
			return crossingGuardFinding{
				Code: diag.FutCrossingPayloadNotShippable,
				Span: info.Span,
				Message: semaRes.CountedBlockRefusalMessage(strings, info.PayloadType,
					fmt.Sprintf("a remote channel cannot carry `%s` yet", label(info.PayloadType)),
					!semaRes.DynamicArrayStaysUnchecked(info.PayloadType)),
			}, true
		}
		// The array half of the same stop: an element whose dynamic array sits
		// behind a handle is put in the ring without the runtime ever being
		// shown its header, so a view of the sender's buffer would arrive on
		// the receiving shard as an ordinary array.
		if semaRes.DynamicArrayStaysUnchecked(info.PayloadType) {
			return crossingGuardFinding{
				Code: diag.FutCrossingPayloadNotShippable,
				Span: info.Span,
				Message: arrayBehindHandleCrossingMessage(
					fmt.Sprintf("a remote channel cannot carry `%s` yet", label(info.PayloadType)),
					"the receiving shard takes the value"),
			}, true
		}
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
		if !semaRes.TriviallyTransportableBits(info.PayloadType) {
			if msg, ok := crossingPayloadRefusalMessage(semaRes, strings, info.PayloadType, "the crossing result"); ok {
				return crossingGuardFinding{Code: diag.FutCrossingPayloadNotShippable, Span: info.Span, Message: msg}, true
			}
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
		if !semaRes.TriviallyTransportableBits(info.PayloadType) {
			if msg, ok := crossingPayloadRefusalMessage(semaRes, strings, info.PayloadType, "the awaited result"); ok {
				return crossingGuardFinding{Code: diag.FutCrossingPayloadNotShippable, Span: info.Span, Message: msg}, true
			}
			return crossingGuardFinding{
				Code: diag.FutCrossingPayloadNotShippable,
				Span: info.Span,
				Message: fmt.Sprintf(
					"the awaited result `%s` is not plain-copy data%s and cannot cross "+
						"back to the caller yet; have the `spawn on` body `ret` plain-copy data "+
						"(unwrap or derive it inside the body)", label(info.PayloadType),
					nonCopyDetail(semaRes, strings, info.PayloadType)),
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

// refCountedCrossingMessage names the real reason a result carrying
// arbitrary-precision values cannot ride the reply. Saying "not plain-copy
// data" would be wrong and confusing here: a `Channel<float>` is Copy and is
// refused for its representation alone — the values are references into
// counted blocks whose count is not atomic, they live in a ring the producer
// keeps, and no walk makes them private before the asker takes the value. A
// `Map<K, float>` is not Copy, but its table is the same kind of storage, and
// that is the reason a reader can act on, so it is named first. A bare
// `float` and a `@copy` composite of them ride, un-shared by the producer's
// `ret`. A fixed array, a tuple and a dynamic array are refused as not Copy
// at all, and a `@copy` struct cannot hold one either (its fields must be
// Copy by the interner's rule or `@copy` themselves). The dynamic array's
// buffer IS walked where an owned move relinquishes it — a capture, a channel
// element, a `blocking` body's `ret`, which crosses no shard — but a crossing
// reply takes only plain-copy data, so the walk never serves one.
func refCountedCrossingMessage(semaRes *sema.Result, strings *source.Interner, t types.TypeID, subject string) (string, bool) {
	if semaRes == nil || semaRes.TypeInterner == nil || !semaRes.CountedBlockStaysShared(t) {
		return "", false
	}
	return semaRes.CountedBlockRefusalMessage(strings, t,
		fmt.Sprintf("%s `%s` cannot cross a shard boundary yet", subject, types.Label(semaRes.TypeInterner, t)),
		semaRes.IsCopyType(t) && !semaRes.DynamicArrayStaysUnchecked(t)), true
}

// crossingPayloadRefusalMessage names the real reason a payload cannot ride the
// reply, when there is one. Both named reasons outrank "not plain-copy data",
// and for the array one that phrase would be simply false: a `Channel<int[]>`
// IS plain-copy data -- one handle word -- and what keeps it here is the ring
// behind the word.
func crossingPayloadRefusalMessage(semaRes *sema.Result, strings *source.Interner, t types.TypeID, subject string) (string, bool) {
	if msg, ok := refCountedCrossingMessage(semaRes, strings, t, subject); ok {
		return msg, true
	}
	if semaRes == nil || semaRes.TypeInterner == nil || !semaRes.DynamicArrayStaysUnchecked(t) {
		return "", false
	}
	return arrayBehindHandleCrossingMessage(
		fmt.Sprintf("%s `%s` cannot cross a shard boundary yet",
			subject, types.Label(semaRes.TypeInterner, t)),
		"the asker takes the value"), true
}

// arrayBehindHandleCrossingMessage is the one sentence this package says when a
// crossing payload reaches a dynamic array only through storage the
// relinquishing walk cannot step. subject is the clause naming the payload and
// what it cannot do; moment is the point by which the runtime would have had to
// see the array's header.
//
// It is deliberately the same sentence sema's capture gates say
// (crossingArrayBehindHandleMessage), because it is the same refusal for the
// same reason arriving by another route, and a reader who meets it twice should
// not have to work out whether the two mean one thing.
func arrayBehindHandleCrossingMessage(subject, moment string) string {
	return fmt.Sprintf(
		"%s: it holds a dynamic array in storage this shard keeps (a map's table, a "+
			"channel's ring, a task's result slot), so the runtime is never shown that array's "+
			"header before %s and cannot tell a view into another array's buffer from an array "+
			"of its own -- whoever takes it out on the far side would write through the view "+
			"into the buffer this shard is still reading. Take the array out of the container "+
			"and cross it on its own, or in a field of the value that crosses",
		subject, moment)
}
