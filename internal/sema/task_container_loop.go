package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
)

// The drain loop's bookkeeping. A `while xs.__len() > 0:uint { let t =
// xs.pop().safe(); ... }` is the one loop the task tracker accepts as a drain
// (taskContainerDrainLoop recognises it); while its body is walked, one of
// these records what the body did with the container -- how many times it
// popped, how many of those pops were consumed, which bindings hold a popped
// task -- and whether the body can leave before the container is empty. That
// last fact is what decides the container's fate at the loop's end: a drain
// left early is no drain, and the exit that left it is what the scope-exit
// refusal names.
type taskContainerLoop struct {
	place       Place
	popCount    int
	popConsumed int
	earlyExit   bool
	exit        source.Span // the first statement that left the loop early; zero until one did
	exitKind    string
	bodyDepth   int // loop nesting depth of this loop's own body; see loopNestingDepth
	popBindings []taskContainerPopBinding
}

type taskContainerPopBinding struct {
	symID    symbols.SymbolID
	span     source.Span
	consumed bool
}

// markEarlyExit records that the loop can be left before its condition turns
// false, keeping the FIRST exit as the one the refusal will name.
func (loop *taskContainerLoop) markEarlyExit(span source.Span, kind string) {
	loop.earlyExit = true
	if loop.exit == (source.Span{}) {
		loop.exit = span
		loop.exitKind = kind
	}
}

func (tc *typeChecker) taskContainerLoopAllowsAwait(info *taskContainerInfo) bool {
	if info == nil || len(tc.taskContainerLoops) == 0 {
		return false
	}
	for i := len(tc.taskContainerLoops) - 1; i >= 0; i-- {
		loop := tc.taskContainerLoops[i]
		if loop.popCount == 0 {
			continue
		}
		if existing := tc.taskContainers[loop.place]; existing == info {
			return true
		}
	}
	return false
}

// loopNestingDepth is how many loop bodies enclose the statement being walked.
// Every loop the language has -- `while`, the classic `for`, `for`-in -- pushes
// exactly one loop drop mark around its body and pops it after, so that stack
// IS the loop nesting. The drain tracker reads it because its OWN stack holds
// only drain loops: without a count of the loops in between, a `break` two
// levels down looks exactly like a `break` of the drain itself.
func (tc *typeChecker) loopNestingDepth() int {
	return len(tc.loopDropMarks)
}

// enterTaskContainerLoop is called from inside the loop's drop-mark region, so
// the depth it reads is the depth the body about to be walked will report.
func (tc *typeChecker) enterTaskContainerLoop(place Place) {
	if !place.IsValid() {
		return
	}
	tc.taskContainerLoops = append(tc.taskContainerLoops, taskContainerLoop{
		place:     place,
		bodyDepth: tc.loopNestingDepth(),
	})
}

func (tc *typeChecker) leaveTaskContainerLoop() (*taskContainerLoop, bool) {
	if len(tc.taskContainerLoops) == 0 {
		return nil, false
	}
	idx := len(tc.taskContainerLoops) - 1
	loop := tc.taskContainerLoops[idx]
	tc.taskContainerLoops = tc.taskContainerLoops[:idx]
	tc.noteTaskContainerDrainAbandoned(&loop)
	return &loop, true
}

// noteTaskContainerDrainAbandoned hands the loop's early exit to the container
// it was draining, so the scope-exit refusal can point at it. Only a loop that
// POPPED is a drain the exit abandons; a loop that merely tested the length
// left the container exactly as it found it, and the refusal stays at the
// container.
func (tc *typeChecker) noteTaskContainerDrainAbandoned(loop *taskContainerLoop) {
	if !loop.earlyExit || loop.popCount == 0 || loop.exit == (source.Span{}) {
		return
	}
	info := tc.taskContainers[loop.place]
	if info == nil || !info.Pending || info.Exit != (source.Span{}) {
		return
	}
	info.Exit = loop.exit
	info.ExitKind = loop.exitKind
}

func (tc *typeChecker) noteTaskContainerPop(place Place) {
	if !place.IsValid() {
		return
	}
	for i := len(tc.taskContainerLoops) - 1; i >= 0; i-- {
		if tc.taskContainerLoops[i].place == place {
			tc.taskContainerLoops[i].popCount++
			return
		}
	}
}

func (tc *typeChecker) noteTaskContainerPopBinding(place Place, symID symbols.SymbolID, span source.Span) {
	if !place.IsValid() || !symID.IsValid() {
		return
	}
	for i := len(tc.taskContainerLoops) - 1; i >= 0; i-- {
		if tc.taskContainerLoops[i].place != place {
			continue
		}
		for _, binding := range tc.taskContainerLoops[i].popBindings {
			if binding.symID == symID {
				return
			}
		}
		tc.taskContainerLoops[i].popBindings = append(tc.taskContainerLoops[i].popBindings, taskContainerPopBinding{
			symID: symID,
			span:  span,
		})
		return
	}
}

func (tc *typeChecker) taskContainerLoopDrained(loop *taskContainerLoop) bool {
	if loop.popCount == 0 {
		return false
	}
	return loop.popConsumed >= loop.popCount
}

// noteTaskContainerLoopBreak marks the innermost drain loop as leavable at
// this `break` -- but only when that drain loop is the loop the `break`
// actually leaves. A `break` inside a plain `while` or `for` nested in the
// drain body leaves the INNER loop; the drain keeps running to its own
// condition and empties the container, so nothing about it was abandoned.
// Blaming it there stated something false about a statement: "this `break`
// leaves the drain of `tasks` unfinished", of a `break` that never left the
// drain, with advice to move it after a `while` it is not in.
//
// `continue` is not recorded at all, at any depth: it goes back to the loop's
// condition, so the drain it belongs to still finishes.
//
// noteTaskContainerLoopReturn marks every enclosing drain loop, since a
// `return` does leave them all, however deep it is nested.
func (tc *typeChecker) noteTaskContainerLoopBreak(span source.Span) {
	if len(tc.taskContainerLoops) == 0 {
		return
	}
	loop := &tc.taskContainerLoops[len(tc.taskContainerLoops)-1]
	if loop.bodyDepth != tc.loopNestingDepth() {
		return
	}
	loop.markEarlyExit(span, "break")
}

func (tc *typeChecker) noteTaskContainerLoopReturn(span source.Span) {
	for i := range tc.taskContainerLoops {
		tc.taskContainerLoops[i].markEarlyExit(span, "return")
	}
}
