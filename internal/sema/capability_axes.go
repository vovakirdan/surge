package sema

import (
	"fmt"

	"surge/internal/types"
)

// The four axis definitions. Each answers about ONE type given its components'
// current answers; the fixpoint next door decides when those answers become
// final.

// evaluateDroppable answers the CARRIER droppability question: with this value
// stored inline in a typed carrier, does releasing the carrier have to run
// something?
//
// This is deliberately NOT `ownsHeap` (ownership_axes.go), though the two agree
// now that a value composite lives inline. `ownsHeap` used to answer the
// question boxed storage asked — is there a box behind this value — so it said
// yes for every value composite, a `@copy` struct of two integers included; it
// answers from the members now (RV2-DEBT-256), as this does: those two integers
// are bytes with nothing to reclaim, while a struct holding a string is
// droppable because the string's handle is. The predicates stay separate
// because they are asked of different storage — a carrier's inline bytes here,
// a binding's scope exit there — and `TestCapabilityDroppableAgreesWithOwnsHeap`
// holds them to one answer.
func (c *CapabilityClassifier) evaluateDroppable(
	id types.TypeID,
	at capabilityLookup,
) (verdict bool, reason string) {
	tt, ok := c.types.Lookup(id)
	if !ok {
		return false, unknownTypeReason
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer:
		return false, borrowOwnsNothingReason
	case types.KindFar:
		return true, farLeaseReason
	case types.KindFn:
		return false, functionPointerReason
	case types.KindOwn:
		return c.forwardDroppable(tt.Elem, at)
	}
	if c.types.IsRefCountedScalar(id) {
		return true, refCountedScalarReason
	}
	switch kind, _ := c.handleKind(id); kind {
	case handleString, handleElements, handleResource:
		return true, handleOwnsStorageReason
	case handlePlacement:
		return false, placementIsPlainReason
	}
	if culprit, ok := c.firstComponent(id, at, (*capabilityNode).isDroppable); ok {
		return true, fmt.Sprintf(componentDroppableReason, c.label(culprit))
	}
	return false, nothingToReclaimReason
}

// evaluateTraceable answers whether a tracing visitor has to enter this value
// to reach the runtime-managed storage it owns.
//
// The axis is NEW with this classifier, so its definition is stated here rather
// than inherited: a value is traceable when it holds, at any depth, a reference
// into storage this shard's runtime manages — a reference-counted scalar, a
// string, or any handle-backed value. Borrows are excluded because their
// storage is owned elsewhere in this shard and is reached through its owner,
// and visiting it twice would count it twice. A FAR handle is excluded for the
// opposite reason: it names storage in another shard, which this shard's
// visitor cannot reach at all. That far-handle row is exactly where this axis
// and CarrierDroppable part company — the lease still has to be released.
func (c *CapabilityClassifier) evaluateTraceable(
	id types.TypeID,
	at capabilityLookup,
) (verdict bool, reason string) {
	tt, ok := c.types.Lookup(id)
	if !ok {
		return false, unknownTypeReason
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer:
		return false, borrowIsNotARootReason
	case types.KindFar:
		return false, farIsAnotherShardReason
	case types.KindFn:
		return false, functionPointerReason
	case types.KindOwn:
		return c.forwardTraceable(tt.Elem, at)
	}
	// ContainsRefCountedScalar (ownership_axes.go) is the named authority for
	// this leg. It walks the shapes it recognises itself and deliberately stops
	// at unions, because a union crosses by move; the fixpoint below supplies
	// that reach, so the two together cover the whole type.
	if c.result.ContainsRefCountedScalar(id) {
		return true, refCountedScalarReason
	}
	switch kind, _ := c.handleKind(id); kind {
	case handleString, handleElements, handleResource:
		return true, handleIsARootReason
	case handlePlacement:
		return false, placementIsPlainReason
	}
	if culprit, ok := c.firstComponent(id, at, (*capabilityNode).isTraceable); ok {
		return true, fmt.Sprintf(componentTraceableReason, c.label(culprit))
	}
	return false, noRootsReason
}

