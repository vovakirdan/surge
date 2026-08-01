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
		then := b.block([]mir.Instr{assign(value, useRV(opStr(env.str, "x")))}, mir.Terminator{})
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
		assign(held, useRV(opStr(env.str, "x"))),
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
