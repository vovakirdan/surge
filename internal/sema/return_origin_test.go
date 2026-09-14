package sema

import (
	"context"
	"errors"
	"slices"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
)

// The fixture has function scope 1, nested scopes 2/3, and sibling scope 4.
func returnOriginTestWithin(child, ancestor symbols.ScopeID) bool {
	parents := map[symbols.ScopeID]symbols.ScopeID{2: 1, 3: 2, 4: 1}
	for child != symbols.NoScopeID {
		if child == ancestor {
			return true
		}
		child = parents[child]
	}
	return false
}

func requireReturnOriginRoot(t *testing.T, value returnOriginValue, root returnOrigin) {
	t.Helper()
	if !value.normal || !slices.Contains(value.roots, root) {
		t.Fatalf("normal=%t roots=%+v, missing %+v", value.normal, value.roots, root)
	}
}

func TestReturnOriginValuesDistinguishUnknownRefFreeAndNoReturn(t *testing.T) {
	noReturn := returnOriginValue{}
	refFree := returnOriginValueOf()
	unknown := returnOriginValueOf(returnOrigin{kind: returnOriginUnknown})
	if noReturn.normal || !refFree.normal || !unknown.normal {
		t.Fatal("normal reachability conflates unknown, ref-free, and no return")
	}
	if noReturn.equal(refFree) || refFree.equal(unknown) || noReturn.equal(unknown) {
		t.Fatal("distinct source facts compare equal")
	}
	if got := noReturn.join(refFree); !got.normal || len(got.roots) != 0 {
		t.Fatalf("only normally returning arm is reference-free: %+v", got)
	}
	requireReturnOriginRoot(t, refFree.join(unknown), returnOrigin{kind: returnOriginUnknown})
	if newReturnOriginEnv().value(99).equal(refFree) {
		t.Fatal("missing binding was treated as a proven reference-free value")
	}
}

func TestReturnOriginBranchUnionDoesNotAliasItsInputs(t *testing.T) {
	left := returnOrigin{kind: returnOriginParam, param: 0}
	right := returnOrigin{kind: returnOriginParam, param: 1}
	input := []returnOrigin{right, left, right}
	value := returnOriginValueOf(input...)
	input[0] = returnOrigin{kind: returnOriginUnknown}
	if len(value.roots) != 2 || value.roots[0] != left || value.roots[1] != right {
		t.Fatalf("source roots lost canonical identity or input ownership: %+v", value)
	}
	base := newReturnOriginEnv().assign(1, 1, returnOriginValueOf(left))
	branch := base.assign(1, 1, returnOriginValueOf(right))
	joined := base.join(branch)
	requireReturnOriginRoot(t, joined.value(1), left)
	requireReturnOriginRoot(t, joined.value(1), right)
	read := joined.value(1)
	read.roots[0] = returnOrigin{kind: returnOriginUnknown}
	if len(base.value(1).roots) != 1 || base.value(1).roots[0] != left || len(joined.value(1).roots) != 2 {
		t.Fatal("join or detached lookup mutated a predecessor")
	}
	requireReturnOriginRoot(t, joined.value(1), left)
}

func TestReturnOriginRebindReplacesTheOldReference(t *testing.T) {
	old := returnOrigin{kind: returnOriginLocal, binding: 7, scope: 2}
	fresh := returnOrigin{kind: returnOriginParam, param: 0}
	before := newReturnOriginEnv().assign(1, 1, returnOriginValueOf(old))
	after := before.assign(1, 1, returnOriginValueOf(fresh))
	if len(after.value(1).roots) != 1 || after.value(1).roots[0] != fresh {
		t.Fatalf("strong update retained the overwritten reference: %+v", after.value(1))
	}
	if cleared := after.assign(1, 1, returnOriginValueOf()); len(cleared.value(1).roots) != 0 {
		t.Fatal("a normal reference-free RHS did not replace the previous value")
	}
	if stopped := after.assign(1, 1, returnOriginValue{}); stopped.reachable || len(stopped.bindings) != 0 {
		t.Fatal("nonreturning RHS fabricated a continuation")
	}
	requireReturnOriginRoot(t, before.value(1), old)
}

