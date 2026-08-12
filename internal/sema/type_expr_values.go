package sema

import (
	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) typeExprIdent(id ast.ExprID, span source.Span) types.TypeID {
	ident, ok := tc.builder.Exprs.Ident(id)
	if !ok || ident == nil {
		return types.NoTypeID
	}
	symID := tc.symbolForExpr(id)
	if symID == symbols.NoSymbolID {
		symID = tc.lookupValueSymbol(ident.Name, tc.currentScope())
	}
	sym := tc.symbolFromID(symID)
	if sym != nil && sym.Kind == symbols.SymbolImport {
		sym = tc.resolveImportedValueSymbol(sym, ident.Name, span)
	}
	switch {
	case sym == nil:
		if param := tc.lookupTypeParam(ident.Name); param != types.NoTypeID {
			name := tc.lookupName(ident.Name)
			if name == "" {
				name = "_"
			}
			tc.report(diag.SemaTypeMismatch, span, "type %s cannot be used as a value", name)
		}
		return types.NoTypeID
	case sym.Kind == symbols.SymbolLet || sym.Kind == symbols.SymbolParam:
		ty := tc.bindingType(symID)
		if tc.assignmentLHSDepth == 0 && tc.placeBaseDepth == 0 {
			tc.checkUseAfterMove(symID, span)
		}
		if sym.Kind == symbols.SymbolLet {
			tc.checkDeprecatedSymbol(symID, "variable", span)
		}
		return ty
	case sym.Kind == symbols.SymbolConst:
		ty := tc.ensureConstEvaluated(symID)
		tc.checkDeprecatedSymbol(symID, "constant", span)
		return ty
	case sym.Kind == symbols.SymbolType:
		name := tc.lookupName(ident.Name)
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaTypeMismatch, span, "type %s cannot be used as a value", name)
		return types.NoTypeID
	default:
		if sym.Kind == symbols.SymbolFunction && tc.callTargetDepth == 0 {
			tc.recordFunctionCall(symID)
		}
		if sym.Kind == symbols.SymbolFunction && len(sym.TypeParams) > 0 {
			if expected := tc.expectedTypeForExpr(id); expected != types.NoTypeID && tc.tryBindGenericFnValue(id, expected) {
				return expected
			}
		}
		return sym.Type
	}
}

func (tc *typeChecker) typeExprLiteral(id ast.ExprID) types.TypeID {
	lit, ok := tc.builder.Exprs.Literal(id)
	if !ok || lit == nil {
		return types.NoTypeID
	}
	return tc.literalType(lit.Kind)
}

func (tc *typeChecker) typeExprGroup(id ast.ExprID) types.TypeID {
	group, ok := tc.builder.Exprs.Group(id)
	if !ok || group == nil {
		return types.NoTypeID
	}
	return tc.typeExpr(group.Inner)
}

// consumeExprValue propagates "this expression's value is consumed" into the
// constructs that hand a subexpression's value outward rather than producing
// one themselves.
//
// The two differ in which direction they need it. A compare ARM keeps a drop
// obligation it may turn out not to owe, and consumption RETRACTS it; a ternary
// BRANCH has no obligation to begin with, and consumption ADDS the move plus
// the sibling's drop. Both are wrong to decide while the expression is typed,
// because the answer belongs to whoever receives the value.
func (tc *typeChecker) consumeExprValue(expr ast.ExprID) {
	tc.releaseArmResultObligations(expr)
	tc.observeTernaryBranchMoves(expr)
}

// observeTernaryBranchMoves settles a CONSUMED ternary's move.
//
// `let picked = cond ? a : b` hands one of the two bindings onward, and nothing
// used to say so: a ternary is not a place, so observeMove on it resolved
// nothing and both bindings kept an obligation one of them had given away.
// Measured as a double free and a read of freed memory on both.
func (tc *typeChecker) observeTernaryBranchMoves(expr ast.ExprID) {
	if tc.builder == nil {
		return
	}
	tern, ok := tc.builder.Exprs.Ternary(tc.unwrapGroups(expr))
	if !ok || tern == nil {
		return
	}
	tc.consumeBranchResults([]ast.ExprID{tern.TrueExpr, tern.FalseExpr})
}

