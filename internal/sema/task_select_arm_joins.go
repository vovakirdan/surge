package sema

import "surge/internal/ast"

// A `select` or `race` runs every arm's head before it runs at all (RV2-DEBT-365, R-g). A task a
// head creates -- `(spawn worker(&l)).await()` -- is live whichever arm wins, and only the arm
// that wins has joined the task its own head awaits. So the heads are typed in ONE pin state that
// keeps every pin a head opens, and the join a head performs is released only in that head's arm.
// Each body starts from there, and the join after the select is the usual union: a pin released in
// one arm and kept in another is live after it. The heads used to be typed each from the state
// before the select, which dropped a pin a head opened from every arm but its own.

// trackArmHeadAwait is trackTaskAwait for the join a select arm's head performs: the task is
// joined only where this arm is the one taken, so its pins are written down for this arm's body.
func (tc *typeChecker) trackArmHeadAwait(target ast.ExprID, joins *[]uint32) {
	outer := tc.selectArmJoins
	tc.selectArmJoins = joins
	tc.trackTaskAwait(target)
	tc.selectArmJoins = outer
}

// releaseAwaitedTaskPins ends the pins of a joined task, or writes the task down for the arm whose
// head joins it.
func (tc *typeChecker) releaseAwaitedTaskPins(task uint32) {
	if tc.selectArmJoins != nil {
		*tc.selectArmJoins = append(*tc.selectArmJoins, task)
		return
	}
	tc.releaseTaskBorrowPins(task)
}

// restoreArmPins starts one arm's body: every pin the heads left, less those of the tasks this
// arm's own head joined.
func (tc *typeChecker) restoreArmPins(heads map[taskBorrowPinKey]taskBorrowPin, joins []uint32) {
	tc.restoreTaskBorrowPins(heads)
	for _, task := range joins {
		tc.releaseTaskBorrowPins(task)
	}
}