// evaluateShardMovable answers the OWNED-MOVE question and only that one: may
// `cross_move_init` transfer a value of this type to another shard?
//
// The structure mirrors the attribute validator (attr_validation_types.go),
// rewritten against the merged fact table because the original needs a live
// checker holding one file's attributes.
//
// The use-site branches that judge a BORROWED or COPIED capture
// (on_crossing_capture.go) are deliberately not folded in. They answer a
// different question and reach a different verdict: a copied capture of a
// reference-counted scalar is refused there, because copying its word would
// leave two shards racing one non-atomic count — while an owned MOVE of the
// same value is fine, since it transfers the reference instead of sharing it.
// Folding the copy refusal in here would make the move impossible.
func (c *CapabilityClassifier) evaluateShardMovable(
	id types.TypeID,
	at capabilityLookup,
) (verdict bool, reason string, refused types.TypeID) {
	tt, ok := c.types.Lookup(id)
	if !ok {
		return false, unknownTypeReason, types.NoTypeID
	}
	switch tt.Kind {
	case types.KindUnit, types.KindNothing, types.KindBool, types.KindString,
		types.KindInt, types.KindUint, types.KindFloat, types.KindEnum:
		return true, builtinTravelsReason, types.NoTypeID
	case types.KindFar:
		return true, farTravelsReason, types.NoTypeID
	case types.KindReference, types.KindPointer:
		return false, borrowCannotCrossReason, types.NoTypeID
	case types.KindFn:
		return true, functionPointerTravelsReason, types.NoTypeID
	case types.KindOwn:
		return c.forwardShardMovable(tt.Elem, at)
	case types.KindTuple:
		return c.allComponents(id, at, (*capabilityNode).isShardMovable,
			tupleTravelsReason, shardComponentRefusedReason)
	case types.KindArray:
		return c.allComponents(id, at, (*capabilityNode).isShardMovable,
			arrayTravelsReason, shardComponentRefusedReason)
	}
	if c.isNominalArray(id) {
		return c.allComponents(id, at, (*capabilityNode).isShardMovable,
			arrayTravelsReason, shardComponentRefusedReason)
	}
	facts := c.facts[id]
	switch {
	case facts.ShardPinned:
		return false, shardPinnedReason, types.NoTypeID
	case facts.NoSend:
		return false, noSendReason, types.NoTypeID
	case facts.ShardMovable:
		return c.allComponents(id, at, (*capabilityNode).isShardMovable,
			markedMovableReason, shardComponentRefusedReason)
	}
	return false, unmarkedReason, types.NoTypeID
}

// evaluateCrossClonable answers whether `cross_clone_init` may build the
// design-defined crossing duplicate of this value WITHOUT invoking a user
// `__clone`.
//
// It is independent of the Clone axis in both directions, and both directions
// really occur. A move-only struct of two strings has no `__clone` at all and
// is still cross-clonable, because the compiler knows how to duplicate a string
// field by field. A struct holding a `Channel` may have a perfectly good user
// `__clone` and is still not cross-clonable, because a channel endpoint has no
// design-defined duplicate and the crossing is forbidden from calling that
// `__clone` to find one.
//
// Being a LEAST fixpoint has one consequence worth stating: a self-referential
// type is not cross-clonable, even when every leaf it can reach is. Nothing in
// such a type earns the verdict — each member is duplicable only if the type
// is — and duplicating one would need an unbounded recursion the design has not
// specified. Refusing is the fail-closed answer, and the shape is refused by
// its own definition rather than by a component.
func (c *CapabilityClassifier) evaluateCrossClonable(
	id types.TypeID,
	at capabilityLookup,
) (verdict bool, reason string, refused types.TypeID) {
	tt, ok := c.types.Lookup(id)
	if !ok {
		return false, unknownTypeReason, types.NoTypeID
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer:
		return false, borrowCannotCrossReason, types.NoTypeID
	case types.KindFar:
		return false, leaseHasOneHolderReason, types.NoTypeID
	case types.KindOwn:
		return c.forwardCrossClonable(tt.Elem, at)
	}
	facts := c.facts[id]
	switch {
	case facts.ShardPinned:
		return false, shardPinnedReason, types.NoTypeID
	case facts.NoSend:
		return false, noSendReason, types.NoTypeID
	}
	switch kind, _ := c.handleKind(id); kind {
	case handleString:
		return true, stringDuplicatesReason, types.NoTypeID
	case handleResource:
		return false, opaqueResourceReason, types.NoTypeID
	case handlePlacement:
		return true, placementIsPlainReason, types.NoTypeID
	case handleElements:
		return c.allComponents(id, at, (*capabilityNode).isCrossClonable,
			containerDuplicatesReason, crossComponentRefusedReason)
	}
	switch tt.Kind {
	case types.KindUnit, types.KindNothing, types.KindBool,
		types.KindInt, types.KindUint, types.KindFloat, types.KindEnum, types.KindFn:
		return true, plainBitsReason, types.NoTypeID
	case types.KindStruct, types.KindTuple, types.KindUnion, types.KindArray:
		return c.allComponents(id, at, (*capabilityNode).isCrossClonable,
			compositeDuplicatesReason, crossComponentRefusedReason)
	}
	return false, noDuplicationRuleReason, types.NoTypeID
}