// consumeBranchResults settles the move a CONSUMED many-branch expression
// makes. A ternary and a compare are the same shape for this: several branches,
// exactly ONE of which runs, and whichever it is hands its result outward.
//
// It cannot be settled while the expression is TYPED, because only the context
// knows whether the value is consumed at all — `peek(cond ? a : b)` with a `&T`
// parameter borrows it, and there every branch keeps what it holds. So this
// runs from observeMove, which is exactly the set of consuming positions.
//
// Marking every branch's operand moved and stopping there would LEAK whichever
// branch did not run. Each branch therefore also owes what the others gave
// away — the one-sided obligation a compare arm already carries for a binding a
// sibling arm moved, computed by the same function and landing on the same
// channel, which `wrapArmDrops` applies to any expression.
//
// Branches are walked through observeMove rather than marked directly, so a
// nested ternary or compare settles its own branches first and this one sees
// the result.
func (tc *typeChecker) consumeBranchResults(results []ast.ExprID) {
	if len(results) == 0 {
		return
	}
	before := tc.snapshotMovedPlaces()
	moved := make([]map[Place]source.Span, len(results))
	for i, result := range results {
		tc.restoreMovedPlaces(before)
		// observeMove settles a nested ternary or compare on its way through,
		// so a branch that is itself a choice is already resolved here.
		tc.observeMove(result, tc.exprSpan(result))
		moved[i] = tc.snapshotMovedPlaces()
	}

	merged := before
	for _, m := range moved {
		merged = mergeMovedPlaces(merged, m)
	}
	for i, result := range results {
		var others map[Place]source.Span
		for j, m := range moved {
			if j == i {
				continue
			}
			others = mergeMovedPlaces(others, m)
		}
		tc.recordBranchOneSidedDrops(result, moved[i], others)
	}
	// A binding ANY branch gave away is moved after the expression: a later use
	// has to be rejected, because some path did hand it on.
	tc.restoreMovedPlaces(merged)
}

// unwrapGroups peels parentheses off an expression.
//
// They are transparent to every question asked here — `(cond ? a : b)` moves
// exactly what `cond ? a : b` moves — but they are NOT transparent to the
// payload accessors, so a nested ternary written the way people write one went
// unrecognised and its branches kept the obligations they had given away.
func (tc *typeChecker) unwrapGroups(expr ast.ExprID) ast.ExprID {
	if tc.builder == nil {
		return expr
	}
	// Bounded rather than `for {}`: a malformed tree must not hang the checker.
	for range 64 {
		group, ok := tc.builder.Exprs.Group(expr)
		if !ok || group == nil || !group.Inner.IsValid() {
			return expr
		}
		expr = group.Inner
	}
	return expr
}

// recordBranchOneSidedDrops gives one branch the obligations the other
// branches' moves left it holding.
func (tc *typeChecker) recordBranchOneSidedDrops(branch ast.ExprID, mine, others map[Place]source.Span) {
	if !branch.IsValid() {
		return
	}
	owing, plans := tc.oneSidedObligations(mine, others)
	if len(owing) == 0 {
		return
	}
	if tc.result.ArmDropsExpr == nil {
		tc.result.ArmDropsExpr = make(map[ast.ExprID][]symbols.SymbolID)
	}
	tc.result.ArmDropsExpr[branch] = append(tc.result.ArmDropsExpr[branch], owing...)
	tc.recordOneSidedDrops(DropSite{Expr: branch}, plans)
}

