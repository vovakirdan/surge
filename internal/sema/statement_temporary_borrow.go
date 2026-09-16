package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A string rvalue passed to a shared `&string` formal is admitted with no borrow
// record (magic_ownership.go:31-33 -> addressability.go:156-186) and is released at
// its evaluation region's end (temp_drops.go:307-379 -> hir/lower_expr.go:88-106 ->
// mir/lower_temp_drops.go:211-253). The borrow table therefore cannot see the loan:
// bindingBorrowForExpr can only report a BorrowID and the temporary has none, so a
// binding that keeps the call's reference result outlives the storage, and the VM
// reports it one statement later as VM3301. The transfer is refused here, at the
// sink, where the region is still open and sema's own release decision is readable.
//
// Two sentences share SEM3023 after this file. The existing one refuses FORMING a
// reference to a temporary (addressability.go:50-66, pinned by
// testdata/golden/sema/invalid/non_string_temp_borrow.diag); this one refuses
// KEEPING one. Both name a temporary and both end in the same advice. A third code
// was not available: new codes may live only in internal/diag/codes.go, which is
// already over the 500-line limit and must not grow by a line.

const (
	statementTemporaryNote = "this value is freed when the statement that created it ends"
	statementTemporaryHelp = "bind the value first, then pass the binding (e.g. `let tmp = \"a\" + tail;` then `id_ref(tmp)`)"
)

// statementTemporary reports whether this evaluation is one sema itself decided to
// release at its region's end.
//
// Both disjuncts are sema's OWN release decision and HIR/MIR emit exactly that set,
// so neither guesses. A frame still open answers through pendingTempCandidate
// (temp_drops.go:89-106); a frame already published -- an inner statement of a block
// expression, an arm body -- answers through Result.TempDrops, which popTempFrame
// filled (temp_drops.go:38-60).
func (tc *typeChecker) statementTemporary(expr ast.ExprID) bool {
	if tc == nil || !expr.IsValid() || tc.builder == nil {
		return false
	}
	if tc.pendingTempCandidate(expr) {
		return true
	}
	if tc.result == nil || tc.result.TempDrops == nil {
		return false
	}
	_, published := tc.result.TempDrops[tc.unwrapTempCandidate(expr)]
	return published
}

// formalMayBeBorrowSource asks whether a formal can be the SOURCE of the storage a
// result points into.
//
// refResultCanAliasParam (borrow_runtime_binding.go:167-180) asks the other
// question -- whether the result may alias that formal AS WRITTEN -- because its one
// caller (:132) needs the mutability of the loan it is about to hand out. For this
// rule a `&mut` result over a shared `&` formal still points into whatever that
// formal borrowed, so the `&mut ` clause at :176-179 would be fail-OPEN here. The
// two cannot be merged: editing that function would change recorded borrow
// inheritance for its existing caller.
//
// The spelling is safe to test: signature keys are built from the parameter TYPE
// alone (symbols/function_signature.go:99), attributes are read separately and only
// for allow_to (:88-98) so @return_source never enters the key, and `&` / `&mut `
// are prefixes (:137-140).
func formalMayBeBorrowSource(param symbols.TypeKey) bool {
	return strings.HasPrefix(strings.TrimSpace(string(param)), "&")
}

// statementTemporaryLoan walks a value back to the statement temporary it may carry
// a borrow of, mirroring inheritedBorrowForExpr's shape
// (borrow_runtime_binding.go:68-165) and returning the TEMPORARY's expression --
// what the note must name -- instead of a BorrowID. The first temporary reached is
// returned, so a nested form reports one row rather than one per level.
func (tc *typeChecker) statementTemporaryLoan(expr ast.ExprID) (ast.ExprID, bool) {
	return tc.statementTemporaryLoanSeen(expr, make(map[ast.ExprID]struct{}, 4))
}

