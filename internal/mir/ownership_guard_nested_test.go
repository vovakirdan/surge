package mir_test

import (
	"testing"

	"surge/internal/mir"
)

// A nested canonical join may be opened only to preserve path correlation.
// The inner mint path raises the guard and builds an owned value, but can then
// overwrite that value with a caller-owned alias before the join. Recursion
// must keep that later definition and reject the guarded drop.
func TestOwnershipNestedGuardRejectsAliasOverwriteAfterTrue(t *testing.T) {
	env := newOwnershipEnv(t)
	b := newFn("guarded_nested_overwrite")

	borrowed := b.param("s", env.str, true)
	outerCond := b.local("outer", env.boolTy, false)
	innerCond := b.local("inner", env.boolTy, false)
	overwriteCond := b.local("overwrite", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	innerValue := b.local("tmp_inner", env.str, true)
	outerValue := b.local("tmp_outer", env.str, true)
	dropped := b.local("tmp_dropped", env.str, true)

	entry := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, false))),
	}, mir.Terminator{})
	innerTest := b.block(nil, mir.Terminator{})
	innerMint := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(innerValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	aliasOverwrite := b.block([]mir.Instr{
		assign(innerValue, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	innerForward := b.block([]mir.Instr{
		assign(innerValue, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	innerJoin := b.block([]mir.Instr{
		assign(outerValue, useRV(opMove(innerValue, env.str))),
	}, mir.Terminator{})
	outerMint := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(outerValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	outerJoin := b.block([]mir.Instr{
		assign(dropped, useRV(opMove(outerValue, env.str))),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(dropped)}, mir.Terminator{})
	join := b.block(nil, retTerm())

	b.setTerm(entry, ifTerm(opCopy(outerCond, env.boolTy), innerTest, outerMint))
	b.setTerm(innerTest, ifTerm(opCopy(innerCond, env.boolTy), innerMint, innerForward))
	b.setTerm(innerMint, ifTerm(opCopy(overwriteCond, env.boolTy), aliasOverwrite, innerJoin))
	b.setTerm(aliasOverwrite, gotoTerm(innerJoin))
	b.setTerm(innerForward, gotoTerm(innerJoin))
	b.setTerm(innerJoin, gotoTerm(outerJoin))
	b.setTerm(outerMint, gotoTerm(outerJoin))
	b.setTerm(outerJoin, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
	b.setTerm(dropBB, gotoTerm(join))

	requireFindings(t, env.verify(b.done()),
		"guarded_nested_overwrite: drop of L7(tmp_dropped) (def bb3#0) at bb8#0",
		"guarded_nested_overwrite: drop of L7(tmp_dropped) (def bb4#0) at bb8#0")
}

// This loop reaches the recursive correlation layer before it cycles: the
// outer mixed frontier opens `outerValue = move innerValue`; a loop-carried
// `innerValue = move outerValue` then points back to the same source query.
// The active-path guard must reject that repetition rather than treating a
// rooted transfer cycle as ownership evidence.
func TestOwnershipNestedGuardCorrelationRejectsCycles(t *testing.T) {
	env := newOwnershipEnv(t)
	b := newFn("guarded_nested_cycle")

	borrowed := b.param("s", env.str, true)
	outerCond := b.local("outer", env.boolTy, false)
	innerCond := b.local("inner", env.boolTy, false)
	loopCond := b.local("again", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	innerValue := b.local("tmp_inner", env.str, true)
	outerValue := b.local("tmp_outer", env.str, true)
	dropped := b.local("tmp_dropped", env.str, true)

	entry := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, false))),
	}, mir.Terminator{})
	innerTest := b.block(nil, mir.Terminator{})
	innerMint := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(innerValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	innerForward := b.block([]mir.Instr{
		assign(innerValue, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	innerJoin := b.block([]mir.Instr{
		assign(outerValue, useRV(opMove(innerValue, env.str))),
	}, mir.Terminator{})
	cycleBack := b.block([]mir.Instr{
		assign(innerValue, useRV(opMove(outerValue, env.str))),
	}, mir.Terminator{})
	outerMint := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(outerValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	outerJoin := b.block([]mir.Instr{
		assign(dropped, useRV(opMove(outerValue, env.str))),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(dropped)}, mir.Terminator{})
	join := b.block(nil, retTerm())

	b.setTerm(entry, ifTerm(opCopy(outerCond, env.boolTy), innerTest, outerMint))
	b.setTerm(innerTest, ifTerm(opCopy(innerCond, env.boolTy), innerMint, innerForward))
	b.setTerm(innerMint, gotoTerm(innerJoin))
	b.setTerm(innerForward, gotoTerm(innerJoin))
	b.setTerm(innerJoin, ifTerm(opCopy(loopCond, env.boolTy), cycleBack, outerJoin))
	b.setTerm(cycleBack, gotoTerm(innerJoin))
	b.setTerm(outerMint, gotoTerm(outerJoin))
	b.setTerm(outerJoin, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
	b.setTerm(dropBB, gotoTerm(join))

	requireFindings(t, env.verify(b.done()),
		"guarded_nested_cycle: drop of L7(tmp_dropped) (def bb3#0) at bb8#0",
		"guarded_nested_cycle: drop of L7(tmp_dropped) (def bb4#0) at bb8#0")
}