func (tc *typeChecker) typeExprTernary(id ast.ExprID, span source.Span) types.TypeID {
	tern, ok := tc.builder.Exprs.Ternary(id)
	if !ok || tern == nil {
		return types.NoTypeID
	}
	tc.ensureBoolContext(tern.Cond, tc.exprSpan(tern.Cond))

	// The branches are typed in ISOLATION from each other, because only one of
	// them runs. Evaluating a branch can itself move something — `cond ?
	// pass(a) : b` hands `a` to a by-value parameter — and without the restore
	// between them the second branch inherits the first's moves, so nothing
	// notices that `a` is still live on the path where `pass` was never
	// called. That is a leak of `a` on exactly the untaken path, and the
	// compensating drops below are what reclaim it: each branch owes what the
	// OTHER branch's evaluation gave away.
	//
	// The same reasoning a compare applies across its arms, which restores
	// before each one for exactly this reason.
	before := tc.snapshotMovedPlaces()
	trueType := tc.typeExpr(tern.TrueExpr)
	movedTrue := tc.snapshotMovedPlaces()

	tc.restoreMovedPlaces(before)
	falseType := tc.typeExpr(tern.FalseExpr)
	movedFalse := tc.snapshotMovedPlaces()

	// The branches transfer into the ternary's own result value, so the outer
	// expression carries the drop candidacy from here. Asked BEFORE consuming,
	// because consuming is what erases the evidence of who produced what.
	//
	// Unless the value goes NOWHERE. A discarded ternary has no consumer to
	// carry anything, so each branch keeps its own candidacy and frees what it
	// built at its own region's end — which is where it was materialized, so
	// the drop is dominated. That is the only shape where a MIXED ternary can
	// be reclaimed without duplicating a consumer into the branches: the
	// forwarding branch was never a candidate and keeps forwarding.
	if tc.isExprDiscarded(id) {
		tc.recordBranchOneSidedDrops(tern.TrueExpr, movedTrue, movedFalse)
		tc.recordBranchOneSidedDrops(tern.FalseExpr, movedFalse, movedTrue)
		tc.restoreMovedPlaces(mergeMovedPlaces(movedTrue, movedFalse))
		return tc.unifyTernaryBranches(trueType, falseType, span)
	}

	tc.noteChoiceOwnsItsValue(id, []ast.ExprID{tern.TrueExpr, tern.FalseExpr})
	tc.consumeTempCandidate(tern.TrueExpr)
	tc.consumeTempCandidate(tern.FalseExpr)

	tc.recordBranchOneSidedDrops(tern.TrueExpr, movedTrue, movedFalse)
	tc.recordBranchOneSidedDrops(tern.FalseExpr, movedFalse, movedTrue)
	tc.restoreMovedPlaces(mergeMovedPlaces(movedTrue, movedFalse))
	resultType := tc.unifyTernaryBranches(trueType, falseType, span)
	if resultType != types.NoTypeID {
		tc.recordNumericWidening(tern.TrueExpr, trueType, resultType)
		tc.recordNumericWidening(tern.FalseExpr, falseType, resultType)
	}
	return resultType
}

func (tc *typeChecker) typeExprArray(id ast.ExprID, span source.Span) types.TypeID {
	arr, ok := tc.builder.Exprs.Array(id)
	if !ok || arr == nil {
		return types.NoTypeID
	}
	var elemType types.TypeID
	for _, elem := range arr.Elements {
		elemTy := tc.typeExpr(elem)
		// The array takes ownership of the element, exactly as a struct
		// literal takes ownership of a field: the temp candidate is consumed
		// AND the binding-level move is observed. Only the first half was here,
		// so `let a: string = ...; let xs = [a, ...]` left `a` readable and
		// owning the same string the array now owned — two owners, no refcount
		// on the native side to arbitrate between them, and a double free at
		// scope exit (RV2-DEBT-209). The tuple literal below had the same gap.
		tc.consumeTempCandidate(elem)
		tc.observeMove(elem, tc.exprSpan(elem))
		if tc.isTaskType(elemTy) {
			tc.trackTaskPassedAsArg(elem)
		}
		if tc.rejectRefInAggregate(elemTy, tc.exprSpan(elem), "array element") {
			return types.NoTypeID
		}
		if elemType == types.NoTypeID {
			elemType = elemTy
			continue
		}
		if elemTy != types.NoTypeID && elemTy != elemType {
			tc.report(diag.SemaTypeMismatch, span, "array elements must have the same type")
		}
	}
	if elemType == types.NoTypeID {
		return types.NoTypeID
	}
	if len(arr.Elements) > 0 {
		if len(arr.Elements) > int(^uint32(0)) {
			tc.report(diag.SemaTypeMismatch, span, "array literal too large")
		} else if length, err := safecast.Conv[uint32](len(arr.Elements)); err == nil {
			if fixed := tc.instantiateArrayFixed(elemType, length); fixed != types.NoTypeID {
				return fixed
			}
		}
	}
	return tc.instantiateArrayType(elemType)
}