func (tc *typeChecker) statementTemporaryLoanSeen(expr ast.ExprID, seen map[ast.ExprID]struct{}) (ast.ExprID, bool) {
	if tc == nil || tc.builder == nil || tc.result == nil {
		return ast.NoExprID, false
	}
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() {
		return ast.NoExprID, false
	}
	if _, visited := seen[expr]; visited {
		return ast.NoExprID, false
	}
	seen[expr] = struct{}{}

	// A block expression hands its value out through its results -- the same
	// expansion the return sink performs (return_value_paths.go:39-63 over
	// recordBlockResultExprs, :10-37).
	if results := tc.blockResultExprs[expr]; len(results) > 0 {
		for _, inner := range results {
			if temp, found := tc.statementTemporaryLoanSeen(inner, seen); found {
				return temp, true
			}
		}
		return ast.NoExprID, false
	}

	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return ast.NoExprID, false
	}
	switch node.Kind {
	case ast.ExprUnary:
		unary, ok := tc.builder.Exprs.Unary(expr)
		if !ok || unary == nil {
			return ast.NoExprID, false
		}
		// Parity with inheritedBorrowForExpr (:97-100): a deref or an `own`
		// marker forwards the value, while `&` mints a new borrow whose operand
		// typeExplicitBorrow has already consumed (index_borrow.go:16-17).
		if unary.Op == ast.ExprUnaryDeref || unary.Op == ast.ExprUnaryOwn {
			return tc.statementTemporaryLoanSeen(unary.Operand, seen)
		}
	case ast.ExprCast:
		cast, ok := tc.builder.Exprs.Cast(expr)
		if !ok || cast == nil || !cast.Value.IsValid() {
			return ast.NoExprID, false
		}
		// A cast that PRODUCES its value built a fresh evaluation, so nothing it
		// hands on can be a borrow of its operand; one that produces nothing
		// forwards the operand unchanged (temp_drops.go:394-410).
		if tc.castProducesItsValue(expr, tc.result.ExprTypes[expr]) {
			return ast.NoExprID, false
		}
		return tc.statementTemporaryLoanSeen(cast.Value, seen)
	case ast.ExprTernary:
		tern, ok := tc.builder.Exprs.Ternary(expr)
		if !ok || tern == nil {
			return ast.NoExprID, false
		}
		if temp, found := tc.statementTemporaryLoanSeen(tern.TrueExpr, seen); found {
			return temp, true
		}
		return tc.statementTemporaryLoanSeen(tern.FalseExpr, seen)
	case ast.ExprCompare:
		cmp, ok := tc.builder.Exprs.Compare(expr)
		if !ok || cmp == nil {
			return ast.NoExprID, false
		}
		for i := range cmp.Arms {
			if temp, found := tc.statementTemporaryLoanSeen(cmp.Arms[i].Result, seen); found {
				return temp, true
			}
		}
	case ast.ExprCall:
		return tc.statementTemporaryLoanForCall(expr, seen)
	}
	return ast.NoExprID, false
}

func (tc *typeChecker) statementTemporaryLoanForCall(expr ast.ExprID, seen map[ast.ExprID]struct{}) (ast.ExprID, bool) {
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		return ast.NoExprID, false
	}
	carried, carries := tc.carriedReferenceType(tc.result.ExprTypes[expr])
	if !carries {
		return ast.NoExprID, false
	}
	symID := tc.symbolForExpr(expr)
	if !symID.IsValid() {
		return ast.NoExprID, false
	}
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Signature == nil || len(sym.Signature.Params) == 0 {
		return ast.NoExprID, false
	}

	// The receiver passes through the SAME formal test as an argument, which is
	// what the mirrored original does through one addCandidate closure
	// (borrow_runtime_binding.go:131-134, :146-152): a by-value `self` is never
	// walked. Gate and move read the same key, so a gate-false receiver is one
	// applyParamOwnership's default arm already moved (magic_ownership.go:21-37),
	// and it carries no candidacy by the time any sink asks.
	visit := func(param symbols.TypeKey, argExpr ast.ExprID) (ast.ExprID, bool) {
		if !formalMayBeBorrowSource(param) {
			return ast.NoExprID, false
		}
		if temp, found := tc.temporaryUnderTransparentWrappers(argExpr, carried); found {
			return temp, true
		}
		return tc.statementTemporaryLoanSeen(argExpr, seen)
	}

	offset := 0
	if sym.Signature.HasSelf {
		offset = 1
		if member, found := tc.builder.Exprs.Member(call.Target); found && member != nil {
			if temp, hit := visit(sym.Signature.Params[0], member.Target); hit {
				return temp, true
			}
		}
	}
	for i, arg := range call.Args {
		paramIndex := i + offset
		if paramIndex >= len(sym.Signature.Params) {
			break
		}
		if temp, hit := visit(sym.Signature.Params[paramIndex], arg.Value); hit {
			return temp, true
		}
	}
	return ast.NoExprID, false
}