func TestReturnOriginScopeExitFindsResultAndOuterAssignment(t *testing.T) {
	owner := returnOrigin{kind: returnOriginLocal, binding: 9, scope: 3}
	expired := owner
	expired.expired = true
	base := newReturnOriginEnv().assign(9, 3, returnOriginValueOf())
	result := base.leaveScope(2, returnOriginValueOf(owner), returnOriginTestWithin)
	if len(result.expired) != 1 || result.expired[0] != expired {
		t.Fatalf("block result lost its dying owner: %+v", result)
	}
	outer := base.assign(1, 1, returnOriginValueOf(owner))
	escape := outer.leaveScope(2, returnOriginValueOf(), returnOriginTestWithin)
	if len(escape.expired) != 1 || escape.expired[0] != expired {
		t.Fatalf("reference-free result hid the outer-binding side effect: %+v", escape)
	}
	requireReturnOriginRoot(t, escape.env.value(1), expired)
	if _, present := escape.env.bindings[9]; present {
		t.Fatal("scope-local binding survived scope exit")
	}
	localOnly := base.assign(8, 2, returnOriginValueOf(owner))
	dead := localOnly.leaveScope(2, returnOriginValueOf(), returnOriginTestWithin)
	if len(dead.expired) != 0 || len(dead.env.bindings) != 0 {
		t.Fatal("reference held only in dying locals was called an outgoing escape")
	}
}

func TestReturnOriginParameterStorageIsNotIncomingBorrowedContent(t *testing.T) {
	storage := returnOrigin{kind: returnOriginLocal, binding: 5, scope: 1}
	incoming := returnOrigin{kind: returnOriginParam, binding: 5, param: 0}
	entry := newReturnOriginEnv().assign(5, 1, returnOriginValueOf(incoming))
	byValueAddress := entry.leaveScope(1, returnOriginValueOf(storage), returnOriginTestWithin)
	if len(byValueAddress.expired) != 1 {
		t.Fatal("address of by-value parameter storage escaped its function")
	}
	borrowedContent := entry.leaveScope(1, returnOriginValueOf(incoming), returnOriginTestWithin)
	if len(borrowedContent.expired) != 0 {
		t.Fatal("incoming external borrow died with the parameter's own slot")
	}
	requireReturnOriginRoot(t, borrowedContent.value, incoming)
}

func TestReturnOriginNewIterationCannotReviveAnEscapedOwner(t *testing.T) {
	owner := returnOrigin{kind: returnOriginLocal, binding: 9, scope: 2}
	first := newReturnOriginEnv().assign(1, 1, returnOriginValueOf(owner))
	out := first.leaveScope(2, returnOriginValueOf(), returnOriginTestWithin)
	second := out.env.assign(9, 2, returnOriginValueOf())
	second = second.assign(2, 1, returnOriginValueOf(owner))
	old := owner
	old.expired = true
	requireReturnOriginRoot(t, second.value(1), old)
	requireReturnOriginRoot(t, second.value(2), owner)
	if second.value(1).equal(second.value(2)) {
		t.Fatal("a new owner incarnation revived the previous reference")
	}
}

func TestReturnOriginJoinRemovesBranchLocalsBeforeMissingFacts(t *testing.T) {
	root := returnOrigin{kind: returnOriginParam, param: 0}
	base := newReturnOriginEnv().assign(1, 1, returnOriginValueOf(root))
	left := base.assign(2, 2, returnOriginValueOf(root))
	closed := left.leaveScope(2, returnOriginValueOf(), returnOriginTestWithin)
	joined := closed.env.join(base)
	if _, present := joined.bindings[2]; present {
		t.Fatal("a dead branch-local binding manufactured an unknown outer binding")
	}
	missingVisible := base.join(newReturnOriginEnv())
	requireReturnOriginRoot(t, missingVisible.value(1), root)
	requireReturnOriginRoot(t, missingVisible.value(1), returnOrigin{kind: returnOriginUnknown})
	if !base.join(returnOriginEnv{}).equal(base) {
		t.Fatal("unreachable predecessor manufactured an unknown source")
	}
}

func TestReturnOriginSequentialFlowDoesNotVisitAfterReturn(t *testing.T) {
	key := returnOriginExit{kind: returnOriginFunctionReturn, target: 1, site: source.Span{Start: 10, End: 12}}
	flow := returnOriginFlow{normal: newReturnOriginEnv()}.end(key, returnOriginValueOf())
	got, err := flow.then(func(returnOriginEnv) (returnOriginFlow, error) {
		t.Fatal("visited a statement after unconditional return")
		return returnOriginFlow{}, nil
	})
	if err != nil || got.normal.reachable || len(got.exits) != 1 {
		t.Fatalf("return outcome was lost: %+v, %v", got, err)
	}
	nonreturn := (returnOriginFlow{normal: newReturnOriginEnv()}).end(key, returnOriginValue{})
	if nonreturn.normal.reachable || len(nonreturn.exits) != 0 {
		t.Fatal("nonreturning return operand fabricated a normal return")
	}
}

