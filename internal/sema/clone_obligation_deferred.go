package sema

import (
	"surge/internal/types"
)

// checkDeferredCloneObligation answers one obligation at one live
// instantiation.
//
// The subject and the container arrive already substituted, so the question is
// asked about the concrete types this instantiation actually produced. The
// classifier is the same one the concrete path uses, which is what makes the
// two report the same diagnostic rather than two that happen to agree.
func (b *reachableClosureBuilder) checkDeferredCloneObligation(
	current *InstantiationInstance,
	edge *DeferredCallableEdge,
	subject types.TypeID,
	container types.TypeID,
) error {
	if b.capabilities == nil {
		return nil
	}
	obligation := CloneObligation{
		Op:                edge.Obligation,
		Subject:           subject,
		Container:         container,
		Owner:             edge.Caller,
		Site:              edge.Witness.Site,
		SourceKey:         edge.Witness.SourceKey,
		SubjectLabel:      types.Label(b.identity.Types.Types, subject),
		ContainerLabel:    types.Label(b.identity.Types.Types, container),
		InstantiationSite: current.Witness.Site,
	}
	if obligation.Op == 0 {
		// An edge that lost its operation would report under whichever code the
		// table happened to answer first. Saying nothing is wrong too, so this
		// is an internal failure rather than a silent pass.
		return errCloneObligationLostItsOperation(edge)
	}
	return checkCloneObligation(b.capabilities, &obligation)
}
