package mir_test

import (
	"bytes"
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// The rule is EVERY reaching definition, not ANY.
//
// An unconditional release downstream of a branch where only one arm minted and
// the other merely aliased is RV2-DEBT-096's shape, and requiring one reaching
// definition to qualify would make the pass blind to it by construction.
func TestOwnershipRequiresEveryReachingDefinition(t *testing.T) {
	env := newOwnershipEnv(t)

	build := func(elseMints bool) *mir.Func {
		b := newFn("branch_mints_sometimes")
		borrowed := b.param("s", env.str, true)
		cond := b.local("c", env.boolTy, false)
		value := b.local("tmp_value", env.str, true)

		elseValue := opCopy(borrowed, env.str)
		if elseMints {
			elseValue = opRetain(borrowed, env.str)
		}

		entry := b.block(nil, mir.Terminator{})
		then := b.block([]mir.Instr{assign(value, useRV(opStr(env.str)))}, mir.Terminator{})
		els := b.block([]mir.Instr{assign(value, useRV(elseValue))}, mir.Terminator{})
		join := b.block([]mir.Instr{dropL(value)}, retTerm())

		b.setTerm(entry, ifTerm(opCopy(cond, env.boolTy), then, els))
		b.setTerm(then, gotoTerm(join))
		b.setTerm(els, gotoTerm(join))
		return b.done()
	}

	t.Run("one_arm_aliases", func(t *testing.T) {
		requireFindings(t, env.verify(build(false)),
			"branch_mints_sometimes: drop of L2(tmp_value) (def bb2#0) at bb3#0")
	})

	t.Run("both_arms_mint", func(t *testing.T) {
		requireClean(t, env.verify(build(true)))
	})
}

// A cycle that never reaches a terminal root resolves to UNRESOLVED, which
// counts as a violation, never as MINTS.
//
// This fixture is a ROOTED loop-carried transfer chain — a real minted value
// circulating through a loop by ordinary moves — and it is deliberately
// rejected. That is the documented cost of a termination rule with no
// strongly-connected-component analysis behind it: a false positive on this
// shape, never a false negative anywhere. If the cycle rule is ever tightened,
// this is the test that flips.
func TestOwnershipRejectsRootedLoopCarriedTransfer(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("circulates")
	held := b.local("tmp_held", env.str, true)
	hop := b.local("tmp_hop", env.str, true)

	entry := b.block([]mir.Instr{
		assign(held, useRV(opStr(env.str))),
	}, mir.Terminator{})
	loop := b.block([]mir.Instr{
		assign(hop, useRV(opMove(held, env.str))),
		assign(held, useRV(opMove(hop, env.str))),
		dropL(held),
	}, mir.Terminator{})
	b.setTerm(entry, gotoTerm(loop))
	b.setTerm(loop, gotoTerm(loop))

	requireFindings(t, env.verify(b.done()),
		"circulates: drop of L0(tmp_held) (def bb1#1) at bb1#2")
}

// A callee shape the lowering could not classify is a finding on its own
// terms, with no reaching-definition query: the gap IS the report, and passing
// it through silently is the failure mode the marker exists to prevent.
func TestOwnershipUnresolvedContractIsItselfAFinding(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("calls_unclassified")
	value := b.param("s", env.str, true)
	b.block([]mir.Instr{
		{Kind: mir.InstrCall, Call: mir.CallInstr{
			Callee:       mir.Callee{Kind: mir.CalleeValue, Name: "opaque"},
			Args:         []mir.Operand{opMove(value, env.str)},
			ArgContracts: []mir.ArgContract{mir.ArgContractUnresolved},
		}},
	}, retTerm())

	requireFindings(t, env.verify(b.done()),
		"calls_unclassified: unresolved_contract of L0(s) (def unresolved contract) at bb0#0")
}

// An operand built without a type is still checked.
//
// operandForLocal leaves Operand.Type unset because the caller already knew the
// local's type, and the async state terminators are built that way. Reading the
// operand as-is makes it look non-owning and skips the sink ENTIRELY — a
// missing check, which is worse than a false one.
func TestOwnershipResolvesEffectiveTypeOfUntypedOperands(t *testing.T) {
	env := newOwnershipEnv(t)

	build := func(kind mir.OperandKind) *mir.Func {
		b := newFn("yields_state")
		borrowed := b.param("s", env.str, true)
		state := b.local("tmp_state", env.str, true)
		b.block([]mir.Instr{
			assign(state, useRV(opCopy(borrowed, env.str))),
		}, mir.Terminator{
			Kind: mir.TermAsyncYield,
			// Exactly what operandForLocal builds: a place, no type.
			AsyncYield: mir.AsyncYieldTerm{State: mir.Operand{Kind: kind, Place: place(state)}},
		})
		return b.done()
	}

	t.Run("untyped_alias_is_flagged", func(t *testing.T) {
		requireFindings(t, env.verify(build(mir.OperandCopy)),
			"yields_state: async_state of L1(tmp_state) (def use) at bb0#term")
	})

	// And with a move at the same position, the recursion runs and reaches the
	// same aliasing definition one step back.
	t.Run("untyped_move_recurses", func(t *testing.T) {
		requireFindings(t, env.verify(build(mir.OperandMove)),
			"yields_state: async_state of L1(tmp_state) (def bb0#0) at bb0#term")
	})
}

// Spawn lowering may leave Value.Type unset because the place already names a
// typed local. The destination inherits that value's ownership answer, so the
// verifier must normalize the operand before classifying the Spawn definition.
func TestOwnershipResolvesEffectiveTypeOfUntypedSpawnValue(t *testing.T) {
	env := newOwnershipEnv(t)

	build := func(alias bool) *mir.Func {
		b := newFn("untyped_spawn_value")
		borrowed := b.param("s", env.str, true)
		source := b.local("tmp_source", env.str, true)
		spawned := b.local("tmp_spawn", env.str, true)
		src := useRV(opStr(env.str))
		if alias {
			src = useRV(opCopy(borrowed, env.str))
		}
		b.block([]mir.Instr{
			assign(source, src),
			{
				Kind: mir.InstrSpawn,
				Spawn: mir.SpawnInstr{
					Dst:   place(spawned),
					Value: mir.Operand{Kind: mir.OperandMove, Place: place(source)},
				},
			},
			dropL(spawned),
		}, retTerm())
		return b.done()
	}

	t.Run("minted_value_is_clean", func(t *testing.T) {
		requireClean(t, env.verify(build(false)))
	})
	t.Run("alias_still_traces_to_its_definition", func(t *testing.T) {
		requireFindings(t, env.verify(build(true)),
			"untyped_spawn_value: drop of L2(tmp_spawn) (def bb0#0) at bb0#2")
	})
}

// The pass reads and reports; it must leave the MIR byte-identical.
//
// Stated as a test rather than a promise, because "read-only" is the property
// every later step depends on and a stray write would be invisible until
// something downstream changed behavior.
func TestOwnershipVerifierLeavesMIRUnchanged(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipCorpusSource)

	before := dumpModule(t, mod, typesIn)
	mir.VerifyOwnership(mod, typesIn, semaRes)
	after := dumpModule(t, mod, typesIn)

	if before != after {
		t.Fatalf("the verifier changed the module it read")
	}
}