func TestReturnOriginFlowKeepsTargetsAndSourceSitesDistinct(t *testing.T) {
	base := returnOriginFlow{normal: newReturnOriginEnv()}
	keys := []returnOriginExit{
		{kind: returnOriginFunctionReturn, target: 1, site: source.Span{Start: 1}},
		{kind: returnOriginFunctionReturn, target: 1, site: source.Span{Start: 2}},
		{kind: returnOriginBlockResult, target: 2, site: source.Span{Start: 2}},
		{kind: returnOriginBlockResult, target: 3, site: source.Span{Start: 2}},
	}
	var joined returnOriginFlow
	for _, key := range keys {
		joined = joined.join(base.end(key, returnOriginValueOf()))
	}
	if joined.normal.reachable || len(joined.exits) != len(keys) {
		t.Fatalf("different source exits collapsed: %+v", joined)
	}
	for _, key := range keys {
		if !joined.exits[key].env.reachable || !joined.exits[key].value.normal {
			t.Fatalf("missing normal exit outcome at %+v", key)
		}
	}
}

func TestReturnOriginLoopPropagatesALaterBackedgeEscape(t *testing.T) {
	input := returnOrigin{kind: returnOriginParam, param: 0}
	owner := returnOrigin{kind: returnOriginLocal, binding: 9, scope: 2}
	entry := newReturnOriginEnv().assign(1, 1, returnOriginValueOf(input))
	entry = entry.assign(2, 1, returnOriginValueOf(input))
	visits := 0
	flow, err := solveReturnOriginLoop(context.Background(), entry, 2, func(header returnOriginEnv) (returnOriginLoopStep, error) {
		visits++
		if visits > 8 {
			t.Fatal("finite reference loop did not converge")
		}
		body := header.assign(2, 1, header.value(1))
		body = body.assign(1, 1, returnOriginValueOf(owner))
		closed := body.leaveScope(2, returnOriginValueOf(), returnOriginTestWithin)
		return returnOriginLoopStep{done: header, body: returnOriginFlow{normal: closed.env}}, nil
	})
	if err != nil || visits < 3 {
		t.Fatalf("loop did not follow later backedges: visits=%d err=%v", visits, err)
	}
	expired := owner
	expired.expired = true
	requireReturnOriginRoot(t, flow.normal.value(2), input)
	requireReturnOriginRoot(t, flow.normal.value(2), expired)
	if len(flow.exits) != 0 {
		t.Fatal("normal loop produced an abrupt exit")
	}
}

func TestReturnOriginLoopRoutesOnlyItsOwnContinueBack(t *testing.T) {
	input := returnOrigin{kind: returnOriginParam, param: 0}
	continued := returnOrigin{kind: returnOriginParam, param: 1}
	broken := returnOrigin{kind: returnOriginLocal, binding: 9, scope: 2, expired: true}
	outer := returnOrigin{kind: returnOriginCapture, binding: 8}
	entry := newReturnOriginEnv().assign(1, 1, returnOriginValueOf(input))
	returnKey := returnOriginExit{kind: returnOriginFunctionReturn, target: 1}
	outerKey := returnOriginExit{kind: returnOriginContinue, target: 4}
	visits := 0
	flow, err := solveReturnOriginLoop(context.Background(), entry, 2, func(header returnOriginEnv) (returnOriginLoopStep, error) {
		visits++
		if visits > 4 || slices.Contains(header.value(1).roots, broken) || slices.Contains(header.value(1).roots, outer) {
			t.Fatal("break/outer-continue polluted the loop header")
		}
		branch := func(root returnOrigin, key returnOriginExit) returnOriginFlow {
			env := header.assign(1, 1, returnOriginValueOf(root))
			return (returnOriginFlow{normal: env}).end(key, returnOriginValueOf())
		}
		body := branch(continued, returnOriginExit{kind: returnOriginContinue, target: 2})
		body = body.join(branch(broken, returnOriginExit{kind: returnOriginBreak, target: 2}))
		body = body.join(branch(outer, outerKey))
		body = body.join(branch(input, returnKey))
		return returnOriginLoopStep{body: body}, nil
	})
	if err != nil || visits != 2 || len(flow.exits) != 2 {
		t.Fatalf("wrong loop convergence/routing: visits=%d flow=%+v err=%v", visits, flow, err)
	}
	requireReturnOriginRoot(t, flow.normal.value(1), broken)
	requireReturnOriginRoot(t, flow.exits[outerKey].env.value(1), outer)
	requireReturnOriginRoot(t, flow.exits[returnKey].env.value(1), input)
}

