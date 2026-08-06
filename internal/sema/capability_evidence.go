package sema

import (
	"fmt"
	"strings"

	"surge/internal/types"
)

// The reasons a capability verdict carries. They are stated once, here, so the
// same shape is described the same way on every axis that meets it, and so the
// classifier itself reads as the rules rather than as the prose.
const (
	unknownTypeReason = "this type is absent from the interner, so nothing can be said about it"

	deferredStructureReason = "the type still carries a generic parameter, so its members are not decided yet"
	copyCloneReason         = "the type is Copy, so duplicating it is the clone"
	deferredCloneReason     = "the receiver still carries a generic parameter, so no body can be selected yet"

	borrowOwnsNothingReason = "a borrow names storage it does not own, so there is nothing here to reclaim"
	farLeaseReason          = "a far handle holds a lease that release has to return"
	functionPointerReason   = "a function pointer names code, which owns nothing"
	refCountedScalarReason  = "an arbitrary-precision scalar is a reference into a counted heap block"
	handleOwnsStorageReason = "a handle-backed value owns the storage its handle names"
	placementIsPlainReason  = "Placement is a plain intrinsic value with no storage of its own"
	nothingToReclaimReason  = "nothing inside this value requires reclamation"

	componentDroppableReason = "it holds `%s`, which requires reclamation"

	borrowIsNotARootReason  = "a borrow is reached through its owner, and visiting it here would count it twice"
	farIsAnotherShardReason = "a far handle names storage in another shard, which this shard's visitor cannot reach"
	handleIsARootReason     = "a handle-backed value names runtime-managed storage a visitor has to enter"
	noRootsReason           = "this value holds no reference into runtime-managed storage"

	componentTraceableReason = "it holds `%s`, which names runtime-managed storage"

	builtinTravelsReason         = "a builtin scalar or string travels between shards as itself"
	farTravelsReason             = "a far handle moves between shards; the resource it names stays put"
	functionPointerTravelsReason = "a function pointer names code every shard in this process shares"
	tupleTravelsReason           = "every element may move between shards"
	arrayTravelsReason           = "every element may move between shards"
	markedMovableReason          = "the type is `@shard_movable` and every member may move with it"
	shardPinnedReason            = "the type is `@shard_pinned`, so it stays in the shard that holds it"
	noSendReason                 = "the type is `@nosend`, so it may not leave the shard that holds it"
	unmarkedReason               = "the type is not marked `@shard_movable`"
	borrowCannotCrossReason      = "a borrowed value cannot cross a shard boundary"

	shardComponentRefusedReason = "it holds `%s`, which may not move between shards"

	leaseHasOneHolderReason   = "a lease has one holder, so duplicating a far handle is not a defined crossing duplicate"
	opaqueResourceReason      = "an opaque runtime resource has no design-defined duplicate"
	stringDuplicatesReason    = "the crossing duplicates a string by design"
	containerDuplicatesReason = "the crossing duplicates the container by duplicating what it holds"
	compositeDuplicatesReason = "the crossing duplicates this value member by member"
	plainBitsReason           = "the value is plain bits the crossing may copy"
	noDuplicationRuleReason   = "the crossing has no rule for duplicating this value"

	crossComponentRefusedReason = "it holds `%s`, which the crossing cannot duplicate on its own"
)

// reason is the sentence one canonical clone outcome contributes as evidence.
// It is derived from the outcome and nothing else, so the five outcomes and the
// five sentences cannot drift apart.
func (k cloneSelectionKind) reason() string {
	switch k {
	case cloneSelectionResolved:
		return "one canonical `__clone(self: &T) -> T` was selected for the whole program"
	case cloneSelectionAbsent:
		return "no `__clone` declaration claims this type"
	case cloneSelectionShapeRejected:
		return "a `__clone` is declared for this type, but none has the required `fn(self: &T) -> T` shape"
	case cloneSelectionConflict:
		return "several equally specific `__clone` bodies remain, so none of them is canonical"
	case cloneSelectionUnmaterializable:
		return "the canonical `__clone` has neither a body nor an intrinsic implementation"
	default:
		return ""
	}
}

// String names a clone state for evidence and for test failures.
func (s CloneState) String() string {
	switch s {
	case CloneCopy:
		return "copy"
	case CloneValidMethod:
		return "valid-method"
	case CloneNonClonable:
		return "non-clonable"
	case CloneDeferred:
		return "deferred"
	default:
		return "unclassified"
	}
}

// refusalPath rebuilds the chain from the type that was asked about down to the
// component that settled a refusal.
//
// A verdict of "this type cannot cross" is not something an author can act on;
// the member at fault is. Each node records which component refused for it, so
// following those records once produces the whole path, and a visited guard
// stops a cyclic type from producing an endless one.
func (c *CapabilityClassifier) refusalPath(root types.TypeID, refused func(*capabilityNode) types.TypeID) []types.TypeID {
	path := []types.TypeID{root}
	seen := map[types.TypeID]struct{}{root: {}}
	for at := root; ; {
		node, ok := c.solved[at]
		if !ok {
			return path
		}
		next := refused(node)
		if next == types.NoTypeID {
			return path
		}
		if _, repeated := seen[next]; repeated {
			return path
		}
		seen[next] = struct{}{}
		path = append(path, next)
		at = next
	}
}

func (c *CapabilityClassifier) label(id types.TypeID) string {
	return types.Label(c.types, id)
}

// Describe renders one type's capability as a line that names types by label
// and never by interner id.
//
// Two compiles of one program are the same program, and this is how that is
// checked: interner ids are an artefact of the order types happened to be
// created in, so comparing them would test the build's bookkeeping rather than
// its verdicts. Comparing labelled evidence tests the verdicts.
func (c *CapabilityClassifier) Describe(id types.TypeID) (string, error) {
	capability, err := c.Classify(id)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s: copy=%t", c.label(capability.Type), capability.Copy)
	fmt.Fprintf(&out, " clone=%s", capability.Clone.State)
	if capability.Clone.MethodKey != "" {
		fmt.Fprintf(&out, "(%s%s)", capability.Clone.MethodKey, c.labelList(capability.Clone.TemplateArgs))
	}
	fmt.Fprintf(&out, " [%s]", capability.Clone.Reason)
	c.describeAxis(&out, "droppable", capability.CarrierDroppable, capability.DroppableReason, nil)
	c.describeAxis(&out, "traceable", capability.Traceable, capability.TraceableReason, nil)
	c.describeAxis(&out, "shard-movable", capability.ShardMovable, capability.ShardReason, capability.ShardPath)
	c.describeAxis(&out, "cross-clonable", capability.CrossClonable, capability.CrossCloneReason, capability.CrossClonePath)
	return out.String(), nil
}

func (c *CapabilityClassifier) describeAxis(out *strings.Builder, name string, verdict bool, reason string, path []types.TypeID) {
	fmt.Fprintf(out, " %s=%t [%s]", name, verdict, reason)
	if len(path) > 1 {
		fmt.Fprintf(out, " via %s", strings.Join(c.labels(path), " -> "))
	}
}

func (c *CapabilityClassifier) labels(ids []types.TypeID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, c.label(id))
	}
	return out
}

func (c *CapabilityClassifier) labelList(ids []types.TypeID) string {
	if len(ids) == 0 {
		return ""
	}
	return "<" + strings.Join(c.labels(ids), ", ") + ">"
}
