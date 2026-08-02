package mir_test

import (
	"testing"

	"surge/internal/mir"
)

// The acceptance corpus: one minimal MIR reconstruction per ownership row this
// session closed by hand, each paired with the shape the fix produced.
//
// These are reconstructions of the SHAPE, not the literal historical source —
// the bugs are all fixed, so the pre-fix MIR cannot be produced by compiling
// anything today. What each fixture pins is the ownership question the row
// turned on, in the smallest MIR that asks it.

// RV2-DEBT-097 — sema classified an identity cast as a producer while MIR read
// its source as an alias. The cast converts nothing, so the temp it fills holds
// a reference it never earned, and the statement-end release frees the
// caller's.
func TestOwnershipFlagsIdentityCastAlias(t *testing.T) {
	env := newOwnershipEnv(t)

	t.Run("pre_fix", func(t *testing.T) {
		b := newFn("identity_cast")
		src := b.param("f", env.flt, true)
		tmp := b.local("tmp_cast", env.flt, true)
		b.block([]mir.Instr{
			assign(tmp, identityCastRV(opCopy(src, env.flt), env.flt)),
			dropL(tmp),
		}, retTerm())

		requireFindings(t, env.verify(b.done()),
			"identity_cast: drop of L1(tmp_cast) (def bb0#0) at bb0#1")
	})

	// The bridge is not a special case in the algorithm: the retain is simply a
	// second, later definition of the same local, and it is the only one
	// reaching the release.
	t.Run("bridged_by_retain", func(t *testing.T) {
		b := newFn("identity_cast")
		src := b.param("f", env.flt, true)
		tmp := b.local("tmp_cast", env.flt, true)
		b.block([]mir.Instr{
			assign(tmp, identityCastRV(opCopy(src, env.flt), env.flt)),
			assign(tmp, useRV(opRetain(tmp, env.flt))),
			dropL(tmp),
		}, retTerm())

		requireClean(t, env.verify(b.done()))
	})
}

// RV2-DEBT-100 — a borrowed union's payload read, then handed onward through
// several ordinary moves before an unconditional drop.
//
// This is THE test of the recursive TRANSFERS resolution: every one of those
// moves is individually OperandMove-shaped, so a flat table treating TRANSFERS
// as self-sufficient waves each link through. Only resolving back to the
// original aliasing read tells the chain apart from a genuinely owned one.
func TestOwnershipFlagsBorrowedPayloadLaunderedThroughMoves(t *testing.T) {
	env := newOwnershipEnv(t)

	build := func(retainAfterRead bool) *mir.Func {
		b := newFn("reads_payload")
		ref := b.param("slot", env.slotRef, false)
		subject := b.local("tmp_deref", env.slot, true)
		payload := b.local("tmp_payload", env.str, true)
		hop1 := b.local("tmp_hop1", env.str, true)
		hop2 := b.local("tmp_hop2", env.str, true)
		result := b.local("tmp_result", env.str, true)

		instrs := []mir.Instr{
			assign(subject, derefRV(opCopy(ref, env.slotRef))),
			assign(payload, tagPayloadRV(opCopy(subject, env.slot), "Payload", false)),
		}
		if retainAfterRead {
			instrs = append(instrs, assign(payload, useRV(opRetain(payload, env.str))))
		}
		instrs = append(instrs,
			assign(hop1, useRV(opMove(payload, env.str))),
			assign(hop2, useRV(opMove(hop1, env.str))),
			assign(result, useRV(opMove(hop2, env.str))),
			dropL(result),
		)
		b.block(instrs, retTerm())
		return b.done()
	}

	t.Run("pre_fix", func(t *testing.T) {
		// The blame lands on the aliasing READ, three moves upstream of the
		// drop, which is the whole point of resolving rather than trusting.
		requireFindings(t, env.verify(build(false)),
			"reads_payload: drop of L5(tmp_result) (def bb0#1) at bb0#5")
	})

	t.Run("retained_extraction", func(t *testing.T) {
		requireClean(t, env.verify(build(true)))
	})
}