func (tc *typeChecker) typeExprMap(id ast.ExprID, span source.Span) types.TypeID {
	mp, ok := tc.builder.Exprs.Map(id)
	if !ok || mp == nil {
		return types.NoTypeID
	}
	var keyType types.TypeID
	var valueType types.TypeID
	for _, entry := range mp.Entries {
		kType := tc.typeExpr(entry.Key)
		tc.consumeTempCandidate(entry.Key)
		if tc.isTaskType(kType) {
			tc.trackTaskPassedAsArg(entry.Key)
		}
		vType := tc.typeExpr(entry.Value)
		tc.consumeTempCandidate(entry.Value)
		if tc.isTaskType(vType) {
			tc.trackTaskPassedAsArg(entry.Value)
		}
		if tc.rejectRefInAggregate(kType, tc.exprSpan(entry.Key), "map key") ||
			tc.rejectRefInAggregate(vType, tc.exprSpan(entry.Value), "map value") {
			return types.NoTypeID
		}
		if keyType == types.NoTypeID {
			keyType = kType
		} else if kType != types.NoTypeID && kType != keyType {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(entry.Key), "map keys must have the same type")
		}
		if valueType == types.NoTypeID {
			valueType = vType
		} else if vType != types.NoTypeID && vType != valueType {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(entry.Value), "map values must have the same type")
		}
	}
	if keyType == types.NoTypeID || valueType == types.NoTypeID {
		return types.NoTypeID
	}
	return tc.instantiateMapType(keyType, valueType, span)
}

func (tc *typeChecker) typeExprRange(id ast.ExprID, span source.Span) types.TypeID {
	rng, ok := tc.builder.Exprs.RangeLit(id)
	if !ok || rng == nil {
		return types.NoTypeID
	}
	intType := tc.types.Builtins().Int
	if rng.Start.IsValid() {
		startType := tc.typeExpr(rng.Start)
		if startType != types.NoTypeID && !tc.sameType(startType, intType) {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(rng.Start),
				"range bound must be int, got %s", tc.typeLabel(startType))
		}
	}
	if rng.End.IsValid() {
		endType := tc.typeExpr(rng.End)
		if endType != types.NoTypeID && !tc.sameType(endType, intType) {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(rng.End),
				"range bound must be int, got %s", tc.typeLabel(endType))
		}
	}
	tc.recordRangeLiteralCall(id, rng)
	return tc.resolveRangeType(intType, span, tc.currentScope())
}

func (tc *typeChecker) recordRangeLiteralCall(id ast.ExprID, rng *ast.ExprRangeLitData) {
	if tc == nil || tc.builder == nil || rng == nil {
		return
	}
	name := "rt_range_int_full"
	switch {
	case rng.Start.IsValid() && rng.End.IsValid():
		name = "rt_range_int_new"
	case rng.Start.IsValid():
		name = "rt_range_int_from_start"
	case rng.End.IsValid():
		name = "rt_range_int_to_end"
	}
	nameID := tc.builder.StringsInterner.Intern(name)
	scope := tc.fileScope()
	if !scope.IsValid() {
		scope = tc.currentScope()
	}
	tc.recordCallSymbol(id, tc.symbolInScope(scope, nameID, symbols.SymbolFunction))
}

