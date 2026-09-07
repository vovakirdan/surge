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
		// The exception is an element whose counted heap blocks the sender
		// cannot make private before the ring takes them. An element carrying
		// an arbitrary-precision value is Copy, so a send would leave the
		// sender's binding on the same block the receiving shard takes — one
		// counted block, two shards, and the count is not atomic. Every entry
		// into a remote channel's ring now hands over a PRIVATE reference: a
		// far-select SEND payload is un-shared in the relinquishing operand
		// (site 2), and an anchored body's `ch.send(own f)` gives the capture's
		// own reference away, one the caller made private when the capture
		// entered the state (site 1; sema holds the send to that shape). So a
		// bare `float`, a struct, a union, a fixed array of them cross, and so
		// does a dynamic array of them: the send's operand hands its buffer to
		// the runtime's element walk, which makes each element private in
		// place before the ring takes the array.
		//
		// What stays refused is what no walk can make private — a map's table,
		// a nested channel's ring — asked as the MOVE question through unions
		// (MayShareCountedBlock) and answered by the walk
		// (CountedBlockCanBeMadePrivate), the same pair the capture gate asks.
		// Refused at the channel's creation rather than at each send, so the
		// diagnostic lands where the element type was chosen.
		//
		// The array question is asked at the same stop and for the same
		// reason. An element that IS an array -- `int[]`, `float[]` -- is
		// walked wherever a relinquishing sink hands it over, and the runtime
		// refuses a view of it by name; an element that merely holds one behind
		// a handle, a `Map<int, int[]>` or a `Channel<int[]>`, hands the ring an
		// array no walk on either shard will ever look at.
		//
		// One send is outside that "wherever", and this gate does not cover it:
		// the anchored body's `ch.send`, which no walk may precede because the
		// body's prefix replays (validate_relinquish.go sinkIsSubject). What
		// closes it, where it is closed at all, is the SHAPE sema holds the
		// payload to: `own <captured binding>`, asked of a counted element and
		// now of a payload that names a captured dynamic array. A payload that
		// names no capture -- one built inside the block, or sliced out of a
		// `@shard_movable` capture's field -- still reaches the ring
		// unexamined (RV2-DEBT-349).
		return !res.CountedBlockStaysShared(info.PayloadType) &&
			!res.DynamicArrayStaysUnchecked(info.PayloadType)
	case sema.CrossingLoweringChannelSelect:
		// The reply is the winner index (plain bits); the arms' send payloads
		// are made private in the relinquishing operand. Async context is the
		// sole shape requirement.
		return true
	default:
		return false
	}
}
