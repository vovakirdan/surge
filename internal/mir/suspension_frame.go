package mir

import (
	"strings"

	"surge/internal/types"
)

// Suspension frames: the compiler-synthesized structs that hold a paused
// computation.
//
// Four kinds exist — an async state machine's frame, the payload union it
// resumes with, a `spawn on` capture set and a blocking body's capture set —
// and none of them is an ordinary value. Each outlives the frame that built it
// by construction: its address is handed to the runtime when the computation
// suspends and read back when the computation resumes, so it cannot live in a
// caller's slot the way a struct the user wrote does.
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
var suspensionFramePrefixes = [...]string{
	asyncStateTypePrefix,
	asyncPayloadTypePrefix,
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
