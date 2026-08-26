package sema

import (
	"fmt"
	"slices"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// CloneObligationOp names an operation whose validity requires that the
// language can obtain an independent value of one of the types it was written
// with.
//
// Only an operation that ACTUALLY needs a clone belongs here. Optional advice
// that would like to mention cloning does not: recording an obligation for it
// would let a hint reject an instantiation, which Global Rule 11 forbids.
type CloneObligationOp uint8

const (
	// CloneObligationTaskClone is `Task<T>.clone()`. Every successful clone is
	// another result entitlement, not another alias to one result, so the task
	// owes an independent `T` for each handle it hands out.
	CloneObligationTaskClone CloneObligationOp = iota + 1
	// CloneObligationMapKeys is `Map<K, V>.keys()`. The array it returns owns
	// its keys, so each key is duplicated out of the map that still holds it.
	//
	// The `keys()` site itself is not wired here: it belongs to the map lane,
	// which records this obligation at that call in one line. What this file
	// owns is that the answer comes from the same classifier.
	CloneObligationMapKeys
)

// cloneObligationSpec is everything that differs between operations. Keeping
// them in one table is what lets a new operation join by naming its code and
// its sentence rather than by growing a second diagnostic path.
type cloneObligationSpec struct {
	code diag.Code
	// headline takes the container the author wrote and the subject that must
	// be clonable, in that order.
	headline string
	// consumeHelp is the way out that does not need a clone at all.
	consumeHelp string
}

func (op CloneObligationOp) spec() (cloneObligationSpec, bool) {
	switch op {
	case CloneObligationTaskClone:
		return cloneObligationSpec{
			code: diag.SemaTypeNotClonable,
			headline: "cloning this handle to %s owes another independent `%s`, " +
				"which is neither Copy nor validly `__clone`-able",
			consumeHelp: "help: a task handle is affine — consume this one by awaiting it, " +
				"or move it to whoever awaits it, instead of handing out a second entitlement",
		}, true
	case CloneObligationMapKeys:
		return cloneObligationSpec{
			code: diag.SemaOperationNeedsClonable,
			headline: "`keys()` on %s returns an array that owns its keys, so it owes an independent `%s`, " +
				"which is neither Copy nor validly `__clone`-able",
			consumeHelp: "help: iterate the map in place, or borrow the keys, " +
				"instead of asking for an array that owns them",
		}, true
	default:
		return cloneObligationSpec{}, false
	}
}

func (op CloneObligationOp) String() string {
	switch op {
	case CloneObligationTaskClone:
		return "Task<T>.clone()"
	case CloneObligationMapKeys:
		return "Map<K, V>.keys()"
	default:
		return "unknown clone obligation"
	}
}

// CloneObligation is one operation that will be valid exactly when its subject
// type turns out to be clonable.
//
// It is recorded while checking a file and answered after every module has
// merged, because a `__clone` may be declared in a module this file only
// imports. A subject that still carries a generic parameter is not recorded
// here at all: it rides the instantiation graph instead, so the question is
// asked once per LIVE instantiation and an uninstantiated generic definition
// stays deferred by construction.
type CloneObligation struct {
	Op        CloneObligationOp
	Subject   types.TypeID
	Container types.TypeID
	Owner     symbols.SymbolID
	Site      source.Span
	SourceKey string
	// SubjectLabel and ContainerLabel are taken in the checker's vocabulary so
	// the message reads the same after the merge renumbers types.
	SubjectLabel   string
	ContainerLabel string
	// InstantiationSite is where the template carrying this operation was
	// instantiated with the argument that fails. It is empty for a concrete
	// use, where the operation and the type are written in one place.
	InstantiationSite source.Span
}

func (tc *typeChecker) recordCloneObligation(obligation *CloneObligation) {
	if tc == nil || tc.result == nil || obligation.Subject == types.NoTypeID {
		return
	}
	tc.result.CloneObligations = append(tc.result.CloneObligations, *obligation)
}

func canonicalizeCloneObligationSources(result *Result, resolve func(source.FileID) (string, error)) error {
	for i := range result.CloneObligations {
		obligation := &result.CloneObligations[i]
		key, err := resolve(obligation.Site.File)
		if err != nil {
			return fmt.Errorf("clone obligation source: %w", err)
		}
		if key == "" {
			return fmt.Errorf("clone obligation has empty canonical source identity")
		}
		obligation.SourceKey = key
	}
	return nil
}

func mergeCloneObligations(dst, src *Result, mapping map[symbols.SymbolID]symbols.SymbolID) {
	for i := range src.CloneObligations {
		obligation := src.CloneObligations[i]
		obligation.Owner = remapInstantiationSymbol(obligation.Owner, mapping)
		dst.CloneObligations = append(dst.CloneObligations, obligation)
	}
}

func cloneCloneObligations(input []CloneObligation) []CloneObligation {
	return slices.Clone(input)
}

func compareCloneObligations(left, right *CloneObligation) int {
	if left.SourceKey != right.SourceKey {
		return strings.Compare(left.SourceKey, right.SourceKey)
	}
	if cmp := compareSpanOffsets(left.Site, right.Site); cmp != 0 {
		return cmp
	}
	if left.Op != right.Op {
		if left.Op < right.Op {
			return -1
		}
		return 1
	}
	return strings.Compare(left.SubjectLabel, right.SubjectLabel)
}

// errCloneObligationLostItsOperation names an edge that arrived without the
// operation it was recorded for. It is an internal failure: the operation is
// what chooses the diagnostic number and the sentence, so an edge without one
// has no diagnostic to report.
func errCloneObligationLostItsOperation(edge *DeferredCallableEdge) error {
	return fmt.Errorf("deferred clone obligation %s carries no operation", edge.UseID)
}

// CloneObligationError is an unmet obligation carrying the diagnostic the user
// sees. The driver consumes it at the finalization seam, exactly like a clone
// canonicality failure, so no path exposes a raw post-merge error.
type CloneObligationError struct {
	diagnostic *diag.Diagnostic
}

func (e *CloneObligationError) Error() string {
	if e == nil || e.diagnostic == nil {
		return "clone obligation failed"
	}
	return e.diagnostic.Message
}

// Diagnostic returns a detached diagnostic for the driver finalization seam.
func (e *CloneObligationError) Diagnostic() *diag.Diagnostic {
	if e == nil || e.diagnostic == nil {
		return nil
	}
	detached := *e.diagnostic
	detached.Notes = slices.Clone(e.diagnostic.Notes)
	detached.Fixes = slices.Clone(e.diagnostic.Fixes)
	return &detached
}

// recordTaskCloneObligation notes that this `.clone()` owes an independent
// payload, and routes the question to whichever authority can answer it.
//
// A concrete payload is recorded for the post-merge validator. A payload that
// still carries a generic parameter is recorded on the instantiation graph
// instead, so it is asked once per live instantiation and an uninstantiated
// generic definition is never asked at all.
func (tc *typeChecker) recordTaskCloneObligation(
	receiver types.TypeID,
	payload types.TypeID,
	span source.Span,
) {
	if tc == nil || tc.result == nil || payload == types.NoTypeID || !tc.isTaskType(receiver) {
		return
	}
	// The message names the VALUE the author wrote. A receiver reached through
	// a borrow is the same task, and `&Task<Widget>` in the headline would read
	// as if the borrow were what refused.
	if value := tc.valueType(receiver); value != types.NoTypeID {
		receiver = value
	}
	if tc.types != nil && types.ContainsGenericParam(tc.types, payload) {
		// Receiver is the subject that must be clonable; ExpectedResult carries
		// the container the author wrote, so the instantiated message can name
		// `Task<Widget>` rather than only `Widget`.
		tc.pendingCloneObligation = CloneObligationTaskClone
		tc.rememberDeferredCallable(
			DeferredCloneObligation, payload, cloneObligationMethodName,
			nil, nil, receiver, false, span, ast.NoExprID, &DeferredCallableRequirement{},
		)
		tc.pendingCloneObligation = 0
		return
	}
	tc.recordCloneObligation(&CloneObligation{
		Op:             CloneObligationTaskClone,
		Subject:        payload,
		Container:      receiver,
		Owner:          tc.currentFnSym(),
		Site:           span,
		SubjectLabel:   tc.typeLabel(payload),
		ContainerLabel: tc.typeLabel(receiver),
	})
}

// cloneObligationMethodName is the method the obligation is about. The
// instantiation graph validates that every deferred edge names a method, and
// naming the contract that would have to exist keeps the edge readable in a
// dump rather than inventing a placeholder.
const cloneObligationMethodName = "__clone"
