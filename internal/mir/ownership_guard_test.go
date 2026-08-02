package mir_test

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/mir"
)

const ownershipGuardRealLoweringSource = `
fn build() -> string {
    return "x";
}

fn peek(x: &string) -> int {
    return 1;
}

fn guarded_choice(cond: bool, borrowed: &string) -> int {
    return peek(cond ? build() : *borrowed);
}

fn nested_inner_forwards(cond: bool, other: bool, a: &string) -> int {
    return peek(cond ? (other ? build() : *a) : build());
}

fn both_branches_nested_mixed(cond: bool, other: bool, a: &string) -> int {
    return peek(cond ? (other ? build() : *a) : (other ? build() : *a));
}
`

func TestOwnershipGuardedDropRealLowering(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipGuardRealLoweringSource)
	findings := mir.VerifyOwnership(mod, typesIn, semaRes)
	for _, fn := range []string{
		"guarded_choice",
		"nested_inner_forwards",
		"both_branches_nested_mixed",
	} {
		t.Run(fn, func(t *testing.T) {
			got := findingsIn(findings, fn)
			if len(got) != 0 {
				t.Fatalf("real lowered mixed choice should be clean, got:\n%s", joinLines(got))
			}
		})
	}
}

// The guarded-drop recognizer, proven on the shape it accepts AND on the
// adversarial shapes it must not.
//
// The negatives matter more than the positive here. A recognizer that fires on
// anything boolean-looking before a drop is worse than none at all: it would
// accept exactly the hand-built "guard" a defect can hide behind, and do it
// silently.
//
// Every fixture below includes real lowering's singleton pure-transfer join
// between the branch-local value and the local that is eventually dropped.
func TestOwnershipGuardedDropRecognizer(t *testing.T) {
	env := newOwnershipEnv(t)

	// build assembles emitGuardedTempDrop's shape:
	//
	//   bb0: G = false; if cond -> bb1 else bb2
	//   bb1: <arm builds R>; G = true;  goto bb3
	//   bb2: <arm forwards a borrow to R>;      goto bb3
	//   bb3: X = move R; if G -> bb4 else bb5
	//   bb4: drop X
	//   bb5: return
	//
	// mintingArmValue is what the guard-raising arm assigns to R; decoy plants
	// an extra assignment to an UNRELATED local next to the guard's true-write.
	build := func(mintingArmValue func(borrowed mir.LocalID) mir.RValue, decoy bool) *mir.Func {
		b := newFn("guarded")
		borrowed := b.param("s", env.str, true)
		cond := b.local("c", env.boolTy, false)
		guard := b.local("owns_temp", env.boolTy, false)
		branchValue := b.local("tmp_branch_value", env.str, true)
		value := b.local("tmp_value", env.str, true)
		unrelated := b.local("decoy", env.str, true)

		mintingArm := []mir.Instr{}
		if decoy {
			// Sits between the guard's true-write and the real assignment, and
			// is exactly the thing textual adjacency would pick up.
			mintingArm = append(mintingArm,
				assign(guard, useRV(opBool(env.boolTy, true))),
				assign(unrelated, useRV(opRetain(borrowed, env.str))),
				assign(branchValue, mintingArmValue(borrowed)),
			)
		} else {
			mintingArm = append(mintingArm,
				assign(guard, useRV(opBool(env.boolTy, true))),
				assign(branchValue, mintingArmValue(borrowed)),
			)
		}

		entry := b.block([]mir.Instr{
			assign(guard, useRV(opBool(env.boolTy, false))),
		}, mir.Terminator{})
		mints := b.block(mintingArm, mir.Terminator{})
		forwards := b.block([]mir.Instr{
			// The arm the guard exists to skip: it hands on storage the caller
			// still owns, and nothing here may release it.
			assign(branchValue, useRV(opCopy(borrowed, env.str))),
		}, mir.Terminator{})
		test := b.block([]mir.Instr{
			assign(value, useRV(opMove(branchValue, env.str))),
		}, mir.Terminator{})
		dropBB := b.block([]mir.Instr{dropL(value)}, mir.Terminator{})
		join := b.block(nil, retTerm())

		b.setTerm(entry, ifTerm(opCopy(cond, env.boolTy), mints, forwards))
		b.setTerm(mints, gotoTerm(test))
		b.setTerm(forwards, gotoTerm(test))
		b.setTerm(test, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
		b.setTerm(dropBB, gotoTerm(join))
		return b.done()
	}

	// The canonical accept: the guard is raised only where the value was built,
	// so the arm that merely forwards a borrow never reaches the drop and does
	// not have to resolve.
	t.Run("canonical_guard_is_accepted", func(t *testing.T) {
		requireClean(t, env.verify(build(mintsFresh(env), false)))
	})

	// The same shape with the guard raised on an ALIASING path. Structurally
	// identical, semantically a defect, and the recognizer has to tell them
	// apart by resolving the definition rather than by recognizing the frame
	// around it.
	t.Run("guard_raised_on_an_aliasing_path_is_rejected", func(t *testing.T) {
		// Once the guard proof fails, ordinary EVERY-definition resolution reports
		// both aliasing roots: the guard-true assignment and the forwarding arm.
		requireFindings(t, env.verify(build(aliasesBorrow(env), false)),
			"guarded: drop of L4(tmp_value) (def bb1#1) at bb4#0",
			"guarded: drop of L4(tmp_value) (def bb2#0) at bb4#0")
	})

	// The decoy attack: a retain of something else, planted between the guard's
	// true-write and the real assignment. Textual adjacency would check the
	// decoy, find it minting, and wave the real aliasing value through.
	//
	// It cannot work here, because the definition being checked is SELECTED
	// from the traced value local's own reaching-definition set — and a decoy
	// defines a different local, so it is not in that set at any position.
	t.Run("a_decoy_next_to_the_guard_cannot_be_mistaken_for_the_value", func(t *testing.T) {
		// bb1#2 is the real assignment to the branch value; the decoy sits at
		// bb1#1 and is never what gets checked.
		requireFindings(t, env.verify(build(aliasesBorrow(env), true)),
			"guarded: drop of L4(tmp_value) (def bb1#2) at bb4#0",
			"guarded: drop of L4(tmp_value) (def bb2#0) at bb4#0")
	})

	// And the decoy must not rescue the guard even when the ARM is fine either:
	// the recognizer's accept has to come from the value's own definition.
	t.Run("a_decoy_does_not_change_an_otherwise_valid_guard", func(t *testing.T) {
		requireClean(t, env.verify(build(mintsFresh(env), true)))
	})
}

// Both edges entering the drop mean the condition guards nothing. predBlocks
// deliberately deduplicates identical CFG edges, so this still has one UNIQUE
// predecessor and must be rejected explicitly by the structural recognizer.
func TestOwnershipGuardedDropRejectsDegenerateBranch(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("degenerate_guard")
	borrowed := b.param("s", env.str, true)
	cond := b.local("c", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	branchValue := b.local("tmp_branch_value", env.str, true)
	value := b.local("tmp_value", env.str, true)

	entry := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, false))),
	}, mir.Terminator{})
	mints := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(branchValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	aliases := b.block([]mir.Instr{
		assign(branchValue, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	test := b.block([]mir.Instr{
		assign(value, useRV(opMove(branchValue, env.str))),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(value)}, retTerm())

	b.setTerm(entry, ifTerm(opCopy(cond, env.boolTy), mints, aliases))
	b.setTerm(mints, gotoTerm(test))
	b.setTerm(aliases, gotoTerm(test))
	b.setTerm(test, ifTerm(opCopy(guard, env.boolTy), dropBB, dropBB))

	requireFindings(t, env.verify(b.done()),
		"degenerate_guard: drop of L4(tmp_value) (def bb2#0) at bb4#0")
}

// Both guard-true paths must survive the singleton join chase. Picking one
// reaching definition would accept this fixture whenever the minting arm was
// encountered first and hide the aliasing arm behind an ANY rule.
func TestOwnershipGuardTransferFrontierPreservesEveryTrueArm(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("guarded_frontier")
	borrowed := b.param("s", env.str, true)
	cond := b.local("c", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	branchValue := b.local("tmp_branch_value", env.str, true)
	value := b.local("tmp_value", env.str, true)

	entry := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, false))),
	}, mir.Terminator{})
	mints := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(branchValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	aliases := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(branchValue, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	test := b.block([]mir.Instr{
		assign(value, useRV(opMove(branchValue, env.str))),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(value)}, mir.Terminator{})
	join := b.block(nil, retTerm())

	b.setTerm(entry, ifTerm(opCopy(cond, env.boolTy), mints, aliases))
	b.setTerm(mints, gotoTerm(test))
	b.setTerm(aliases, gotoTerm(test))
	b.setTerm(test, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
	b.setTerm(dropBB, gotoTerm(join))

	requireFindings(t, env.verify(b.done()),
		"guarded_frontier: drop of L4(tmp_value) (def bb2#1) at bb4#0")
}

// A true guard write and an owned arm definition are not enough on their own:
// every value definition that can reach the guarded drop must still correlate
// with the guard value on that SAME path. Here the minting arm can overwrite
// its value with a borrow after raising the guard. Selecting only the owned
// definition in the guard-true block would silently excuse that later alias.
func TestOwnershipGuardTrueWriteCorrelatesOneToOneWithFrontier(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("guarded_path_correlation")
	borrowed := b.param("s", env.str, true)
	choose := b.local("choose", env.boolTy, false)
	overwrite := b.local("overwrite", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	branchValue := b.local("tmp_branch_value", env.str, true)
	value := b.local("tmp_value", env.str, true)

	entry := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, false))),
	}, mir.Terminator{})
	mints := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(branchValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	falseArm := b.block([]mir.Instr{
		// This arm is owned too, so ordinary EVERY-resolution blames only the
		// adversarial overwrite below when the shortcut correctly refuses it.
		assign(branchValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	aliasesAfterTrue := b.block([]mir.Instr{
		assign(branchValue, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	test := b.block([]mir.Instr{
		assign(value, useRV(opMove(branchValue, env.str))),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(value)}, mir.Terminator{})
	join := b.block(nil, retTerm())

	b.setTerm(entry, ifTerm(opCopy(choose, env.boolTy), mints, falseArm))
	b.setTerm(mints, ifTerm(opCopy(overwrite, env.boolTy), aliasesAfterTrue, test))
	b.setTerm(falseArm, gotoTerm(test))
	b.setTerm(aliasesAfterTrue, gotoTerm(test))
	b.setTerm(test, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
	b.setTerm(dropBB, gotoTerm(join))

	requireFindings(t, env.verify(b.done()),
		"guarded_path_correlation: drop of L5(tmp_value) (def bb3#0) at bb5#0")
}

// A may-reaching set cannot represent an uninitialized path: the path simply
// contributes no definition to the union. The canonical false write therefore
// has to dominate the final guard read, rather than merely be the only false
// definition the query happened to return.
func TestOwnershipGuardRequiresDominatingFalseInitialization(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("guard_without_full_init")
	borrowed := b.param("s", env.str, true)
	chooseInit := b.local("choose_init", env.boolTy, false)
	chooseMint := b.local("choose_mint", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	branchValue := b.local("tmp_branch_value", env.str, true)
	value := b.local("tmp_value", env.str, true)

	entry := b.block(nil, mir.Terminator{})
	initialized := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, false))),
	}, mir.Terminator{})
	mints := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(branchValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	falseArm := b.block([]mir.Instr{
		assign(branchValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	bypassesInit := b.block([]mir.Instr{
		assign(branchValue, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	test := b.block([]mir.Instr{
		assign(value, useRV(opMove(branchValue, env.str))),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(value)}, mir.Terminator{})
	join := b.block(nil, retTerm())

	b.setTerm(entry, ifTerm(opCopy(chooseInit, env.boolTy), initialized, bypassesInit))
	b.setTerm(initialized, ifTerm(opCopy(chooseMint, env.boolTy), mints, falseArm))
	b.setTerm(mints, gotoTerm(test))
	b.setTerm(falseArm, gotoTerm(test))
	b.setTerm(bypassesInit, gotoTerm(test))
	b.setTerm(test, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
	b.setTerm(dropBB, gotoTerm(join))

	requireFindings(t, env.verify(b.done()),
		"guard_without_full_init: drop of L5(tmp_value) (def bb4#0) at bb6#0")
}

// TRANSFERS alone is not enough to enter the shortcut. Unary plus also
// classifies TRANSFERS, but real guarded-temp lowering does not use it for its
// join. Chasing it would silently broaden the recognizer beyond its canonical
// representation and excuse the false arm here.
func TestOwnershipGuardTransferChaseRejectsNonCanonicalTransfers(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("guarded_noncanonical_transfer")
	borrowed := b.param("s", env.str, true)
	cond := b.local("c", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	branchValue := b.local("tmp_branch_value", env.str, true)
	value := b.local("tmp_value", env.str, true)

	entry := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, false))),
	}, mir.Terminator{})
	mints := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(branchValue, useRV(opStr(env.str))),
	}, mir.Terminator{})
	aliases := b.block([]mir.Instr{
		assign(branchValue, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	test := b.block([]mir.Instr{
		assign(value, mir.RValue{Kind: mir.RValueUnaryOp, Unary: mir.UnaryOp{
			Op: ast.ExprUnaryPlus, Operand: opMove(branchValue, env.str),
		}}),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(value)}, mir.Terminator{})
	join := b.block(nil, retTerm())

	b.setTerm(entry, ifTerm(opCopy(cond, env.boolTy), mints, aliases))
	b.setTerm(mints, gotoTerm(test))
	b.setTerm(aliases, gotoTerm(test))
	b.setTerm(test, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
	b.setTerm(dropBB, gotoTerm(join))

	requireFindings(t, env.verify(b.done()),
		"guarded_noncanonical_transfer: drop of L4(tmp_value) (def bb2#0) at bb4#0")
}

// A loop-carried singleton transfer chain must terminate as unresolved. The
// guard shortcut may not turn a rooted or unrooted cycle into evidence of
// ownership; SCC-based acceptance is explicitly outside Step 1.
func TestOwnershipGuardTransferChaseRejectsCycles(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("guarded_transfer_cycle")
	cond := b.local("c", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	value := b.local("tmp_value", env.str, true)
	hop := b.local("tmp_hop", env.str, true)
	dropped := b.local("tmp_dropped", env.str, true)

	entry := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, false))),
	}, mir.Terminator{})
	loop := b.block([]mir.Instr{
		assign(guard, useRV(opBool(env.boolTy, true))),
		assign(value, useRV(opMove(hop, env.str))),
	}, mir.Terminator{})
	back := b.block([]mir.Instr{
		assign(hop, useRV(opMove(value, env.str))),
	}, mir.Terminator{})
	test := b.block([]mir.Instr{
		assign(dropped, useRV(opMove(value, env.str))),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(dropped)}, mir.Terminator{})
	join := b.block(nil, retTerm())

	b.setTerm(entry, gotoTerm(loop))
	b.setTerm(loop, ifTerm(opCopy(cond, env.boolTy), back, test))
	b.setTerm(back, gotoTerm(loop))
	b.setTerm(test, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
	b.setTerm(dropBB, gotoTerm(join))

	got := env.verify(b.done())
	if len(got) != 1 {
		t.Fatalf("cycle must produce one conservative finding, got %d:\n%s", len(got), formatFindings(got))
	}
}

// A guard whose own definitions are not literal bool-const writes is not the
// canonical shape, so the recognizer does not fire and the drop falls through
// to the ordinary query — which is what surfaces the aliasing arm.
func TestOwnershipGuardMustBeWrittenFromBoolLiterals(t *testing.T) {
	env := newOwnershipEnv(t)

	b := newFn("computed_guard")
	borrowed := b.param("s", env.str, true)
	cond := b.local("c", env.boolTy, false)
	guard := b.local("owns_temp", env.boolTy, false)
	value := b.local("tmp_value", env.str, true)

	entry := b.block([]mir.Instr{
		// Not a literal: a copy of another bool. The compiler never emits a
		// guard this way, so trusting it would be trusting a shape nothing
		// vouches for.
		assign(guard, useRV(opCopy(cond, env.boolTy))),
		assign(value, useRV(opCopy(borrowed, env.str))),
	}, mir.Terminator{})
	dropBB := b.block([]mir.Instr{dropL(value)}, mir.Terminator{})
	join := b.block(nil, retTerm())

	b.setTerm(entry, ifTerm(opCopy(guard, env.boolTy), dropBB, join))
	b.setTerm(dropBB, gotoTerm(join))

	requireFindings(t, env.verify(b.done()),
		"computed_guard: drop of L3(tmp_value) (def bb0#1) at bb1#0")
}

// mintsFresh is an arm that BUILT its value: a fresh constant nothing else
// holds.
func mintsFresh(env *ownershipEnv) func(mir.LocalID) mir.RValue {
	return func(mir.LocalID) mir.RValue { return useRV(opStr(env.str)) }
}

// aliasesBorrow is an arm that only read through to the caller's storage, with
// the guard wrongly raised on it anyway.
func aliasesBorrow(env *ownershipEnv) func(mir.LocalID) mir.RValue {
	return func(borrowed mir.LocalID) mir.RValue { return useRV(opCopy(borrowed, env.str)) }
}
