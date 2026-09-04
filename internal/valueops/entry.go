package valueops

import (
	"fmt"

	"surge/internal/layout"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Capabilities carries the semantic verdicts that are deliberately NOT ABI in
// this wave. They are true answers from the capability classifier, but the
// compiler cannot yet emit the rt_value_ops callbacks that would let them ride
// in Flags, so they are held here where no descriptor writer can mistake them
// for ABI bits.
//
// A later wave moves one of these into Flags at the same time as it fills the
// bit's slot, never before: the slot invariant refuses a Flags bit whose slot
// this registry cannot populate.
//
// Each verdict's evidence lives in the entry's Evidence record, so every reason
// string has exactly one home.
type Capabilities struct {
	// Traceable is the root-visiting verdict destined for RT_VALUE_FLAG_TRACEABLE.
	Traceable bool
	// CrossClonable is the crossing-duplicate verdict destined for
	// RT_VALUE_FLAG_CROSS_CLONABLE.
	//
	// ShardMovable used to sit beside it and left when its slot got a body: it
	// is RT_VALUE_FLAG_SHARD_MOVABLE now, an ABI bit like Droppable, and a
	// verdict recorded here as well would be the second statement of one fact
	// the owner's 2026-09-04 ruling forbids.
	CrossClonable bool
}

// Evidence is the frozen explanation of why one entry's flags and capabilities
// hold the values they do. It carries strings and type ids only; it never
// carries a symbol table, an AST node, or any other live compiler handle.
//
// Evidence participates in the alias-sharing predicate exactly, because two
// types that reach the same verdict for different reasons are two different
// entries as far as a reader of this registry is concerned.
type Evidence struct {
	// CloneMethodKey identifies the selected canonical __clone body.
	CloneMethodKey string
	// CloneReason explains the clone verdict, including every refusal kind.
	CloneReason string
	// DroppableReason explains the Capabilities.Droppable verdict.
	DroppableReason string
	// TraceableReason explains the Capabilities.Traceable verdict.
	TraceableReason string
	// ShardReason explains the Capabilities.ShardMovable verdict.
	ShardReason string
	// CrossCloneReason explains the Capabilities.CrossClonable verdict.
	CrossCloneReason string
	// ShardPath names the component chain that refused shard movability.
	ShardPath []types.TypeID
	// CrossClonePath names the component chain that refused cross cloning.
	CrossClonePath []types.TypeID
}

func (e *Evidence) clone() Evidence {
	out := *e
	out.ShardPath = clonePath(e.ShardPath)
	out.CrossClonePath = clonePath(e.CrossClonePath)
	return out
}

func (e *Evidence) equal(other *Evidence) bool {
	return e.CloneMethodKey == other.CloneMethodKey &&
		e.CloneReason == other.CloneReason &&
		e.DroppableReason == other.DroppableReason &&
		e.TraceableReason == other.TraceableReason &&
		e.ShardReason == other.ShardReason &&
		e.CrossCloneReason == other.CrossCloneReason &&
		pathsEqual(e.ShardPath, other.ShardPath) &&
		pathsEqual(e.CrossClonePath, other.CrossClonePath)
}

// Entry is the frozen operation plan for one exact semantic type.
//
// Fields absent from this struct are absent on purpose, and for two different
// reasons. drop_in_place, trace, cross_move_init and cross_clone_init arrive
// with the wave that can emit them; until then there is no field to hold a
// placeholder, so no reader can be told a slot exists when it does not.
//
// move_init and plan_cross are absent for the opposite reason: they are
// MANDATORY, and neither is this registry's to name. The backend derives a
// move body's name from the type id, the way it already names drop glue, and
// plan_cross resolves to a module-local symbol the backend defines. A field
// here would put the package back in the business of naming something it
// cannot see — which is exactly how copy_init came to be described as filled
// by a runtime symbol that did not exist.
type Entry struct {
	// Type is the EXACT semantic type id, never a layout-canonical one.
	Type types.TypeID
	// Facts are the frozen physical layout facts for the type. Their slice
	// storage is private to internal/layout and every accessor there returns an
	// owned copy, so this value is immutable from outside that package.
	Facts layout.PhysicalFacts
	// Flags are the ABI-true capability bits. See Flags for what may be set.
	Flags Flags
	// Capabilities are the staged, explicitly non-ABI verdicts.
	Capabilities Capabilities
	// CloneInit is the emitted clone_init symbol, required whenever FlagClonable
	// is set.
	CloneInit symbols.SymbolID
	// RecipeIndex is this entry's index in the module's descriptor recipe table.
	RecipeIndex int
	// Evidence explains the verdicts above.
	Evidence Evidence
}

func (e *Entry) clone() Entry {
	out := *e
	out.Evidence = e.Evidence.clone()
	return out
}

// sharesWith reports whether two entries are indistinguishable as operation
// plans, and may therefore share one stored entry behind two exact type ids.
//
// The comparison is exhaustive over everything except Type, which is the
// identity being aliased rather than part of the question: Value returns the
// requested exact id, so sharing is invisible to callers. Equal PhysicalFacts is
// the plain-data form of "same layout canonical" — the registry holds no
// interner and cannot consult a layout alias map, and two entries with identical
// facts describe identical physical layout, which is all this registry records
// about layout.
//
// There is deliberately no structural shortcut. A partial comparison would let
// two types that agree on their verdicts but disagree on why silently collapse
// into one entry, which is the kind of quiet merge this registry exists to make
// impossible.
func (e *Entry) sharesWith(other *Entry) bool {
	return factsEqual(&e.Facts, &other.Facts) &&
		e.Flags == other.Flags &&
		e.Capabilities == other.Capabilities &&
		e.CloneInit == other.CloneInit &&
		e.RecipeIndex == other.RecipeIndex &&
		e.Evidence.equal(&other.Evidence)
}

// checkSlots is THE slot invariant, in one implementation.
//
// It states one rule: every set Flags bit whose rt_value_ops slot must be filled
// by a compiler-emitted symbol has that symbol populated on this entry. Both the
// "a set flag bit has its manifest-required slots populated" invariant and the
// "every operation implied by a set flag resolves" assertion are this rule; they
// were two statements of one decision, and two implementations of one decision
// drift apart.
//
// Two bits fall outside the COMPILER-EMITTED symbol requirement for opposite
// reasons. COPY's copy_init slot is filled by CopyInitUnboundTrap, a named
// runtime symbol that does not copy: it exists to make the slot non-null, since
// the runtime's owner-private slot control refuses a descriptor whose flag and
// callback disagree, and to abort loudly if anyone dispatches the slot. The
// bytes are moved by the runtime's own rt_value_copy_init, which still holds the
// descriptor whose rt_value_layout.size is the width the callback signature does
// not carry. So there is a symbol here — it just is not one this compiler
// emitted, and it is not the thing that copies. DROPPABLE's drop_in_place is
// the other: the backend names that body from the type id, the way it already
// names drop glue, so this registry holds no field for it either — it records
// the verdict and lets the writer supply the name. The three still-staged bits
// require a slot nothing can fill yet, so setting them is refused outright
// rather than accepted with a slot that would ship null.
//
// It fails closed, naming the type, the flag bit and the slot.
func (e *Entry) checkSlots() error {
	if unknown := e.Flags &^ knownFlags(); unknown != 0 {
		return fmt.Errorf(
			"valueops: type#%d sets unknown rt_value_flags bits %s",
			e.Type, formatHex(uint64(unknown)),
		)
	}
	for index := range slotRules {
		rule := &slotRules[index]
		// A capability slot is only required when its bit is set. An
		// unconditional one is required always, which is why the guard names the
		// applicability rather than testing the bit: `e.Flags&0 == 0` would skip
		// every mandatory row silently, which is how both of them went missing.
		if rule.when == capabilitySlot && e.Flags&rule.bit == 0 {
			continue
		}
		switch rule.fillFor(e.Flags) {
		case filledByRuntimeSymbol, filledByModuleStub:
			// One shared symbol fills the slot for every descriptor this policy
			// applies to. The table carries the name; nothing per-entry is owed.
			continue
		case filledByBackendDerivedBody:
			// The backend generates the body and names it from the type id, so
			// this registry holds no symbol to check. Refusing here would demand
			// a name the package cannot see.
			continue
		case filledByRegistryNamedBody:
			if e.emittedSlot(rule.slot) == symbols.NoSymbolID {
				return fmt.Errorf(
					"valueops: type#%d sets %s but its %s slot has no emitted symbol",
					e.Type, rule.flag, rule.slot,
				)
			}
		case filledNowhere:
			if rule.when == unconditionalSlot {
				return fmt.Errorf(
					"valueops: type#%d needs its mandatory %s slot, which nothing fills",
					e.Type, rule.slot,
				)
			}
			return fmt.Errorf(
				"valueops: type#%d sets %s but this registry cannot carry its %s slot; "+
					"record the verdict in Capabilities until the slot can be emitted",
				e.Type, rule.flag, rule.slot,
			)
		}
	}
	return nil
}

// emittedSlot returns the emitted symbol this registry carries for one slot.
// Only clone_init is representable in this wave; it is keyed by slot name
// because that is the row identity, and two rows have no bit to key on.
func (e *Entry) emittedSlot(slot string) symbols.SymbolID {
	if slot == "clone_init" {
		return e.CloneInit
	}
	return symbols.NoSymbolID
}

// KeyEntry is the operation plan for one exact type used as a map key: the
// complete value plan plus the two always-present key callbacks.
//
// Hash and Equal are NoSymbolID in this wave. The manifest requires both to be
// non-null, and nothing derives them yet, so they are staged exactly like the
// four staged flag bits: recorded as absent, never as present-and-null.
type KeyEntry struct {
	// Value is the complete value operation plan embedded once per key region.
	Value Entry
	// Hash is the emitted rt_key_ops.hash symbol. Staged: NoSymbolID.
	Hash symbols.SymbolID
	// Equal is the emitted rt_key_ops.equal symbol. Staged: NoSymbolID.
	Equal symbols.SymbolID
}

func clonePath(path []types.TypeID) []types.TypeID {
	if len(path) == 0 {
		return nil
	}
	return append([]types.TypeID(nil), path...)
}

func pathsEqual(a, b []types.TypeID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// factsEqual compares frozen physical facts through their public surface, which
// is the only surface this package can reach.
func factsEqual(a, b *layout.PhysicalFacts) bool {
	if a.Size != b.Size || a.Align != b.Align || a.Stride != b.Stride ||
		a.TagSize != b.TagSize || a.TagAlign != b.TagAlign {
		return false
	}
	if !uint64sEqual(a.FieldOffsets(), b.FieldOffsets()) ||
		!uint64sEqual(a.FieldAligns(), b.FieldAligns()) {
		return false
	}
	leftCases, rightCases := a.UnionCases(), b.UnionCases()
	if len(leftCases) != len(rightCases) {
		return false
	}
	for i := range leftCases {
		left, right := leftCases[i], rightCases[i]
		if left.PayloadOffset != right.PayloadOffset ||
			left.PayloadSize != right.PayloadSize ||
			left.PayloadAlign != right.PayloadAlign {
			return false
		}
		if !uint64sEqual(left.FieldOffsets(), right.FieldOffsets()) {
			return false
		}
	}
	return true
}

func uint64sEqual(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
