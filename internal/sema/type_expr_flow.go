package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) typeExprCompare(id ast.ExprID, span source.Span) types.TypeID {
	cmp, ok := tc.builder.Exprs.Compare(id)
	if !ok || cmp == nil {
		return types.NoTypeID
	}
	movedBefore := tc.snapshotMovedPlaces()
	movedArms := make([]map[Place]source.Span, len(cmp.Arms))
	armClosed := make([]bool, len(cmp.Arms))
	valueType := tc.typeExpr(cmp.Value)
	// A compare INSPECTS its subject; it does not take it. That is what makes
	// `compare *arg { ... }` legitimate where `let b = *arg` is not, and the
	// flag says so for exactly this one expression rather than for anything
	// nested inside it. What an ARM does with a payload bound out of a borrowed
	// subject is a separate question, asked below.
	tc.observingCompareScrutinee = true
	tc.observeMove(cmp.Value, tc.exprSpan(cmp.Value))
	tc.observingCompareScrutinee = false
	movedAfterValue := tc.snapshotMovedPlaces()
	expectedCompare := tc.expectedTypeForExpr(id)
	resultType := expectedCompare
	remainingMembers := tc.unionMembers(valueType)
	nothingType := types.NoTypeID
	if tc.types != nil {
		nothingType = tc.types.Builtins().Nothing
	}
	armTypes := make([]types.TypeID, len(cmp.Arms))
	// A payload BOUND by an arm's pattern is a droppable this arm owns, and
	// until now nobody owned it: only the `let` walk reaches
	// registerDroppableBinding, so a heap payload bound and used locally was
	// abandoned. It is collected per arm rather than per function because that
	// is where it dies — see armPayloadDroppables.
	armPayloadDrops := make([][]symbols.SymbolID, len(cmp.Arms))
	// Read inside the arm loop, not in the recording loop below: a residual
	// plan is a question about THIS arm's moved-set, and by the time the arms
	// are merged the answer would be the union's.
	armResidualPlans := make([]map[symbols.SymbolID][]DropStep, len(cmp.Arms))
	// Asked once, of the subject, because it decides the same thing for every
	// arm: a compare that only READS its union owns none of what it takes out.
	subjectBorrowed := tc.compareSubjectIsBorrowed(cmp.Value, valueType)
	compareDiscarded := tc.isExprDiscarded(id)
	// Who releases the compare's own value, by the same three answers a ternary
	// asks of its branches — see noteChoiceOwnsItsValue. Arms that MINT on
	// every path are collected because they raise the guard where they stand;
	// an arm that mints only SOMETIMES obliges the guard without raising it.
	mintingArms := make([]ast.ExprID, 0, len(cmp.Arms))
	sometimesMintingArms := 0
	valueArms := 0

	for i, arm := range cmp.Arms {
		tc.restoreMovedPlaces(movedAfterValue)
		armSubject := valueType
		if narrowed := tc.narrowCompareSubjectType(valueType, remainingMembers); narrowed != types.NoTypeID {
			armSubject = narrowed
		}
		var armBindings []symbols.SymbolID
		// The scope opens BEFORE the pattern is typed so the bindings it
		// introduces land in it, and closes after the arm's result so anything
		// the result moved out is already recorded when the obligations are read.
		tc.pushDropScope(false)
		tc.inferComparePatternTypes(arm.Pattern, armSubject, &armBindings)
		tc.registerComparePayloadDroppables(
			armBindings,
			subjectBorrowed,
			tc.compareTupleElementsAreBorrowed(cmp.Value, arm.Pattern, armSubject),
		)
		if arm.Guard.IsValid() {
			// A guard runs BEFORE this arm commits (payload extraction
			// already ran, but a failed guard falls through to the next
			// arm against the SAME scrutinee) — moving one of this arm's
			// own pattern bindings out of the guard would free storage
			// the next arm (or this fix's own deep-drop release) still
			// needs. Guards may only borrow; see reportCompareGuardMove.
			tc.pushCompareGuardBindings(armBindings)
			tc.ensureBoolContext(arm.Guard, tc.exprSpan(arm.Guard))
			tc.popCompareGuardBindings(armBindings)
		}
		if compareDiscarded {
			tc.pushDiscardedExpr(arm.Result)
		}
		armResult := tc.typeExprWithExpected(arm.Result, expectedCompare)
		// The other half of the shared-borrow rule. The scrutinee was allowed
		// through because a compare only inspects its subject; an arm that
		// ANSWERS with its own payload binding is where a borrowed value would
		// escape, and that is refused here instead.
		if subjectBorrowed {
			tc.rejectArmHandingOutBorrowedPayload(arm.Result, armBindings, armResult)
		}
		// Asked BEFORE consuming, because consuming is what erases the evidence:
		// an arm that FORWARDS a place was never a temp candidate, and one such
		// arm is enough to leave the compare's value someone else's to release.
		armMints := tc.branchMintsItsValue(arm.Result)
		armSometimesMints := !armMints && tc.branchSometimesMintsItsValue(arm.Result)
		tc.consumeTempCandidate(arm.Result)
		if compareDiscarded {
			tc.popDiscardedExpr()
		}
		armAbrupt := tc.compareArmAbruptExit(arm.Result)
		armClosed[i] = armAbrupt
		if !armAbrupt {
			valueArms++
			switch {
			case armMints:
				mintingArms = append(mintingArms, arm.Result)
			case armSometimesMints:
				sometimesMintingArms++
			}
		}
		armTypes[i] = armResult
		if !armAbrupt && armResult != types.NoTypeID {
			if expectedCompare != types.NoTypeID {
				tc.ensureBindingTypeMatch(ast.NoTypeID, expectedCompare, armResult, arm.Result)
			} else {
				switch {
				case resultType == types.NoTypeID:
					resultType = armResult
				case nothingType != types.NoTypeID && resultType == nothingType:
					resultType = armResult
				case nothingType != types.NoTypeID && armResult == nothingType:
				case tc.typesAssignable(resultType, armResult, true):
				case tc.typesAssignable(armResult, resultType, true):
					resultType = armResult
				default:
					tc.report(diag.SemaTypeMismatch, tc.exprSpan(arm.Result), "compare arm type mismatch: expected %s, got %s", tc.typeLabel(resultType), tc.typeLabel(armResult))
				}
			}
		}
		if len(remainingMembers) > 0 {
			remainingMembers = tc.consumeCompareMembers(remainingMembers, arm)
		}
		// Read before the scope closes, and only what the arm still holds: a
		// payload the result moved onward has a new owner and is skipped by
		// liveDroppables. An arm that exits abruptly has already had these
		// collected by its own return/ret, which walks the open scopes.
		if !armAbrupt {
			armPayloadDrops[i] = tc.liveDroppables(&tc.dropScopes[len(tc.dropScopes)-1])
			// An arm whose result IS one of its own payload bindings hands that
			// value to the compare, so the arm must not also free it — and the
			// compare must, which is what MINTING says here. Both halves are
			// required together: leaving the drop behind frees the value while
			// the compare's own reader still holds it, and dropping it without
			// the mint abandons the value on that path.
			if kept, handedOut := tc.armHandsOutItsPayload(arm.Result, armBindings, armPayloadDrops[i]); handedOut {
				armPayloadDrops[i] = kept
				if !armMints && !armSometimesMints {
					mintingArms = append(mintingArms, arm.Result)
				}
			}
			// A binding the arm still owes a drop for may have had a PART of
			// it given away — `own x.name` out of a payload binding. Asking
			// for the residual is what narrows the drop to what is left; every
			// sibling obligation channel asks, and this one never did, so the
			// arm kept a whole-binding drop and freed the moved field a second
			// time. The VM survived that only because its move-out physically
			// empties the slot, which made a wrong obligation look right on
			// one backend.
			armResidualPlans[i] = tc.residualDropPlansFor(armPayloadDrops[i])
		}
		tc.popDropScope()
		movedArms[i] = tc.snapshotMovedPlaces()
	}

	if owned := len(mintingArms) + sometimesMintingArms; owned > 0 {
		tc.markChoiceOwnsItsValue(id)
		if sometimesMintingArms > 0 || len(mintingArms) != valueArms {
			// The arms disagree, so the release has to ask which one ran.
			if tc.result.ChoiceReleaseGuards == nil {
				tc.result.ChoiceReleaseGuards = make(map[ast.ExprID][]ast.ExprID)
			}
			tc.result.ChoiceReleaseGuards[id] = mintingArms
		}
	}

	targetCompare := resultType
	if expectedCompare != types.NoTypeID {
		targetCompare = expectedCompare
	}
	if expectedCompare == types.NoTypeID && targetCompare != types.NoTypeID && !tc.isExprDiscarded(id) && (nothingType == types.NoTypeID || targetCompare != nothingType) {
		for i, arm := range cmp.Arms {
			if armClosed[i] || armTypes[i] != nothingType {
				continue
			}
			tc.ensureBindingTypeMatch(ast.NoTypeID, targetCompare, armTypes[i], arm.Result)
		}
	}

	// Union across the arms: a value moved on ANY reachable arm is moved after
	// the compare, because a later use has to be rejected if some path gave it
	// away. An intersection was computed here alongside the union and never
	// read; it encoded "moved on every arm", which is not the condition a use
	// is checked against, so it was deleted rather than carried to places.
	var mergedMoves map[Place]source.Span
	for i := range cmp.Arms {
		if armClosed[i] {
			continue
		}
		if mergedMoves == nil {
			mergedMoves = movedArms[i]
			continue
		}
		mergedMoves = mergeMovedPlaces(mergedMoves, movedArms[i])
	}
	// Per-arm drop synthesis, of two kinds that land on the same channel.
	//
	// The arm's own PAYLOAD bindings die here whatever the other arms did, so
	// they are emitted even when no arm moved anything. On top of that, a
	// droppable moved on some arms but live on this one drops at this arm's end
	// rather than being an error; after the join every such binding is in the
	// union moved-set, so using it stays a use-of-moved.
	for i := range cmp.Arms {
		if armClosed[i] {
			continue
		}
		drops := armPayloadDrops[i]
		var plans map[symbols.SymbolID][]DropStep
		if mergedMoves != nil {
			oneSided, oneSidedPlans := tc.oneSidedObligations(movedArms[i], mergedMoves)
			drops = append(drops, oneSided...)
			plans = oneSidedPlans
		}
		if len(drops) == 0 {
			continue
		}
		if tc.result.ArmDropsExpr == nil {
			tc.result.ArmDropsExpr = make(map[ast.ExprID][]symbols.SymbolID)
		}
		tc.result.ArmDropsExpr[cmp.Arms[i].Result] = drops
		// The arm's own residuals go down first, so a one-sided plan for the
		// same binding still wins: that one answers for a binding this arm
		// kept whole and another arm gave away, which is a different question.
		tc.recordOneSidedDrops(DropSite{Expr: cmp.Arms[i].Result}, armResidualPlans[i])
		tc.recordOneSidedDrops(DropSite{Expr: cmp.Arms[i].Result}, plans)
	}
	if mergedMoves == nil {
		tc.movedPlaces = movedBefore
	} else {
		tc.movedPlaces = mergedMoves
	}
	if expectedCompare == types.NoTypeID && resultType != types.NoTypeID {
		for i, arm := range cmp.Arms {
			tc.recordNumericWidening(arm.Result, armTypes[i], resultType)
		}
	}
	tc.checkCompareExhausiveness(cmp, valueType, span)
	return resultType
}

