package sema

import (
	"fmt"
	"sort"

	"surge/internal/types"
)

// FinalizeCloneObligations answers every concrete clone obligation against the
// merged program.
//
// It runs at the same post-merge seam that answers direct `clone(&value)` uses,
// for the same reason: the `__clone` that settles the question may be declared
// in a module the file only imports, so no file-local view can answer it. The
// seam is shared by build, ordinary `surge diag` and the language server, which
// is what makes all three report this identically without any of them running
// backend lowering.
func (r *Result) FinalizeCloneObligations() error {
	if r == nil || len(r.CloneObligations) == 0 {
		return nil
	}
	classifier, err := r.NewCapabilityClassifier()
	if err != nil {
		return err
	}
	obligations := cloneCloneObligations(r.CloneObligations)
	sort.SliceStable(obligations, func(i, j int) bool {
		return compareCloneObligations(&obligations[i], &obligations[j]) < 0
	})
	for i := range obligations {
		if err := checkCloneObligation(classifier, &obligations[i]); err != nil {
			return err
		}
	}
	return nil
}

// checkCloneObligation is the whole decision, for both the concrete and the
// instantiated-generic path.
//
// Deferred is not a refusal. A subject that still carries a generic parameter
// has not been decided, and answering "no" here would reject a program that is
// about to be legal — the instantiation asks again with the substitution in
// hand.
func checkCloneObligation(c *CapabilityClassifier, obligation *CloneObligation) error {
	if obligation.Subject == types.NoTypeID {
		return nil
	}
	evidence, err := c.LanguageClonable(obligation.Subject)
	if err != nil {
		// A subject the classifier cannot answer for is not a user error: it is
		// a type this program never built. Saying nothing is right, and saying
		// "not clonable" would be a refusal the author cannot act on.
		return nil //nolint:nilerr // an unanswerable subject is not a refusal
	}
	switch evidence.State {
	case CloneCopy, CloneValidMethod, CloneDeferred:
		return nil
	case CloneNonClonable:
		return reportCloneObligation(c, obligation, &evidence)
	default:
		return fmt.Errorf("clone obligation for %s got an unclassified verdict", obligation.SubjectLabel)
	}
}