// temporaryUnderTransparentWrappers descends the wrappers that hand their operand's
// value on unchanged -- parentheses, and a cast that converts nothing -- and answers
// with the innermost evaluation, which is the one MIR frees.
//
// Descending BEFORE the test is what makes the note name `make()` rather than the
// `make() to string` wrapped around it. The rule is unwrapTempCandidate's
// (temp_drops.go:279-305); it is spelled out here because the walk needs the node it
// stopped at, not merely the flag.
func (tc *typeChecker) temporaryUnderTransparentWrappers(expr ast.ExprID, carried types.TypeID) (ast.ExprID, bool) {
	anchor := tc.unwrapTempCandidate(tc.unwrapGroupExpr(expr))
	if !anchor.IsValid() {
		return ast.NoExprID, false
	}
	if tc.statementTemporary(anchor) && tc.referentCanBeTheTemporary(carried, anchor) {
		return anchor, true
	}
	return ast.NoExprID, false
}

// referentCanBeTheTemporary narrows the walk to a carried referent the temporary
// could actually hold: a `&int` cannot point into a `string`, because a string
// exposes no interior reference. An unresolved type answers YES, because this rule
// refuses and must not go quiet exactly where the evidence is missing.
func (tc *typeChecker) referentCanBeTheTemporary(carried types.TypeID, arg ast.ExprID) bool {
	if tc == nil || tc.types == nil || !arg.IsValid() {
		return false
	}
	referent := tc.referenceReferent(carried)
	if referent == types.NoTypeID {
		return true
	}
	produced := types.NoTypeID
	if tc.result != nil {
		// An argument the parameter converts on the way in produces what the
		// conversion MADE, not what was written (the same correction popTempFrame
		// applies at temp_drops.go:50-58).
		if conv, ok := tc.result.ImplicitConversions[arg]; ok && conv.Kind == ImplicitConversionTo {
			produced = conv.Target
		} else {
			produced = tc.result.ExprTypes[arg]
		}
	}
	if produced == types.NoTypeID {
		return true
	}
	return tc.resolveAlias(referent) == tc.resolveAlias(produced)
}

func (tc *typeChecker) referenceReferent(id types.TypeID) types.TypeID {
	if tc.types == nil || id == types.NoTypeID {
		return types.NoTypeID
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(id))
	if !ok || tt.Kind != types.KindReference {
		return types.NoTypeID
	}
	return tt.Elem
}

// bindingCarriesReference answers whether the binding this store feeds can hold a
// reference at all. updateBindingValue runs for EVERY let and every assignment
// (type_checker_walk.go:251, borrow_runtime_ops.go:588), so this guard is what keeps
// the walk away from the overwhelming majority of statements.
func (tc *typeChecker) bindingCarriesReference(symID symbols.SymbolID, expr ast.ExprID) bool {
	bound := tc.bindingType(symID)
	if bound == types.NoTypeID && tc.result != nil {
		bound = tc.result.ExprTypes[expr]
	}
	_, carries := tc.carriedReferenceType(bound)
	return carries
}

func (tc *typeChecker) refuseStatementTemporaryBinding(symID symbols.SymbolID, expr ast.ExprID) {
	if tc == nil || !expr.IsValid() {
		return
	}
	if !tc.bindingCarriesReference(symID, expr) {
		return
	}
	temp, ok := tc.statementTemporaryLoan(expr)
	if !ok {
		return
	}
	tc.reportStatementTemporary(diag.SemaBorrowNonAddressable,
		"cannot keep a reference to a temporary value; bind it to a variable first",
		tc.exprSpan(expr), temp)
}

func (tc *typeChecker) refuseStatementTemporaryReturn(expr ast.ExprID, ty types.TypeID, span source.Span) {
	if tc == nil || !expr.IsValid() {
		return
	}
	if _, carries := tc.carriedReferenceType(ty); !carries {
		return
	}
	temp, ok := tc.statementTemporaryLoan(expr)
	if !ok {
		return
	}
	tc.reportStatementTemporary(diag.SemaBorrowEscapesReturn,
		"cannot return a borrow of a temporary: it is freed when the statement that created it ends",
		span, temp)
}

// reportStatementTemporary keeps the message, the note and the help in one place so
// the two sinks cannot drift apart. Reporting changes nothing else: the binding's
// recorded borrow stays NoBorrowID, so every later rule behaves exactly as before
// and no cascade follows.
func (tc *typeChecker) reportStatementTemporary(code diag.Code, message string, at source.Span, temp ast.ExprID) {
	if tc.reporter == nil {
		tc.report(code, at, "%s", message)
		return
	}
	b := diag.ReportError(tc.reporter, code, at, message)
	if b == nil {
		return
	}
	b.WithNote(tc.exprSpan(temp), statementTemporaryNote)
	b.WithHelp(at, statementTemporaryHelp)
	b.Emit()
}