func (tc *typeChecker) taskBlockPayload(span source.Span, body ast.StmtID, async bool) types.TypeID {
	var returns []collectedResult
	tc.pushReturnContext(returnCtxTaskPayload, types.NoTypeID, span, &returns, nil)
	if async {
		tc.awaitDepth++
		tc.asyncBlockDepth++
	}
	tc.walkStmt(body)
	if async {
		tc.asyncBlockDepth--
		tc.awaitDepth--
	}
	tc.popReturnContext()

	payload := tc.types.Builtins().Nothing
	for _, result := range returns {
		rt := result.typ
		if rt == types.NoTypeID {
			continue
		}
		if payload == tc.types.Builtins().Nothing {
			payload = rt
			continue
		}
		if !tc.typesAssignable(payload, rt, true) && !tc.typesAssignable(rt, payload, true) {
			payload = types.NoTypeID
		}
	}
	if payload == types.NoTypeID {
		payload = tc.types.Builtins().Nothing
	}
	return tc.taskType(payload, span)
}

func (tc *typeChecker) typeExprAsync(id ast.ExprID, span source.Span) types.TypeID {
	asyncData, ok := tc.builder.Exprs.Async(id)
	if !ok || asyncData == nil {
		return types.NoTypeID
	}
	// Three things have to happen together here, and any two of them without the
	// third is a leak or a double free.
	//
	// The barrier comes first. The body becomes its own function, so its exits
	// must stop at this boundary rather than at the enclosing one; without it the
	// block's `return` keeps collecting the caller's droppables, which MIR
	// discards today only because the captured symbols do not resolve to
	// anything. They are about to resolve.
	tc.pushDropScope(true)
	// Then the body is given the obligation for what moves into it, BEFORE it is
	// walked, so a `ret` inside sees the capture as a live one.
	tc.registerAsyncBodyOwnership(asyncData.Body)
	resultType := tc.taskBlockPayload(span, asyncData.Body, true)
	tc.popDropScope()

	captures := tc.collectBlockingCaptures(asyncData.Body)
	tc.recordAsyncCaptures(id, captures)
	// A borrow captured by a LOCAL async block is legal — parent and child share
	// a shard (docs/RUNTIME_V2.md, "Structured Concurrency"), and `spawn f(&s)`
	// already accepts exactly that. What is not legal is a borrow laundered
	// through a binding, and the spawn scan is the rule that says so, so the
	// block asks it the same question rather than inventing a narrower one.
	//
	// It is asked about the CAPTURES and not about the body. Scanning the body
	// would also see bindings the block declares itself, and report a task
	// container built and consumed entirely inside the block as escaping. What
	// crosses into the block is the whole question.
	//
	// When `spawn` is typing this block it runs the scan over the same
	// expression itself, and asking twice would report everything twice.
	if id != tc.spawnOperand {
		seen := make(map[symbols.SymbolID]struct{})
		for _, cap := range captures {
			tc.scanSpawn(cap.exprID, seen, false)
		}
	}
	for _, cap := range captures {
		// The caller stops owning what moves in, which is what pairs with the
		// registration above. observeMove is Copy-gated internally, so it fires
		// on exactly the move-only captures — a `@copy` value composite leaves
		// the caller's binding alone and the body owns the clone instead. That
		// disagreement between the two sets is correct; making them equal
		// reintroduces one of the two failures.
		tc.observeMove(cap.exprID, cap.span)
	}
	return resultType
}