// RV2-DEBT-098 — an arm's payload obligation did not follow the value when the
// arm handed it to the compare's own result.
//
// The pre-fix shape reads the payload without taking it out (MoveOut false, an
// ALIASES read) and then registers a release against the result it was assigned
// to. The fix is the flag saying the extraction actually MOVED, which makes the
// read inherit from a subject the callee owns at entry.
func TestOwnershipFlagsComparePayloadHandedToArmResult(t *testing.T) {
	env := newOwnershipEnv(t)

	build := func(moveOut bool) *mir.Func {
		b := newFn("arm_result")
		subject := b.param("slot", env.slot, true)
		payload := b.local("tmp_payload", env.str, true)
		result := b.local("tmp_arm", env.str, true)
		b.block([]mir.Instr{
			assign(payload, tagPayloadRV(opCopy(subject, env.slot), "Payload", moveOut)),
			assign(result, useRV(opMove(payload, env.str))),
			dropL(result),
		}, retTerm())
		return b.done()
	}

	t.Run("pre_fix", func(t *testing.T) {
		requireFindings(t, env.verify(build(false)),
			"arm_result: drop of L2(tmp_arm) (def bb0#0) at bb0#2")
	})

	t.Run("moved_out", func(t *testing.T) {
		requireClean(t, env.verify(build(true)))
	})
}

// RV2-DEBT-099 was not one bug but two in the same direction, which is why
// fixing only the obvious half made things worse before it made them better.
// Both are reconstructible, and each is flagged on its own.
func TestOwnershipFlagsBothHalvesOf099(t *testing.T) {
	env := newOwnershipEnv(t)

	// Half one: MIR's deref-through-a-reference did not retain its pointee.
	t.Run("deref_without_retain", func(t *testing.T) {
		b := newFn("deref_borrow")
		ref := b.param("fr", env.fltRef, false)
		tmp := b.local("tmp_deref", env.flt, true)
		b.block([]mir.Instr{
			assign(tmp, derefRV(opCopy(ref, env.fltRef))),
			dropL(tmp),
		}, retTerm())

		requireFindings(t, env.verify(b.done()),
			"deref_borrow: drop of L1(tmp_deref) (def bb0#0) at bb0#1")
	})

	t.Run("deref_with_retain", func(t *testing.T) {
		b := newFn("deref_borrow")
		ref := b.param("fr", env.fltRef, false)
		tmp := b.local("tmp_deref", env.flt, true)
		b.block([]mir.Instr{
			assign(tmp, derefRV(opCopy(ref, env.fltRef))),
			assign(tmp, useRV(opRetain(tmp, env.flt))),
			dropL(tmp),
		}, retTerm())

		requireClean(t, env.verify(b.done()))
	})

	// Half two: a reference-counted scalar PARAMETER is a borrow the caller
	// keeps holding, so releasing it without a retain of one's own frees the
	// caller's block. The exclusion mirrors sema's paramTransfersOwnership.
	t.Run("refcounted_scalar_parameter_released", func(t *testing.T) {
		b := newFn("drops_its_float_param")
		f := b.param("f", env.flt, true)
		b.block([]mir.Instr{dropL(f)}, retTerm())

		requireFindings(t, env.verify(b.done()),
			"drops_its_float_param: drop of L0(f) (def parameter L0) at bb0#0")
	})

	// A by-value droppable parameter that is NOT a reference-counted scalar is
	// owned at entry, and releasing it is correct. Both answers come from the
	// same predicate, so both belong here.
	t.Run("owned_parameter_released", func(t *testing.T) {
		b := newFn("drops_its_string_param")
		s := b.param("s", env.str, true)
		b.block([]mir.Instr{dropL(s)}, retTerm())

		requireClean(t, env.verify(b.done()))
	})
}

// RV2-DEBT-052's residual — an ignored payload position whose read is
// OperandCopyValue must never be treated as though the clone's SOURCE were
// released. Cloning MINTS: the source is untouched and keeps its own release.
func TestOwnershipCloneMintsRatherThanTransfers(t *testing.T) {
	env := newOwnershipEnv(t)

	t.Run("clone_is_minted", func(t *testing.T) {
		b := newFn("clones_a_composite")
		src := b.param("cell", env.cell, true)
		clone := b.local("tmp_clone", env.cell, true)
		b.block([]mir.Instr{
			assign(clone, useRV(opCopyValue(src, env.cell))),
			// BOTH are released, because there are genuinely two values. A
			// classification letting the clone's source count as released
			// through this operation is exactly the defect.
			dropL(clone),
			dropL(src),
		}, retTerm())

		requireClean(t, env.verify(b.done()))
	})

	// The same position read as a BARE copy is the defect: one value, two
	// holders, two releases.
	t.Run("bare_copy_is_not_a_clone", func(t *testing.T) {
		b := newFn("copies_a_composite")
		src := b.param("cell", env.cell, true)
		alias := b.local("tmp_alias", env.cell, true)
		b.block([]mir.Instr{
			assign(alias, useRV(opCopy(src, env.cell))),
			dropL(alias),
		}, retTerm())

		requireFindings(t, env.verify(b.done()),
			"copies_a_composite: drop of L1(tmp_alias) (def bb0#0) at bb0#1")
	})
}