func (tc *typeChecker) typeExprTuple(id ast.ExprID) types.TypeID {
	tuple, ok := tc.builder.Exprs.Tuple(id)
	if !ok || tuple == nil {
		return types.NoTypeID
	}
	elems := make([]types.TypeID, 0, len(tuple.Elements))
	allValid := true
	for _, elem := range tuple.Elements {
		elemType := tc.typeExpr(elem)
		// Same ownership transfer as the array literal above and the struct
		// literal in type_expr_struct.go: the aggregate owns the element, so
		// the binding it came from must be dead afterwards (RV2-DEBT-209).
		tc.consumeTempCandidate(elem)
		tc.observeMove(elem, tc.exprSpan(elem))
		if elemType == types.NoTypeID {
			allValid = false
		}
		if tc.rejectRefInAggregate(elemType, tc.exprSpan(elem), "tuple element") {
			allValid = false
		}
		elems = append(elems, elemType)
	}
	if len(elems) == 0 {
		return tc.types.Builtins().Unit
	}
	if !allValid {
		return types.NoTypeID
	}
	return tc.types.RegisterTuple(elems)
}

func (tc *typeChecker) typeExprMember(id ast.ExprID, span source.Span) types.TypeID {
	member, ok := tc.builder.Exprs.Member(id)
	if !ok || member == nil {
		return types.NoTypeID
	}
	if module := tc.moduleSymbolForExpr(member.Target); module != nil {
		result := tc.typeOfModuleMember(module, member.Field, span)
		if tc.callTargetDepth == 0 {
			if symID := tc.symbolForExpr(id); symID.IsValid() {
				if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolFunction {
					if len(sym.TypeParams) == 0 {
						tc.recordFunctionCall(symID)
					} else if expected := tc.expectedTypeForExpr(id); expected != types.NoTypeID && tc.tryBindGenericFnValue(id, expected) {
						return expected
					}
				}
			}
		}
		return result
	}
	if enumType := tc.enumTypeForExpr(member.Target); enumType != types.NoTypeID {
		return tc.typeOfEnumVariant(enumType, member.Field, span)
	}
	targetType := tc.typeExprAsPlaceBase(member.Target)
	if tc.reportFarLocalOp(targetType, span) {
		return types.NoTypeID
	}
	resultType := tc.memberResultType(targetType, member.Field, span)
	_, isAddressOfOperand := tc.addressOfOperands[id]
	tc.checkAtomicFieldDirectAccess(id, isAddressOfOperand, span)
	return resultType
}

func (tc *typeChecker) typeExprTupleIndex(id ast.ExprID, span source.Span) types.TypeID {
	data, ok := tc.builder.Exprs.TupleIndex(id)
	if !ok || data == nil {
		return types.NoTypeID
	}
	targetType := tc.typeExprAsPlaceBase(data.Target)
	return tc.tupleIndexResultType(targetType, data.Index, span)
}

func (tc *typeChecker) typeExprSpread(id ast.ExprID) {
	spread, ok := tc.builder.Exprs.Spread(id)
	if ok && spread != nil {
		tc.typeExpr(spread.Value)
	}
}

func (tc *typeChecker) typeExprStruct(id ast.ExprID, span source.Span) types.TypeID {
	data, ok := tc.builder.Exprs.Struct(id)
	if !ok || data == nil {
		return types.NoTypeID
	}
	for _, field := range data.Fields {
		tc.typeExpr(field.Value)
	}
	if !data.Type.IsValid() {
		return types.NoTypeID
	}
	scope := tc.scopeOrFile(tc.currentScope())
	if inferred, handled := tc.inferStructLiteralType(data, scope, span); handled {
		return inferred
	}
	ty := tc.resolveTypeExprWithScope(data.Type, scope)
	if ty != types.NoTypeID {
		tc.validateStructLiteralFields(ty, data, span)
	}
	return ty
}