func (tc *typeChecker) typeExprBlocking(id ast.ExprID, span source.Span) types.TypeID {
	blockingData, ok := tc.builder.Exprs.Blocking(id)
	if !ok || blockingData == nil {
		return types.NoTypeID
	}
	tc.blockingDepth++
	resultType := tc.taskBlockPayload(span, blockingData.Body, false)
	tc.blockingDepth--
	captures := tc.collectBlockingCaptures(blockingData.Body)
	tc.recordBlockingCaptures(id, captures)
	for _, cap := range captures {
		capType := tc.bindingType(cap.symID)
		if tc.isReferenceType(capType) {
			tc.report(diag.SemaBlockingBorrowCapture, cap.span,
				"blocking captures must be by value; cannot capture reference %s", tc.typeLabel(capType))
			continue
		}
		// `blocking` ships its state to a worker thread while this one keeps
		// running, so a captured arbitrary-precision value would leave both
		// threads pointing at one counted block. The count is deliberately not
		// atomic, so that is a race, not just a sharing question. A Copy
		// capture cannot be made exclusive either — the caller keeps its
		// binding by definition. Refuse until the boundary deep-copies.
		if tc.result != nil && tc.result.ContainsRefCountedScalar(capType) && tc.isCopyType(capType) {
			tc.report(diag.SemaCrossNotShardMovable, cap.span,
				"`%s` cannot be captured into `blocking` yet: it carries an arbitrary-precision "+
					"value, which is a reference into a counted heap block, and the count is not "+
					"safe to share with the worker thread. Use a fixed-width type (`float64`) for the "+
					"captured value",
				tc.typeLabel(capType))
			continue
		}
		tc.checkSpawnSendability(cap.symID, cap.span)
		tc.observeMove(cap.exprID, cap.span)
	}
	return resultType
}
