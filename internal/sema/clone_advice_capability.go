package sema

import (
	"fmt"

	"surge/internal/source"
	"surge/internal/types"
)

// LanguageClonable answers the one question every clone diagnostic and every
// clone advice asks: can the language obtain an independent value of this type?
//
// It is deliberately NOT the descriptor's `CLONABLE` bit and NOT a task's
// acquired duplication recipe. Those two are narrower facts about a compiled
// artefact; this one is about the source program, and the three ways it can be
// true collapse onto two recorded states: a Copy type duplicates as itself
// (CloneCopy), and everything else — including the runtime duplication that
// `string` performs — is reached through the one canonical
// `__clone(self: &T) -> T` selected for the whole program (CloneValidMethod).
// `string` is the reason that is not a third state: its duplication is declared
// as `@intrinsic fn __clone(self: &string) -> string`, so the selector already
// answers for it and a separate "runtime duplicates this" arm would be a second
// authority for the same fact.
//
// A receiver that still carries a generic parameter answers CloneDeferred: the
// question has no answer yet rather than a negative one, and an emitter that
// treats deferred as "no" would refuse a program that is about to be legal.
func (c *CapabilityClassifier) LanguageClonable(id types.TypeID) (CloneEvidence, error) {
	if c == nil {
		return CloneEvidence{}, fmt.Errorf("clone classification needs a classifier")
	}
	if id == types.NoTypeID {
		return CloneEvidence{}, fmt.Errorf("clone classification has nothing to answer for the absent type")
	}
	resolved := c.resolve(id)
	if resolved == types.NoTypeID {
		return CloneEvidence{}, fmt.Errorf("type %d is absent from the interner this classifier was built for", uint32(id))
	}
	return c.classifyClone(id, resolved, c.result.IsCopyType(id))
}

// cloneStateOf answers only the state, for one component of a walk.
//
// It goes through the same Select the top-level verdict uses, so a component
// cannot be called clonable by one route and non-clonable by another.
func (c *CapabilityClassifier) cloneStateOf(id types.TypeID) CloneState {
	if c.result.IsCopyType(id) {
		return CloneCopy
	}
	resolved := c.resolve(id)
	if resolved == types.NoTypeID {
		return CloneDeferred
	}
	if types.ContainsGenericParam(c.types, resolved) {
		return CloneDeferred
	}
	if c.clones.Select(c.valueIdentity(id)).kind == cloneSelectionResolved {
		return CloneValidMethod
	}
	return CloneNonClonable
}

// firstNonClonablePath runs from the type asked about down to the first
// component that is itself non-clonable.
//
// The walk follows the classifier's one edge relation, so it cannot reach a
// member the other four axes do not know about. It is breadth-first in
// declaration order, which is what makes "the first" mean the same thing on
// every run and puts the shallowest culprit — the one the author is most likely
// to own — at the end of the shortest path.
//
// A type whose refusal is entirely its own returns the root alone. That is not
// a missing answer: a struct of `int`s with no `__clone` is exactly the case
// where naming a component would be a lie.
func (c *CapabilityClassifier) firstNonClonablePath(root types.TypeID) []types.TypeID {
	queue := []cloneWalkStep{{id: root, parent: -1}}
	seen := map[types.TypeID]struct{}{root: {}}
	for head := 0; head < len(queue) && head < cloneComponentWalkLimit; head++ {
		for _, component := range c.components(queue[head].id) {
			if _, repeated := seen[component]; repeated {
				continue
			}
			seen[component] = struct{}{}
			queue = append(queue, cloneWalkStep{id: component, parent: head})
			if c.cloneStateOf(component) == CloneNonClonable {
				return cloneWalkPath(queue, len(queue)-1)
			}
		}
	}
	return []types.TypeID{root}
}

// cloneComponentWalkLimit bounds the component walk. A cyclic type is already
// stopped by the visited set; this stops a pathologically wide one from
// spending the compiler's time on a note.
const cloneComponentWalkLimit = 4096

// cloneWalkStep is one node of the breadth-first component walk together with
// the index of the node that reached it, which is how the path is rebuilt
// without keeping a slice per node.
type cloneWalkStep struct {
	id     types.TypeID
	parent int
}

// cloneWalkPath rebuilds the root-to-culprit chain from the parent links.
func cloneWalkPath(queue []cloneWalkStep, at int) []types.TypeID {
	var reversed []types.TypeID
	for at >= 0 {
		reversed = append(reversed, queue[at].id)
		at = queue[at].parent
	}
	path := make([]types.TypeID, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		path = append(path, reversed[i])
	}
	return path
}

// canDefineCloneHere proves whether a legal `extern<T> { fn __clone(self: &T) -> T }`
// could be declared for this exact type.
//
// It answers about the TYPE. Whether the spelling is reachable from one
// particular source position is a second question, and the advice renderer asks
// it: a type this proof accepts may still be unnameable in the file that would
// have to declare the method.
//
// `!sealed` alone is not the proof. A declaration the resolver would accept but
// the canonical selector would never find is worse than no advice, because the
// author writes it and the same error comes back.
func (c *CapabilityClassifier) canDefineCloneHere(
	resolved types.TypeID,
	kind cloneSelectionKind,
) (canDefine bool, reason string) {
	if kind != cloneSelectionAbsent {
		// Something already claims this type. Adding a second declaration turns
		// a rejected shape into a conflict, or leaves the winner in place; the
		// edit the author needs is on the declaration that exists.
		return false, cloneAlreadyClaimedReason
	}
	if c.facts[resolved].Sealed {
		return false, sealedTargetReason
	}
	tt, ok := c.types.Lookup(resolved)
	if !ok {
		return false, unknownTypeReason
	}
	if tt.Kind == types.KindFar || c.types.IsRuntimeHandleType(resolved) || c.types.IsRuntimePlacementType(resolved) {
		return false, runtimeOwnedTargetReason
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer, types.KindFn, types.KindGenericParam:
		return false, unextendableTargetReason
	}
	if !c.hasCanonicalDeclaration(resolved) {
		return false, structuralTargetReason
	}
	return true, extendableTargetReason
}

// hasCanonicalDeclaration reports whether the type has a nominal identity an
// `extern` block can name and the selector can key a method under.
//
// A structural shape — an unnamed tuple, an anonymous struct, an array — has no
// such identity, so there is no spelling for the block that would hold the
// method.
func (c *CapabilityClassifier) hasCanonicalDeclaration(resolved types.TypeID) bool {
	if info, ok := c.types.StructInfo(resolved); ok && info != nil {
		return info.Name != source.NoStringID && info.Decl != (source.Span{})
	}
	if info, ok := c.types.UnionInfo(resolved); ok && info != nil {
		return info.Name != source.NoStringID && info.Decl != (source.Span{})
	}
	if info, ok := c.types.EnumInfo(resolved); ok && info != nil {
		return info.Name != source.NoStringID && info.Decl != (source.Span{})
	}
	return false
}