// The counterexample that disproved deriving a payload read's ownership from
// its subject: a `@copy` union read through a deref MINTS a fresh cloned
// envelope, and its payload extraction must STILL retain, because the clone is
// deep-dropped later rather than shallow-released.
//
// "Does the subject resolve to MINTS" answers yes for both the moved and the
// duplicated case and cannot tell them apart. Only the extraction's own flag
// can, which is why the flag exists.
func TestOwnershipFlagsDuplicatedSubjectPayloadWithoutRetain(t *testing.T) {
	env := newOwnershipEnv(t)

	build := func(moveOut bool) *mir.Func {
		b := newFn("reads_copy_payload")
		ref := b.param("h", env.dupRef, false)
		borrowed := b.local("tmp_deref", env.dup, true)
		subject := b.local("tmp_clone", env.dup, true)
		payload := b.local("tmp_payload", env.cell, true)
		b.block([]mir.Instr{
			assign(borrowed, derefRV(opCopy(ref, env.dupRef))),
			// The clone: a genuinely fresh envelope, so the subject MINTS.
			assign(subject, useRV(opCopyValue(borrowed, env.dup))),
			assign(payload, tagPayloadRV(opCopy(subject, env.dup), "Held", moveOut)),
			dropL(payload),
		}, retTerm())
		return b.done()
	}

	// MoveOut false is the correct flag for a duplicated subject, and the
	// extraction is then an ALIASES read that owes a retain. A minted subject
	// does not excuse it.
	t.Run("duplicated_subject_needs_its_own_retain", func(t *testing.T) {
		requireFindings(t, env.verify(build(false)),
			"reads_copy_payload: drop of L3(tmp_payload) (def bb0#2) at bb0#3")
	})

	// And with the flag set, the extraction still RECURSES into the subject
	// rather than terminating on HIR's belief — here the subject really was
	// built as a clone, so it resolves.
	t.Run("moved_out_still_recurses_into_the_subject", func(t *testing.T) {
		requireClean(t, env.verify(build(true)))
	})
}

// A call whose positions disagree: the receiver is BORROWED and the value is
// STORED. Per-argument granularity has to be real, not per-call — the exact
// rt_array_push shape.
func TestOwnershipCallContractsAreCheckedPerArgument(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("pushes")
	ref := b.param("src", env.slotRef, false)
	subject := b.local("tmp_deref", env.slot, true)
	value := b.local("tmp_alias", env.str, true)
	b.block([]mir.Instr{
		assign(subject, derefRV(opCopy(ref, env.slotRef))),
		assign(value, tagPayloadRV(opCopy(subject, env.slot), "Payload", false)),
		{Kind: mir.InstrCall, Call: mir.CallInstr{
			Callee: mir.Callee{Kind: mir.CalleeValue, Name: "rt_array_push"},
			// The SAME aliasing operand at both positions, so the only thing
			// that can tell them apart is the contract.
			Args:         []mir.Operand{opCopy(value, env.str), opCopy(value, env.str)},
			ArgContracts: []mir.ArgContract{mir.ArgContractBorrow, mir.ArgContractStore},
		}},
	}, retTerm())

	requireFindings(t, env.verify(b.done()),
		"pushes: call_arg of L2(tmp_alias) (def use) at bb0#2")
}

// A reference-counted scalar in a STORAGE position is a sink, unlike the same
// type in an ordinary borrowing parameter.
//
// An earlier draft copied paramTransfersOwnership's reference-counted-scalar
// exclusion onto aggregate fields, where it does not apply: a `float` field of
// a struct is released by a later drop of that struct, so it needs a reference
// of its own exactly like any other stored value.
func TestOwnershipFlagsUnretainedScalarInAggregateField(t *testing.T) {
	env := newOwnershipEnv(t)

	build := func(retained bool) *mir.Func {
		b := newFn("builds_holder")
		f := b.param("f", env.flt, true)
		holder := b.local("tmp_holder", env.holder, true)
		field := opCopy(f, env.flt)
		if retained {
			field = opRetain(f, env.flt)
		}
		b.block([]mir.Instr{
			assign(holder, mir.RValue{Kind: mir.RValueStructLit, StructLit: mir.StructLit{
				TypeID: env.holder,
				Fields: []mir.StructLitField{{Name: "v", Value: field}},
			}}),
		}, retTerm())
		return b.done()
	}

	t.Run("bare_alias_in_field", func(t *testing.T) {
		requireFindings(t, env.verify(build(false)),
			"builds_holder: aggregate_field of L0(f) (def use) at bb0#0")
	})

	t.Run("retained_field", func(t *testing.T) {
		requireClean(t, env.verify(build(true)))
	})
}
