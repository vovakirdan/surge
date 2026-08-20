// Package valueops holds the immutable per-module operation-plan registry that
// pairs frozen physical layout facts with the semantic verdicts a runtime value
// descriptor needs.
//
// The package is deliberately standalone. It depends on no compiler analysis
// package, so a stored entry can never become a live view onto mutable compiler
// state: everything is plain data, deep-copied on the way in and on the way out.
// An import-absence test pins the allowed dependency set so that adding one is a
// conscious decision rather than an accident.
package valueops

import (
	"fmt"
	"strings"
)

// Flags mirrors the frozen runtime rt_value_flags bit set exactly. The values
// are the ABI's, not this package's; a pinning test compares every constant
// against the generated manifest view.
//
// STAGING. Only the bits the compiler can currently back with a real runtime
// slot may be set here: Copy and Clonable. The remaining four verdicts are
// recorded on Capabilities instead, which is explicitly not ABI. A later wave
// migrates a verdict from Capabilities into Flags only when it can fill that
// bit's rt_value_ops slot, and the slot invariant below refuses the migration
// until it can. That makes the half-migrated descriptor unrepresentable rather
// than merely discouraged.
type Flags uint64

const (
	// FlagCopy mirrors RT_VALUE_FLAG_COPY: ordinary copies may initialize an
	// independent destination.
	FlagCopy Flags = 1 << iota
	// FlagClonable mirrors RT_VALUE_FLAG_CLONABLE: clone_init is present under
	// the Copy-or-valid-__clone rule.
	FlagClonable
	// FlagDroppable mirrors RT_VALUE_FLAG_DROPPABLE: drop_in_place is present
	// and must run exactly once for an initialized obligation.
	FlagDroppable
	// FlagTraceable mirrors RT_VALUE_FLAG_TRACEABLE: trace is present and visits
	// every live VM or GC root.
	FlagTraceable
	// FlagShardMovable mirrors RT_VALUE_FLAG_SHARD_MOVABLE: cross_move_init may
	// transfer the value between shards.
	FlagShardMovable
	// FlagCrossClonable mirrors RT_VALUE_FLAG_CROSS_CLONABLE: cross_clone_init
	// may create the design-defined crossing duplicate without invoking user
	// __clone.
	FlagCrossClonable
)

// StagingNotice is the sentence Dump prints so that every reader of a dump, and
// every reader of a hash mismatch, is told which bits this registry can carry
// before they reach the entries.
const StagingNotice = "flags carry abi-true bits only (copy, clonable); " +
	"droppable, traceable, shard_movable and cross_clonable are recorded as " +
	"non-abi capabilities and become flags only once their rt_value_ops slots are filled"

// applicability says WHEN the frozen manifest requires a slot to be non-null.
//
// The table used to have one row per flag bit, which encoded "a slot exists if
// and only if a capability bit names it". The manifest does not say that: it
// declares `move_init` and `plan_cross` non-null unconditionally, and those are
// exactly the two slots no bit names. Modelling presence as its own axis is what
// lets them have rows at all.
type applicability uint8

const (
	// capabilitySlot mirrors a manifest field declared nullable: the slot is
	// present exactly when its bit is set.
	capabilitySlot applicability = iota + 1
	// unconditionalSlot mirrors a manifest field declared non-null: the slot is
	// present in every descriptor, whatever the flags say.
	unconditionalSlot
)

// fillPolicy says WHAT occupies a slot once it is present, and who owns the
// name. It is a separate axis from applicability, and `plan_cross` is the row
// that proves the two cannot be collapsed: it is unconditional in PRESENCE
// while its FILLER is chosen per descriptor.
type fillPolicy uint8

