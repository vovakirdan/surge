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

// slotRule states, for one rt_value_flags bit, which rt_value_ops callback slot
// the frozen manifest requires whenever that bit is set, and how this registry
// can satisfy it.
type slotRule struct {
	bit  Flags
	flag string // the manifest rt_value_flags enum name
	slot string // the manifest rt_value_ops field name

	// structural marks a slot the runtime satisfies generically, with no
	// compiler-emitted symbol to populate.
	structural bool

	// staged marks a slot this registry cannot carry an emitted symbol for at
	// all. Setting such a bit is refused: an entry that claims the capability
	// while the descriptor would ship a null callback is exactly the ABI
	// violation the invariant exists to prevent.
	staged bool
}

// slotRules is the whole flag-to-slot contract, in bit order. It is the only
// place the contract is written down.
var slotRules = [...]slotRule{
	{bit: FlagCopy, flag: "RT_VALUE_FLAG_COPY", slot: "copy_init", structural: true},
	{bit: FlagClonable, flag: "RT_VALUE_FLAG_CLONABLE", slot: "clone_init"},
	{bit: FlagDroppable, flag: "RT_VALUE_FLAG_DROPPABLE", slot: "drop_in_place", staged: true},
	{bit: FlagTraceable, flag: "RT_VALUE_FLAG_TRACEABLE", slot: "trace", staged: true},
	{bit: FlagShardMovable, flag: "RT_VALUE_FLAG_SHARD_MOVABLE", slot: "cross_move_init", staged: true},
	{bit: FlagCrossClonable, flag: "RT_VALUE_FLAG_CROSS_CLONABLE", slot: "cross_clone_init", staged: true},
}

// knownFlags is the union of every bit this package understands. A bit outside
// it is refused rather than carried through to a descriptor unexamined.
func knownFlags() Flags {
	var all Flags
	for _, rule := range slotRules {
		all |= rule.bit
	}
	return all
}

// name renders one bit for dumps and diagnostics, derived from the manifest
// enum name so the two never drift apart.
func (r slotRule) name() string {
	return strings.ToLower(strings.TrimPrefix(r.flag, "RT_VALUE_FLAG_"))
}

// String renders the set bits in a stable bit order. It is part of the dump, so
// its output is hashed: keep it deterministic.
func (f Flags) String() string {
	if f == 0 {
		return "none"
	}
	var parts []string
	for _, rule := range slotRules {
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
