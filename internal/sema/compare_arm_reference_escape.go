package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

// An arm that answers with a REFERENCE into a payload it frees hands the caller
// a pointer to storage that is gone before the pointer is read.
//
//	let got: &uint64 = compare values.pop() { Some(inner) => &inner[0]; _ => &zero; };
//
// `values.pop()` is a temporary the compare OWNS, so `inner` is the arm's to
// release; `&inner[0]` points into what that release frees. Measured on both
// sides of RV2-DEBT-205's fix — `25708659636` before, `24548386978` after — so
// it is pre-existing and untouched by it, while the VM prints 8 both times.
//
// LOWERING IS THE WRONG KIND OF ANSWER HERE and was measured to be: 205's fix
// lives in `lowerValueExpr`, which runs only where the consumer wants a VALUE,
// and pushing a load inside the block to cover this case silently turns the
// program's declared `&uint64` into a value and produced invalid LLVM IR
// (`'%t57' defined with type 'i64' but expected 'ptr'`). No emission order makes
// a reference to about-to-be-freed storage safe, so the program is refused.
//
// THE PREDICATE IS NOT "ANY REFERENCE OUT OF AN ARM". When the scrutinee is
// BORROWED, `&inner[0]` points into storage the owner keeps and which outlives
// the compare — that is legal today and must stay legal. The discriminant is
// armFreesPayloadBinding, the same decision that gave the binding its release
// obligation in the first place; asking it here is what keeps the two from
// drifting apart.

// armFreesPayloadBinding reports whether the arm itself releases this payload
// binding when it ends.
//
// It is the decision registerComparePayloadDroppables makes, named so this rule
// can ask it instead of restating it: a reference into a payload the arm frees
// dangles, while a reference into a payload the OWNER keeps is legal and common.
// A reference judged safe against a payload that function actually frees is
// precisely the use-after-free being refused, so the two share one answer.
func (tc *typeChecker) armFreesPayloadBinding(
	symID symbols.SymbolID,
	subjectBorrowed bool,
	tupleElementsBorrowed bool,
) bool {
	if tupleElementsBorrowed {
		return false
	}
	if subjectBorrowed && !tc.payloadTakesItsOwnReference(symID) {
		return false
	}
	// A binding with nothing to release frees nothing. Asking here rather than
	// only inside registerDroppableBinding leaves that caller unchanged - it
	// re-asks the same question - and makes the predicate answerable on its own.
	return tc.isDroppableBinding(symID)
}

// rejectArmReferenceIntoFreedPayload refuses an arm whose result is a reference
// rooted in a payload binding the arm releases on its way out.
func (tc *typeChecker) rejectArmReferenceIntoFreedPayload(
	result ast.ExprID,
	bindings []symbols.SymbolID,
	resultType types.TypeID,
	subjectBorrowed bool,
	tupleElementsBorrowed bool,
) {
	if !result.IsValid() || len(bindings) == 0 || tc.reporter == nil {
		return
	}
	if !tc.isReferenceType(resultType) {
		return
	}
	base := tc.loanRootBase(result)
	if !base.IsValid() {
		return
	}
	if !slices.Contains(bindings, base) {
		return
	}
	if !tc.armFreesPayloadBinding(base, subjectBorrowed, tupleElementsBorrowed) {
		return
	}
	sym := tc.symbolFromID(base)
	if sym == nil {
		return
	}
	name := tc.lookupName(sym.Name)
	if name == "" {
		return
	}
	span := tc.exprSpan(result)
	b := diag.ReportError(tc.reporter, diag.SemaArmReferenceIntoFreedPayload, span,
		fmt.Sprintf("cannot answer with a reference into `%s`: this arm frees it when it ends", name))
	if b == nil {
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"the compare owns the value it matched, so `%s` is this arm's to release - the reference would point at freed storage before it is ever read",
		name))
	b.WithNote(span, fmt.Sprintf(
		"a reference into a payload stays legal when the compare only BORROWS its subject, because then the owner keeps `%s` alive",
		name))
	b.WithNote(span, fmt.Sprintf(
		"hint: answer with the value instead of a reference to it, or match on a borrow of the subject so `%s` outlives the compare",
		name))
	b.Emit()
}