const (
	// filledByRuntimeSymbol binds one exported runtime symbol the frozen
	// manifest declares, so a runtime without it fails to link.
	filledByRuntimeSymbol fillPolicy = iota + 1
	// filledByModuleStub binds one module-local symbol the BACKEND emits per
	// module. It is deliberately not the same kind as a runtime symbol: it is
	// absent from the manifest's runtime_functions, so nothing links against it
	// and the link sentinel says nothing about it.
	filledByModuleStub
	// filledByRegistryNamedBody binds a per-type symbol this registry can name,
	// because monomorphization minted it and mir handed it over.
	filledByRegistryNamedBody
	// filledByBackendDerivedBody binds a per-type body the backend generates and
	// names from the type id, the way it already names drop glue. This registry
	// never sees that name, which is why it holds no field for it.
	filledByBackendDerivedBody
	// filledNowhere is a slot no filler exists for in this wave. It is legal
	// only on a capability slot, whose bit can then be refused; an unconditional
	// slot filled nowhere would ship a null the runtime rejects.
	filledNowhere
)

// PlanCrossUnavailableStub is the module-local symbol a descriptor without any
// crossing capability binds into rt_value_ops.plan_cross.
//
// It is an invariant trap, not a refusal. A descriptor whose own flags admit
// neither cross mode has no legal `mode` argument to answer, so reaching this
// body means the caller chose an operation the descriptor excludes — a protocol
// violation rather than a recoverable outcome. That is why it carries no status:
// a status would assert the call was legal and merely declined.
//
// Unlike CopyInitUnboundTrap this is NOT a runtime symbol. The runtime may never
// call it, so declaring it in the frozen manifest would extend the ABI, move the
// hash and the link sentinel, and buy no capability. The backend emits one
// `define internal` per module instead.
const PlanCrossUnavailableStub = "__surge_value_plan_cross_unavailable"

// slotRule states which rt_value_ops callback slot the frozen manifest requires,
// when it requires it, and how this registry can satisfy it.
//
// The row identity is `slot`, not `bit`: two rows have no bit at all.
type slotRule struct {
	slot string // the manifest rt_value_ops field name — the row identity
	when applicability

	bit  Flags  // zero unless when == capabilitySlot
	flag string // empty unless when == capabilitySlot

	// fill is the policy for a descriptor that HAS what the slot is for;
	// otherwise is the policy for one that does not, and gate names the bits
	// that choose between them. A row whose filler does not depend on the
	// descriptor leaves otherwise and gate zero.
	fill      fillPolicy
	otherwise fillPolicy
	gate      Flags

	// runtimeSymbol names the exported runtime symbol a filledByRuntimeSymbol
	// policy binds. The runtime carries it under the same frozen manifest whose
	// hash the link sentinel checks, so a runtime that lacks it fails to link.
	//
	// It is a name and not a claim of absence on purpose. "The runtime handles
	// it" was once written here as a comment, and the runtime did not: the C
	// side demands COPY imply a non-null copy_init, and no symbol existed to
	// satisfy it. A row that names a filler nothing provides repeats that.
	runtimeSymbol string

	// moduleSymbol names the module-local symbol a filledByModuleStub policy
	// binds. It is kept apart from runtimeSymbol because the two differ in who
	// owns them: this one is absent from the manifest, nothing links against it,
	// and the backend defines it once per module.
	moduleSymbol string
}

// fillFor resolves which policy applies to a descriptor carrying these flags.
func (r *slotRule) fillFor(flags Flags) fillPolicy {
	if r.otherwise == 0 || r.gate == 0 {
		return r.fill
	}
	if flags&r.gate != 0 {
		return r.fill
	}
	return r.otherwise
}

// sharedSymbol returns the one symbol that fills this slot for every descriptor
// the policy applies to, and whether such a symbol exists.
func (r *slotRule) sharedSymbol(policy fillPolicy) (string, bool) {
	switch policy {
	case filledByRuntimeSymbol:
		return r.runtimeSymbol, true
	case filledByModuleStub:
		return r.moduleSymbol, true
	default:
		return "", false
	}
}

