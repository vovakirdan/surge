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
		sema.CrossingLoweringChannelCreate,
		sema.CrossingLoweringChannelShare,
		sema.CrossingLoweringChannelSelect:
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
		sema.CrossingLoweringChannelShare,
		sema.CrossingLoweringChannelSelect,
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
			// Owned @shard_movable captures ship since the migration
			// vertical: in shared memory the capture is a pointer in the
			// crossing state struct, and reclamation follows the language's
			// current memory model unchanged (see the migration epic's
			// drop-obligation record).
			if capture.Verdict == sema.CrossingCaptureFarHandle &&
				res.IsDirectFarTaskType(capture.Type) {
				return false
			}
		}
		return res.TriviallyTransportableBits(info.PayloadType)
	case sema.CrossingLoweringFarTaskAwait:
		return res.TriviallyTransportableBits(info.PayloadType)
	case sema.CrossingLoweringFarTaskCancel:
		return true
	case sema.CrossingLoweringChannelShare:
		// Only the sibling token rides the reply — plain bits by
		// construction; the async context is the sole shape requirement.
		return true
	case sema.CrossingLoweringChannelCreate:
		// The element type was the channel's payload boundary while the
		// runtime moved only raw bits (RV2-DEBT-059/062's investigation).
		// The buffer, the parked-receiver mailbox, and each remote-select
		// SEND arm now carry a payload_drop_fn_id (Task 8), so a non-Copy
		// element reclaims correctly; any element type may mint remotely.
		//
		// The exception is an element carrying an arbitrary-precision value.
		// It is Copy, so a send leaves the sender's binding alive while the
		// receiving shard takes the same word — one counted block, two shards,
		// and the count is not atomic. Refuse the channel at its creation
		// rather than at each send, so the diagnostic lands where the element
		// type was chosen.
		return !res.ContainsRefCountedScalar(info.PayloadType)
	case sema.CrossingLoweringChannelSelect:
		// The reply is the winner index (plain bits); the arms' send payloads
		// are plain-copy by channel construction. Async context is the sole
		// shape requirement.
		return true
	default:
		return false
	}
}
