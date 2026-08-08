package mir

import (
	"strings"

	"surge/internal/types"
)

// Suspension frames: the compiler-synthesized structs that hold a paused
// computation.
//
// Three kinds exist — an async state machine's frame, a `spawn on` capture set
// and a blocking body's capture set — and none of them is an ordinary value.
// Each outlives the frame that built it by construction: its address is handed
// to the runtime when the computation suspends and read back when the
// computation resumes, so it cannot live in a caller's slot the way a struct
// the user wrote does.
//
// The resume payload union is NOT one of them, and the difference is exactly
// the property above: it is a MEMBER of the async state frame, so the storage
// it occupies is storage the frame already owns. Giving it a lifetime of its
// own bought a second allocation per suspension and nothing else — the layout
// registry had been sizing the frame's payload field as the union's own bytes
// all along, so a frame that carried it by pointer left those bytes unwritten
// and reached for the allocator instead.
//
// The prefixes are named once, here, and used both by the builders that mint
// these types and by the backends that have to recognize them. Two lists would
// drift, and the way they would drift is silently: a frame the backends stopped
// recognizing would be given a value's representation and would then be read
// after the poll that built it returned.

const (
	asyncStateTypePrefix    = "__AsyncState$"
	asyncPayloadTypePrefix  = "__AsyncPayload$"
	spawnOnStateTypePrefix  = "__SpawnOnState$"
	blockingStateTypePrefix = "__BlockingState$"
)

// suspensionFramePrefixes is every synthesized frame name this compiler mints.
//
// asyncPayloadTypePrefix is deliberately absent: that union is a field of the
// async state frame, not a frame.
var suspensionFramePrefixes = [...]string{
	asyncStateTypePrefix,
	spawnOnStateTypePrefix,
	blockingStateTypePrefix,
}

// IsSuspensionFrameType reports whether a type is one of those frames.
//
// A backend asks so that it keeps giving these frames storage the runtime owns,
// rather than the inline storage an ordinary composite gets. They are not
// ordinary composites and the difference is a lifetime, not a layout: their
// fields still sit at the offsets `internal/layout` published, and a composite
// field inside one is still inline within it.
func IsSuspensionFrameType(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || typesIn.Strings == nil || id == types.NoTypeID {
		return false
	}
	name, ok := frameTypeName(typesIn, resolveAlias(typesIn, id))
	if !ok {
		return false
	}
	for _, prefix := range suspensionFramePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// LivesInInlineStorage reports whether values of a type occupy storage at the
// site that declares them, rather than naming storage somebody else owns.
//
// This is the ONE rule, and it exists because it has more than one consumer. The
// call contract classifies a parameter or a result from it; a backend spells the
// memory that holds a value from it. Those two answers become the two halves of
// one ABI — who allocates, who copies, which argument position carries what —
// and they are only an ABI while they agree.
//
// They stopped agreeing once before, and the shape of that failure is why the
// rule is stated here instead of at each consumer. The frame exemption lived in
// a backend and nowhere else, so the contract classified every `blocking { }`
// capture set as a by-value composite while the backend spelled it as the handle
// the runtime hands back. Neither side was wrong about a size or an offset; they
// were answering different questions. A rule with one consumer is a convention,
// and a convention is what drifts.
//
// The frame check comes FIRST, ahead of any question about size. A frame with no
// captures is zero bytes, and a zero-sized value composite is elided from the
// argument list entirely — so asking about size first would elide an argument
// that is really a live runtime handle.
func LivesInInlineStorage(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	// Resolved once, so the frame question and the composite question cannot be
	// asked about two different spellings of one type.
	resolved := resolveAliasAndOwn(typesIn, id)
	if IsSuspensionFrameType(typesIn, resolved) {
		return false
	}
	return typesIn.IsValueComposite(resolved)
}

// frameTypeName is the declared name of a struct or union type, if it has one.
func frameTypeName(typesIn *types.Interner, id types.TypeID) (string, bool) {
	if info, ok := typesIn.StructInfo(id); ok && info != nil {
		return typesIn.Strings.MustLookup(info.Name), true
	}
	if info, ok := typesIn.UnionInfo(id); ok && info != nil {
		return typesIn.Strings.MustLookup(info.Name), true
	}
	return "", false
}