func dumpModule(t *testing.T, mod *mir.Module, typesIn *types.Interner) string {
	t.Helper()
	var buf bytes.Buffer
	if err := mir.DumpModule(&buf, mod, typesIn, mir.DumpOptions{}); err != nil {
		t.Fatalf("dump: %v", err)
	}
	return buf.String()
}

// An RValue's OWN classification reads operand types before it answers, so the
// effective type has to be resolved one level deeper than the operand a
// TRANSFERS answer would later recurse into.
//
// castIsIdentity compares the cast's source type against its target. With the
// source type left blank, it reports "not identity" — a representation change,
// which classifies MINTS — and the alias underneath is accepted without ever
// being traced. That is a false NEGATIVE, the one direction this pass may not
// err in, and it is invisible without a fixture that omits the type on purpose.
func TestOwnershipResolvesEffectiveTypeInsideAnRValue(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("untyped_identity_cast")
	src := b.param("f", env.flt, true)
	tmp := b.local("tmp_cast", env.flt, true)
	b.block([]mir.Instr{
		// The cast source names a local and carries no type of its own.
		assign(tmp, mir.RValue{Kind: mir.RValueCast, Cast: mir.CastOp{
			Value:    mir.Operand{Kind: mir.OperandCopy, Place: place(src)},
			TargetTy: env.flt,
		}}),
		dropL(tmp),
	}, retTerm())

	requireFindings(t, env.verify(b.done()),
		"untyped_identity_cast: drop of L1(tmp_cast) (def bb0#0) at bb0#1")
}

// A write into a MAP entry is a STORE sink exactly like a write into a struct
// field or an array element: the map keeps the value, and a later drop of the
// map is what releases it.
//
// It needs its own fixture because it is the one projected destination whose
// type the lowering's own place walk does not resolve — and an unresolved
// destination type means the position is skipped silently, which looks like a
// pass rather than the missing check it is.
func TestOwnershipChecksMapEntryAssignment(t *testing.T) {
	env := newOwnershipEnv(t)

	build := func(retained bool) *mir.Func {
		b := newFn("writes_map_entry")
		borrowed := b.param("s", env.str, true)
		entries := b.local("m", env.strMap, true)
		key := b.local("k", env.boolTy, false)

		value := opCopy(borrowed, env.str)
		if retained {
			value = opRetain(borrowed, env.str)
		}
		dst := place(entries)
		dst.Proj = []mir.PlaceProj{{Kind: mir.PlaceProjIndex, IndexLocal: key}}
		b.block([]mir.Instr{assignTo(dst, useRV(value))}, retTerm())
		return b.done()
	}

	t.Run("bare_alias_written_into_a_map", func(t *testing.T) {
		requireFindings(t, env.verify(build(false)),
			"writes_map_entry: projected_assign of L0(s) (def use) at bb0#0")
	})

	t.Run("retained_value_written_into_a_map", func(t *testing.T) {
		requireClean(t, env.verify(build(true)))
	})
}

// A bare global assignment is a STORE even though it has no projection: the
// module-global slot, not a local definition, owns the value after the write.
func TestOwnershipChecksBareGlobalAssignment(t *testing.T) {
	env := newOwnershipEnv(t)

	verify := func(retained bool) []mir.OwnershipFinding {
		b := newFn("writes_global")
		borrowed := b.param("s", env.str, true)
		value := opCopy(borrowed, env.str)
		if retained {
			value = opRetain(borrowed, env.str)
		}
		dst := mir.Place{Kind: mir.PlaceGlobal, Global: 0}
		b.block([]mir.Instr{assignTo(dst, useRV(value))}, retTerm())
		f := b.done()
		mod := &mir.Module{
			Funcs:   map[mir.FuncID]*mir.Func{f.ID: f},
			Globals: []mir.Global{{Name: "stored", Type: env.str, IsMut: true}},
		}
		return mir.VerifyOwnership(mod, env.typesIn, env.semaRes)
	}

	t.Run("bare_alias_written_into_a_global", func(t *testing.T) {
		requireFindings(t, verify(false),
			"writes_global: global_assign of L0(s) (def use) at bb0#0")
	})

	t.Run("retained_value_written_into_a_global", func(t *testing.T) {
		requireClean(t, verify(true))
	})
}