// CopyInitUnboundTrap is the runtime symbol every descriptor that sets FlagCopy
// binds into rt_value_ops.copy_init.
//
// It does not copy. It is an aborting trap whose two jobs are to make the slot
// non-null, so the runtime's flag/callback biconditional holds, and to be loud
// if anyone dispatches the slot: the frozen callback signature carries no width,
// so the byte copy is performed by the runtime's rt_value_copy_init, which still
// holds the descriptor whose rt_value_layout.size is that width.
//
// It is exported because the descriptor writer is the one that has to bind it,
// and there is exactly one right name. The runtime declares it from the frozen
// ABI manifest, so a runtime that does not carry it fails to link rather than
// producing a descriptor the owner-private slot control refuses at run time.
const CopyInitUnboundTrap = "rt_value_copy_init_unbound_trap"

// slotRules is the whole slot contract: every rt_value_ops callback field, when
// the manifest requires it, and what fills it. Capability rows come first in bit
// order, because Flags.String renders from this table and its output is hashed;
// the two mandatory rows follow and render nothing.
//
// It is the only place the contract is written down, and a test pins it against
// the manifest RECORD in both directions — without which a slot no bit names is
// invisible here by construction, which is how both mandatory rows went missing.
var slotRules = [...]slotRule{
	{slot: "copy_init", when: capabilitySlot, bit: FlagCopy, flag: "RT_VALUE_FLAG_COPY",
		fill: filledByRuntimeSymbol, runtimeSymbol: CopyInitUnboundTrap},
	{slot: "clone_init", when: capabilitySlot, bit: FlagClonable, flag: "RT_VALUE_FLAG_CLONABLE",
		fill: filledByRegistryNamedBody},
	{slot: "drop_in_place", when: capabilitySlot, bit: FlagDroppable, flag: "RT_VALUE_FLAG_DROPPABLE",
		fill: filledNowhere},
	{slot: "trace", when: capabilitySlot, bit: FlagTraceable, flag: "RT_VALUE_FLAG_TRACEABLE",
		fill: filledNowhere},
	{slot: "cross_move_init", when: capabilitySlot, bit: FlagShardMovable, flag: "RT_VALUE_FLAG_SHARD_MOVABLE",
		fill: filledNowhere},
	{slot: "cross_clone_init", when: capabilitySlot, bit: FlagCrossClonable, flag: "RT_VALUE_FLAG_CROSS_CLONABLE",
		fill: filledNowhere},

	// The two the manifest declares non-null, which no capability bit names.
	// They are mandatory alike and mandatory for DIFFERENT reasons.
	//
	// move_init is SEMANTICALLY unconditional: every initialized value can
	// transfer its ownership into an empty destination, so there is always a
	// real body to emit. The backend derives its name from the type id, the way
	// it already names drop glue, which is why no field here holds it.
	{slot: "move_init", when: unconditionalSlot, fill: filledByBackendDerivedBody},

	// plan_cross is only STRUCTURALLY unconditional: the slot is always present
	// while its callable domain is decided by the cross capability flags. A
	// descriptor carrying neither cross bit has no legal `mode` to answer, so it
	// binds the module-local trap instead of a body.
	//
	// The gate reads FLAGS and not Capabilities on purpose. The runtime sees
	// only layout.flags, and a staged verdict must imply no ABI obligation until
	// its bit can be set — letting one choose an ABI slot's filler is the
	// coupling staging exists to prevent. While both cross rows are
	// filledNowhere their bits are refused outright, so this row resolves to the
	// trap for every descriptor this registry can hold today.
	{slot: "plan_cross", when: unconditionalSlot,
		gate:      FlagShardMovable | FlagCrossClonable,
		fill:      filledByBackendDerivedBody,
		otherwise: filledByModuleStub, moduleSymbol: PlanCrossUnavailableStub},
}

// FillKind tells a descriptor writer who owns the thing that goes in a slot.
type FillKind uint8

const (
	// FillRuntimeSymbol binds the named exported runtime symbol.
	FillRuntimeSymbol FillKind = iota + 1
	// FillModuleStub binds the named module-local symbol, which this module must
	// also define.
	FillModuleStub
	// FillRegistryNamedBody binds the per-type symbol this registry carries.
	FillRegistryNamedBody
	// FillBackendDerivedBody means emit a per-type body and name it yourself.
	FillBackendDerivedBody
	// FillNone means no filler exists in this wave; the slot ships null and the
	// bit that would require it is refused.
	FillNone
)

