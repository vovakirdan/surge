package buildpipeline

import "surge/internal/sema"

// crossingBackendGuardApplies reports whether compile-time crossing surfaces
// must be stopped before lowering for this backend/form selection. An empty
// backend means no executable backend was selected; every non-empty backend/form
// pair remains blocked until it is explicitly recorded as transport-capable.
func crossingBackendGuardApplies(backend Backend, form sema.CrossingLoweringKind) bool {
	if backend == "" {
		return false
	}
	return !backendSupportsCrossingForm(backend, form)
}

func crossingBackendGuardAppliesForRequest(req *CompileRequest, form sema.CrossingLoweringKind) bool {
	if req == nil {
		return false
	}
	if req.CrossingFormsForTest != nil && req.CrossingFormsForTest[form] {
		return false
	}
	return crossingBackendGuardApplies(req.Backend, form)
}

func backendSupportsCrossingForm(backend Backend, form sema.CrossingLoweringKind) bool {
	if backend != BackendLLVM {
		return false
	}
	switch form {
	case sema.CrossingLoweringSpawnOn,
		sema.CrossingLoweringOnPlacement,
		sema.CrossingLoweringOnFarHandle,
		sema.CrossingLoweringFarTaskAwait,
		sema.CrossingLoweringFarTaskCancel,
		sema.CrossingLoweringChannelCreate:
		return true
	default:
		return false
	}
}

func crossingFormsForRequest(req *CompileRequest) map[sema.CrossingLoweringKind]bool {
	if req == nil {
		return nil
	}
	forms := make(map[sema.CrossingLoweringKind]bool, len(req.CrossingFormsForTest)+3)
	for form, enabled := range req.CrossingFormsForTest {
		if enabled {
			forms[form] = true
		}
	}
	for _, form := range []sema.CrossingLoweringKind{
		sema.CrossingLoweringSpawnOn,
		sema.CrossingLoweringOnPlacement,
		sema.CrossingLoweringOnFarHandle,
		sema.CrossingLoweringFarTaskAwait,
		sema.CrossingLoweringFarTaskCancel,
		sema.CrossingLoweringChannelCreate,
	} {
		if backendSupportsCrossingForm(req.Backend, form) {
			forms[form] = true
		}
	}
	if len(forms) == 0 {
		return nil
	}
	return forms
}

// crossingRecordExecutable applies the narrower representation gate
// after a backend advertises the form. This first vertical is suspend-only and
// may carry only plain-data/copyable payloads; heap-owned shard-movable values
// stay guarded until remote-free ownership exists.
func crossingRecordExecutable(res *sema.Result, info *sema.CrossingLoweringInfo) bool {
	if res == nil || info == nil || !info.SuspendCapable {
		return false
	}
	switch info.Kind {
	case sema.CrossingLoweringSpawnOn, sema.CrossingLoweringOnPlacement,
		sema.CrossingLoweringOnFarHandle:
		for _, capture := range info.Captures {
			if capture.Verdict == sema.CrossingCaptureOwnedShardMovable {
				return false
			}
			if capture.Verdict == sema.CrossingCaptureFarHandle &&
				res.IsDirectFarTaskType(capture.Type) {
				return false
			}
		}
		return res.IsCopyType(info.PayloadType)
	case sema.CrossingLoweringFarTaskAwait:
		return res.IsCopyType(info.PayloadType)
	case sema.CrossingLoweringFarTaskCancel:
		return true
	case sema.CrossingLoweringChannelShare:
		// Only the sibling token rides the reply — plain bits by
		// construction; the async context is the sole shape requirement.
		return true
	case sema.CrossingLoweringChannelCreate:
		// The element type is the channel's payload boundary: a channel whose
		// values cannot cross shards must not be mintable remotely.
		return res.IsCopyType(info.PayloadType)
	default:
		return false
	}
}