func TestReturnOriginLoopStrongUpdateDoesNotKeepOverwrittenSource(t *testing.T) {
	old := returnOrigin{kind: returnOriginLocal, binding: 9, scope: 2, expired: true}
	fresh := returnOrigin{kind: returnOriginParam, param: 0}
	entry := newReturnOriginEnv().assign(1, 1, returnOriginValueOf(old))
	visits := 0
	flow, err := solveReturnOriginLoop(context.Background(), entry, 2, func(header returnOriginEnv) (returnOriginLoopStep, error) {
		visits++
		if visits > 3 {
			t.Fatal("strong-update loop did not converge")
		}
		body := returnOriginFlow{normal: header.assign(1, 1, returnOriginValueOf(fresh))}
		continued := body.end(returnOriginExit{kind: returnOriginContinue, target: 2}, returnOriginValueOf())
		broken := body.end(returnOriginExit{kind: returnOriginBreak, target: 2}, returnOriginValueOf())
		return returnOriginLoopStep{body: continued.join(broken)}, nil
	})
	if err != nil || visits != 2 || !flow.normal.value(1).equal(returnOriginValueOf(fresh)) {
		t.Fatalf("loop transfer revived an overwritten source: visits=%d value=%+v err=%v", visits, flow.normal.value(1), err)
	}
}

func TestReturnOriginInfiniteLoopHasNoNormalContinuation(t *testing.T) {
	flow, err := solveReturnOriginLoop(context.Background(), newReturnOriginEnv(), 2, func(header returnOriginEnv) (returnOriginLoopStep, error) {
		return returnOriginLoopStep{body: returnOriginFlow{normal: header}}, nil
	})
	if err != nil || flow.normal.reachable || len(flow.exits) != 0 {
		t.Fatalf("definitely-infinite loop fabricated a continuation: %+v, %v", flow, err)
	}
}

func TestReturnOriginTransfersPropagateFailureAndCancellation(t *testing.T) {
	want := errors.New("typed transfer failed")
	flow, err := (returnOriginFlow{normal: newReturnOriginEnv()}).then(func(returnOriginEnv) (returnOriginFlow, error) {
		return returnOriginFlow{}, want
	})
	if !errors.Is(err, want) || flow.normal.reachable {
		t.Fatalf("sequential failure was converted into a safe result: %+v, %v", flow, err)
	}
	flow, err = solveReturnOriginLoop(context.Background(), newReturnOriginEnv(), 2, func(returnOriginEnv) (returnOriginLoopStep, error) {
		return returnOriginLoopStep{}, want
	})
	if !errors.Is(err, want) || flow.normal.reachable {
		t.Fatalf("loop transfer failure was converted into a safe result: %+v, %v", flow, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	flow, err = solveReturnOriginLoop(ctx, newReturnOriginEnv(), 2, func(returnOriginEnv) (returnOriginLoopStep, error) {
		t.Fatal("entered loop transfer after cancellation")
		return returnOriginLoopStep{}, nil
	})
	if !errors.Is(err, context.Canceled) || flow.normal.reachable {
		t.Fatalf("cancellation was converted into a safe result: %+v, %v", flow, err)
	}
}

func TestReturnOriginMissingCallbacksAreNotSafeDefaults(t *testing.T) {
	cases := map[string]func(){
		"scope": func() { newReturnOriginEnv().leaveScope(1, returnOriginValueOf(), nil) },
		"sequence": func() { _, _ = (returnOriginFlow{}).then(nil) },
		"loop": func() { _, _ = solveReturnOriginLoop(context.Background(), returnOriginEnv{}, 1, nil) },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("missing required callback silently accepted")
				}
			}()
			run()
		})
	}
}