// Filler is the answer to "what do I put in this slot". Symbol is non-empty only
// for the two shared kinds.
type Filler struct {
	Kind   FillKind
	Symbol string
}

// SlotFiller answers what fills one rt_value_ops slot for a descriptor carrying
// these flags.
//
// It is keyed by SLOT NAME rather than by flag bit, because two mandatory slots
// have no bit — the predecessor took a bit and therefore could not be asked
// about them at all, and would have answered a zero-bit query with whichever
// row happened to match. It fails closed on a name no rule covers.
func SlotFiller(slot string, flags Flags) (Filler, error) {
	for index := range slotRules {
		rule := &slotRules[index]
		if rule.slot != slot {
			continue
		}
		// Presence first. A capability slot whose bit is clear is ABSENT, and an
		// absent slot ships null whatever its filler would have been — the
		// runtime's biconditional is "callback non-null exactly when the bit is
		// set", so answering with copy_init's trap for a non-Copy descriptor
		// would build one the preflight refuses.
		if rule.when == capabilitySlot && flags&rule.bit == 0 {
			return Filler{Kind: FillNone}, nil
		}
		policy := rule.fillFor(flags)
		symbol, shared := rule.sharedSymbol(policy)
		if shared && symbol == "" {
			return Filler{}, fmt.Errorf(
				"valueops: slot %s resolves to a shared filler with no symbol", slot,
			)
		}
		switch policy {
		case filledByRuntimeSymbol:
			return Filler{Kind: FillRuntimeSymbol, Symbol: symbol}, nil
		case filledByModuleStub:
			return Filler{Kind: FillModuleStub, Symbol: symbol}, nil
		case filledByRegistryNamedBody:
			return Filler{Kind: FillRegistryNamedBody}, nil
		case filledByBackendDerivedBody:
			return Filler{Kind: FillBackendDerivedBody}, nil
		case filledNowhere:
			return Filler{Kind: FillNone}, nil
		}
		return Filler{}, fmt.Errorf("valueops: slot %s has no fill policy", slot)
	}
	return Filler{}, fmt.Errorf("valueops: no rule covers rt_value_ops slot %q", slot)
}

// knownFlags is the union of every bit this package understands. A bit outside
// it is refused rather than carried through to a descriptor unexamined.
func knownFlags() Flags {
	var all Flags
	for index := range slotRules {
		rule := &slotRules[index]
		if rule.when != capabilitySlot {
			continue
		}
		all |= rule.bit
	}
	return all
}

// name renders one bit for dumps and diagnostics, derived from the manifest
// enum name so the two never drift apart.
func (r *slotRule) name() string {
	return strings.ToLower(strings.TrimPrefix(r.flag, "RT_VALUE_FLAG_"))
}

// String renders the set bits in a stable bit order. It is part of the dump, so
// its output is hashed: keep it deterministic.
func (f Flags) String() string {
	if f == 0 {
		return "none"
	}
	var parts []string
	for index := range slotRules {
		rule := &slotRules[index]
		// Unconditional rows name no bit, so they render nothing. The guard is
		// explicit rather than relying on `f&0 != 0` being false, because this
		// output is hashed: a row that started rendering would move the hash of
		// every dump.
		if rule.when != capabilitySlot {
			continue
		}
		if f&rule.bit != 0 {
			parts = append(parts, rule.name())
		}
	}
	if unknown := f &^ knownFlags(); unknown != 0 {
		parts = append(parts, "unknown:"+formatHex(uint64(unknown)))
	}
	return strings.Join(parts, "|")
}

func formatHex(v uint64) string {
	const digits = "0123456789abcdef"
	if v == 0 {
		return "0x0"
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v&0xf]
		v >>= 4
	}
	return "0x" + string(buf[i:])
}
