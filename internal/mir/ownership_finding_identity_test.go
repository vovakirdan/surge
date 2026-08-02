package mir_test

import (
	"testing"

	"surge/internal/mir"
)

// Two consuming operands can name the same local at the same MIR instruction.
// Their argument index is therefore part of the finding identity; otherwise a
// census dedupe (and one exact allowance) would collapse both defects.
func TestOwnershipFindingPositionsDistinguishSameInstructionOperands(t *testing.T) {
	env := newOwnershipEnv(t)
	b := newFn("stores_twice")
	value := b.param("value", env.str, true)
	b.block([]mir.Instr{{Kind: mir.InstrCall, Call: mir.CallInstr{
		Callee:       mir.Callee{Kind: mir.CalleeValue, Name: "store_pair"},
		Args:         []mir.Operand{opCopy(value, env.str), opCopy(value, env.str)},
		ArgContracts: []mir.ArgContract{mir.ArgContractStore, mir.ArgContractStore},
	}}}, retTerm())

	findings := env.verify(b.done())
	if len(findings) != 2 {
		t.Fatalf("same-instruction stores produced %d findings, want 2:\n%s", len(findings), formatFindings(findings))
	}
	if findings[0].ConsumingPosition != "arg[0]" || findings[1].ConsumingPosition != "arg[1]" {
		t.Fatalf("call positions = %q, %q, want arg[0], arg[1]", findings[0].ConsumingPosition, findings[1].ConsumingPosition)
	}
	if findings[0].ConsumingSite != findings[1].ConsumingSite || findings[0].DefSite != findings[1].DefSite {
		t.Fatalf("fixture did not isolate position identity: first=%+v second=%+v", findings[0], findings[1])
	}
}

// A sink after a control-flow join must retain every independent terminal
// failure. Returning after the first one would let an exact allowance for that
// branch hide the other branch's defect.
func TestOwnershipReportsEveryTerminalFailingDefinition(t *testing.T) {
	env := newOwnershipEnv(t)
	b := newFn("joins_two_aliases")
	borrowed := b.param("borrowed", env.str, true)
	cond := b.local("cond", env.boolTy, false)
	value := b.local("value", env.str, true)

	entry := b.block(nil, mir.Terminator{})
	left := b.block([]mir.Instr{assign(value, useRV(opCopy(borrowed, env.str)))}, mir.Terminator{})
	right := b.block([]mir.Instr{assign(value, useRV(opCopy(borrowed, env.str)))}, mir.Terminator{})
	join := b.block([]mir.Instr{dropL(value)}, retTerm())
	b.setTerm(entry, ifTerm(opCopy(cond, env.boolTy), left, right))
	b.setTerm(left, gotoTerm(join))
	b.setTerm(right, gotoTerm(join))

	findings := env.verify(b.done())
	if len(findings) != 2 {
		t.Fatalf("joined aliases produced %d findings, want 2:\n%s", len(findings), formatFindings(findings))
	}
	if findings[0].DefSite != "bb1#0" || findings[1].DefSite != "bb2#0" {
		t.Fatalf("terminal blame roots = %q, %q, want bb1#0, bb2#0", findings[0].DefSite, findings[1].DefSite)
	}
	for _, finding := range findings {
		if finding.ConsumingPosition != "place" {
			t.Fatalf("drop consuming position = %q, want place", finding.ConsumingPosition)
		}
	}
}

// Parameter roots need their local id in DefSite. Without it, two borrowed
// parameters feeding the same joined sink would still normalize to one key.
func TestOwnershipTerminalParameterRootsRemainDistinct(t *testing.T) {
	env := newOwnershipEnv(t)
	b := newFn("joins_two_parameters")
	leftParam := b.param("left", env.flt, true)
	rightParam := b.param("right", env.flt, true)
	cond := b.local("cond", env.boolTy, false)
	value := b.local("value", env.flt, true)

	entry := b.block(nil, mir.Terminator{})
	left := b.block([]mir.Instr{assign(value, useRV(opMove(leftParam, env.flt)))}, mir.Terminator{})
	right := b.block([]mir.Instr{assign(value, useRV(opMove(rightParam, env.flt)))}, mir.Terminator{})
	join := b.block([]mir.Instr{dropL(value)}, retTerm())
	b.setTerm(entry, ifTerm(opCopy(cond, env.boolTy), left, right))
	b.setTerm(left, gotoTerm(join))
	b.setTerm(right, gotoTerm(join))

	findings := env.verify(b.done())
	if len(findings) != 2 {
		t.Fatalf("joined parameter roots produced %d findings, want 2:\n%s", len(findings), formatFindings(findings))
	}
	if findings[0].DefSite != "parameter L0" || findings[1].DefSite != "parameter L1" {
		t.Fatalf("parameter blame roots = %q, %q, want parameter L0, parameter L1",
			findings[0].DefSite, findings[1].DefSite)
	}
}
