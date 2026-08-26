package sema

import (
	"fmt"
	"slices"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// CloneState names how a value of one type is duplicated when the language is
// asked for a clone.
//
// The zero value is deliberately not a state. An unset CloneEvidence must not
// read as "this type is Copy", which is the most permissive answer there is.
type CloneState uint8

const (
	// CloneCopy means the type is Copy, so duplicating it IS the clone and no
	// `__clone` body takes part.
	CloneCopy CloneState = iota + 1
	// CloneValidMethod means exactly one canonical `__clone(self: &T) -> T` was
	// selected for the whole program.
	CloneValidMethod
	// CloneNonClonable means no canonical implementation exists. The evidence's
	// Reason names which of the four ways the selection failed.
	CloneNonClonable
	// CloneDeferred means the receiver is not concrete, so the question has no
	// answer here rather than a negative one.
	CloneDeferred
)

// CloneEvidence is the clone verdict for one type together with what settled
// it: the selected body when there is one, and the reason when there is not.
//
// The three fields below the reason exist for the diagnostic that has to be
// acted on rather than merely read. "This type is not clonable" names no place
// to go; the component that refused, the declaration that was rejected, and
// whether a legal `__clone` could be declared for this exact type are the facts
// that turn the verdict into an edit the author can make.
type CloneEvidence struct {
	State        CloneState
	Method       symbols.SymbolID
	MethodKey    string
	TemplateArgs []types.TypeID
	Reason       string

	// Path runs from the type asked about down to the first component that is
	// itself non-clonable. It holds the root alone when the refusal is the
	// type's own and every component it holds is clonable.
	Path []types.TypeID
	// Decl points at the `__clone` declaration that settled a negative outcome,
	// when one exists at all.
	Decl source.Span
	// CanDefineHere reports whether a legal `extern<T> { fn __clone(self: &T) -> T }`
	// could be declared for this exact type. It is proved by the same canonical
	// receiver identity the resolver would register the method under; `!sealed`
	// alone is not proof.
	CanDefineHere bool
	// DefineReason says why definition advice is or is not offered.
	DefineReason string
}

// Capability is the whole semantic answer for one concrete type: whether it may
// be copied, how it is cloned, and the four verdicts the typed carrier ABI
// attaches to a monomorphic value descriptor.
//
// Every axis carries its reason, and the two axes that can be refused by
// something buried inside the type carry the path down to the component that
// refused, because "this type cannot cross" is not an answer anyone can act on.
type Capability struct {
	Type  types.TypeID
	Copy  bool
	Clone CloneEvidence

	CarrierDroppable bool
	Traceable        bool
	ShardMovable     bool
	CrossClonable    bool

	DroppableReason  string
	TraceableReason  string
	ShardReason      string
	CrossCloneReason string

	ShardPath      []types.TypeID
	CrossClonePath []types.TypeID
}

// CapabilityClassifier is the one semantic authority for these verdicts.
//
// It is built at a whole-program authority site, after every module's type
// attribute facts have been merged, and it consumes the merged table rather
// than a checker's live per-file attribute store: a type imported from another
// module carries its facts only through that merge, so a file-local view would
// answer differently depending on which file was asking.
//
// Its clone axis is a pure consumer of the program-wide canonical clone selector and calls
// Select, never Resolve. Select answers "which body clones this type" for the
// whole program; Resolve then applies one use site's lexical view. A capability
// is a property of the type, so a `__clone` that some caller cannot SEE is
// still the implementation this type has, and the classifier says so.
//
// One classifier is not safe for concurrent use: it memoizes, as the clone
// selector it wraps already does. Classification runs at serialized
// whole-program points, so this costs nothing today.
type CapabilityClassifier struct {
	types  *types.Interner
	result *Result
	clones *cloneCanonicalSelector
	facts  map[types.TypeID]TypeAttrFacts
	solved map[types.TypeID]*capabilityNode
}

// NewCapabilityClassifier builds the classifier for one merged semantic result.
//
// The clone selector is constructed HERE, from this result's CallableCandidates
// and nothing else. That list is the program's single clone authority; a
// private catalog assembled beside it would be a second one, free to disagree.
func (r *Result) NewCapabilityClassifier() (*CapabilityClassifier, error) {
	if r == nil {
		return nil, fmt.Errorf("capability classification needs a semantic result")
	}
	if r.TypeInterner == nil {
		return nil, fmt.Errorf("capability classification needs the type interner that holds the types it must answer for")
	}
	return &CapabilityClassifier{
		types:  r.TypeInterner,
		result: r,
		clones: newCloneCanonicalSelector(r.CallableCandidates, r.TypeInterner),
		facts:  r.TypeAttrFacts,
		solved: make(map[types.TypeID]*capabilityNode),
	}, nil
}

// Classify answers every axis for one type.
func (c *CapabilityClassifier) Classify(id types.TypeID) (Capability, error) {
	if c == nil {
		return Capability{}, fmt.Errorf("capability classification needs a classifier")
	}
	if id == types.NoTypeID {
		return Capability{}, fmt.Errorf("capability classification has nothing to answer for the absent type")
	}
	resolved := c.resolve(id)
	if resolved == types.NoTypeID {
		return Capability{}, fmt.Errorf("type %d is absent from the interner this classifier was built for", uint32(id))
	}

	capability := Capability{Type: id, Copy: c.result.IsCopyType(id)}
	clone, err := c.classifyClone(id, resolved, capability.Copy)
	if err != nil {
		return Capability{}, err
	}
	capability.Clone = clone

	if types.ContainsGenericParam(c.types, resolved) {
		// A type still carrying a generic parameter has no structure to walk:
		// what its members are is exactly what has not been decided. Answering
		// false is not a verdict about the type, so every reason says so.
		capability.DroppableReason = deferredStructureReason
		capability.TraceableReason = deferredStructureReason
		capability.ShardReason = deferredStructureReason
		capability.CrossCloneReason = deferredStructureReason
		return capability, nil
	}

	if err := c.solve(resolved); err != nil {
		return Capability{}, err
	}
	node := c.solved[resolved]
	capability.CarrierDroppable = node.droppable
	capability.Traceable = node.traceable
	capability.ShardMovable = node.shardMovable
	capability.CrossClonable = node.crossClonable
	capability.DroppableReason = node.droppableReason
	capability.TraceableReason = node.traceableReason
	capability.ShardReason = node.shardReason
	capability.CrossCloneReason = node.crossCloneReason
	if !node.shardMovable {
		capability.ShardPath = c.refusalPath(resolved, (*capabilityNode).shardRefusal)
	}
	if !node.crossClonable {
		capability.CrossClonePath = c.refusalPath(resolved, (*capabilityNode).crossRefusal)
	}
	return capability, nil
}

// classifyClone consumes the canonical selector and translates its outcome.
//
// The receiver handed to Select has its aliases and its `own` marker stripped:
// a clone hook is declared for the value type, and `own T` is the same value
// under a marker saying who holds it.
func (c *CapabilityClassifier) classifyClone(id, resolved types.TypeID, isCopy bool) (CloneEvidence, error) {
	if isCopy {
		return CloneEvidence{State: CloneCopy, Reason: copyCloneReason}, nil
	}
	if types.ContainsGenericParam(c.types, resolved) {
		return CloneEvidence{State: CloneDeferred, Reason: deferredCloneReason}, nil
	}
	selection := c.clones.Select(c.valueIdentity(id))
	if selection.internal != nil {
		return CloneEvidence{}, selection.internal
	}
	if selection.kind == cloneSelectionResolved {
		return CloneEvidence{
			State:        CloneValidMethod,
			Method:       selection.hook.Callee,
			MethodKey:    selection.hook.BodyKey,
			TemplateArgs: slices.Clone(selection.hook.TemplateArgs),
			Reason:       selection.kind.reason(),
		}, nil
	}
	reason := selection.kind.reason()
	if reason == "" {
		return CloneEvidence{}, fmt.Errorf(
			"canonical clone selection for type %d returned an unknown outcome %d",
			uint32(resolved), uint8(selection.kind),
		)
	}
	evidence := CloneEvidence{State: CloneNonClonable, Reason: reason, Decl: selection.decl}
	evidence.Path = c.firstNonClonablePath(resolved)
	evidence.CanDefineHere, evidence.DefineReason = c.canDefineCloneHere(resolved, selection.kind)
	return evidence, nil
}
