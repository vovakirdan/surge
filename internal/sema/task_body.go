package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/types"
)

// taskBlockPayload types the body of an `async { }` / `blocking { }` block
// and returns the `Task<T>` it evaluates to.
//
// The body is a value-producing block of its own: `ret v;` gives it its
// value, exactly as it does in an `on` crossing body, and `T` is the
// unification of the `ret` sites. It is walked under the same return-context
// machinery a crossing body uses (returnCtxTaskPayload collects the `ret`
// sites), because the two answer the same question — what the block is worth
// — and a third mechanism would be a second place for that answer to drift.
//
// A `return` inside the body is refused there (validateReturn): the body runs
// as its own task, and there is no enclosing function for it to leave.
func (tc *typeChecker) taskBlockPayload(id ast.ExprID, span source.Span, body ast.StmtID, async bool) types.TypeID {
	var returns []collectedResult
	var bareRet []source.Span
	label := "blocking body"
	if async {
		label = "async body"
	}
	// A known `Task<T>` expectation reaches the `ret` sites as `T`, so a
	// literal or a bare struct literal is typed against it instead of being
	// reported as a mismatch after the fact.
	expected := tc.expectedTypeForExpr(id)
	expectedPayload := tc.taskPayloadType(expected)
	tc.pushReturnContext(returnCtxTaskPayload, expectedPayload, span, &returns, &bareRet)
	tc.returnStack[len(tc.returnStack)-1].bodyLabel = label
	if async {
		tc.awaitDepth++
		tc.asyncBlockDepth++
	}
	tc.walkStmt(body)
	if async {
		tc.asyncBlockDepth--
		tc.awaitDepth--
	}
	returnRejected := tc.returnStack[len(tc.returnStack)-1].returnRejected
	tc.popReturnContext()

	payload, sawRet := tc.unifyBodyResults(label, returns, bareRet)
	// A body that produced no proper result adopts the expected type so the
	// enclosing binding/return does not also report a cascading mismatch:
	// the refusal (or the missing-value diagnosis) already names the edit.
	explained := returnRejected ||
		tc.checkTaskBodyValue(span, body, label, payload, sawRet, bareRet, expectedPayload)
	if explained && expected != types.NoTypeID {
		return expected
	}
	return tc.taskType(payload, span)
}

// recordRetExit records what a `ret` releases on its way out, when it leaves
// a body of its own rather than a block expression.
//
// An async/blocking body's ONLY exit is `ret`, and the body owns what it
// was handed: the captures moved into it and anything it built itself.
// Without this the obligations were collected by nobody — the block's normal
// end is unreachable past a `ret`. A crossing body is in the same position.
//
// Restricted to those bodies on purpose. A `ret` in an ordinary block
// expression yields to the enclosing block, whose own scope still closes
// normally around it, and collecting out to the function root there would
// free the enclosing function's live bindings.
func (tc *typeChecker) recordRetExit(id ast.StmtID) {
	if ctx := tc.currentBlockReturnContext(); ctx != nil && ctx.kind == returnCtxTaskPayload {
		// The same two notes an explicit `return` leaves: a task-container
		// loop this exit cuts short, and the drops the exit owes.
		tc.noteTaskContainerLoopReturn()
		tc.recordEarlyExitDrops(id, false)
		return
	}
	if tc.insideOnCrossing() {
		tc.recordEarlyExitDrops(id, false)
	}
}

// reportTaskBodyReturn refuses a `return` inside an async/blocking body. The
// fix replaces the keyword; it is applied without asking only when the
// program it produces is proven well-typed — the returned value already has
// the type the body must yield, or nothing constrains it — and left for
// review otherwise, because then `ret` alone would not make it compile.
func (tc *typeChecker) reportTaskBodyReturn(body *returnContext, span source.Span, expr ast.ExprID, actual types.TypeID) {
	body.returnRejected = true
	if tc.reporter == nil {
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaTaskBodyReturn, span,
		fmt.Sprintf("cannot return from the enclosing function inside the %s: the body runs as its own task; write `ret <expr>;` to give the body its value", body.bodyLabel))
	if b == nil {
		return
	}
	nothing := tc.types.Builtins().Nothing
	unconstrained := body.expected == types.NoTypeID || body.expected == nothing
	proven := false
	switch {
	case !expr.IsValid():
		proven = unconstrained
	case actual == types.NoTypeID:
		proven = false
	default:
		proven = unconstrained || tc.typesAssignable(body.expected, actual, true)
	}
	applicability := diag.FixApplicabilityManualReview
	if proven {
		applicability = diag.FixApplicabilityAlwaysSafe
	}
	keyword := source.Span{File: span.File, Start: span.Start, End: span.Start + uint32(len("return"))}
	b.WithFixSuggestion(fix.ReplaceSpan(
		"replace `return` with `ret`",
		keyword,
		"ret",
		"return",
		fix.WithID(fix.MakeFixID(diag.SemaTaskBodyReturn, keyword)),
		fix.WithApplicability(applicability),
		fix.Preferred(),
	))
	b.WithNote(body.span, fmt.Sprintf("the %s starts here: `ret` leaves it with a value, `return` leaves a function, and there is no function between the two", body.bodyLabel))
	b.Emit()
}

// checkTaskBodyValue diagnoses a body whose `ret` sites do not add up to the
// value its site expects, and reports whether it explained the site's
// expectation (so the caller adopts the expected type instead of also
// reporting a bare mismatch):
//
//   - no `ret` at all where `Task<T>` is expected — an error naming the edit;
//   - no `ret`, nothing expected, but a discarded trailing value — a warning;
//   - a valued body some path of which falls off the end — an error;
//   - a bare `ret;` in a body whose other exits carry a value — an error.
func (tc *typeChecker) checkTaskBodyValue(span source.Span, body ast.StmtID, label string, payload types.TypeID, sawRet bool, bareRet []source.Span, expectedPayload types.TypeID) bool {
	if tc.types == nil {
		return false
	}
	nothing := tc.types.Builtins().Nothing
	if !sawRet {
		return tc.taskBodyNoValue(span, body, expectedPayload, label)
	}
	if payload == types.NoTypeID || payload == nothing {
		return false
	}
	for _, sp := range bareRet {
		tc.report(diag.SemaTypeMismatch, sp,
			"bare 'ret;' can only be used in %s whose value is nothing; this one yields %s, so write 'ret value;'",
			aOrAn(label), tc.typeLabel(payload))
	}
	if tc.returnStatus(body) != returnClosed {
		tc.report(diag.SemaTaskBodyNoValue, span,
			"not every path of this %s ends in `ret`: the body yields %s, so each path must give it a value with `ret <expr>;`",
			label, tc.typeLabel(payload))
		return true
	}
	return false
}

func aOrAn(label string) string {
	if label != "" && label[0] == 'a' {
		return "an " + label
	}
	return "a " + label
}

// taskBodyNoValue explains a body that never `ret`s. It reports an error when
// the site expects a `Task<T>` with a real `T` and a warning when the body's
// last statement computes a value and discards it, and returns whether the
// site's expectation has been explained.
func (tc *typeChecker) taskBodyNoValue(span source.Span, body ast.StmtID, expectedPayload types.TypeID, label string) bool {
	nothing := tc.types.Builtins().Nothing
	wantsValue := expectedPayload != types.NoTypeID && expectedPayload != nothing
	tail := tc.taskBodyTail(body)
	if tail.bare {
		// `blocking { 42 }`: the parser already named the edit at the `}`
		// (SynTaskBodyBareValue / the missing `;`); saying it again here would
		// diagnose one mistake twice.
		return wantsValue
	}
	if tc.reporter == nil {
		return wantsValue
	}
	if wantsValue {
		b := diag.ReportError(tc.reporter, diag.SemaTaskBodyNoValue, span,
			fmt.Sprintf("this %s never gives a value with `ret`, so its task yields nothing, but Task<%s> is expected here", label, tc.typeLabel(expectedPayload)))
		if b == nil {
			return true
		}
		b.WithHelp(span, "write `ret <expr>;` where the body's value is known")
		if tail.expr.IsValid() && tail.typ != types.NoTypeID && tc.typesAssignable(expectedPayload, tail.typ, true) {
			// The discarded last value already has the expected type: `ret`
			// in front of it is the one edit that makes the program well-typed.
			b.WithFixSuggestion(fix.InsertText("insert `ret ` before the last statement", tail.span.ZeroideToStart(), "ret ", "",
				fix.WithID(fix.MakeFixID(diag.SemaTaskBodyNoValue, tail.span)), fix.Preferred()))
		}
		b.Emit()
		return true
	}
	if tail.value {
		b := diag.ReportWarning(tc.reporter, diag.SemaTaskBodyNoValue, tail.span,
			fmt.Sprintf("this %s yields nothing: its last statement computes a %s and discards it; write `ret` in front of it if that is the body's value", label, tc.typeLabel(tail.typ)))
		if b != nil {
			// Whether the discarded value was meant as the body's value is the
			// author's call, so the edit is offered, never applied unasked.
			b.WithFixSuggestion(fix.InsertText("insert `ret ` before the last statement", tail.span.ZeroideToStart(), "ret ", "",
				fix.WithID(fix.MakeFixID(diag.SemaTaskBodyNoValue, tail.span)), fix.WithApplicability(diag.FixApplicabilityManualReview)))
			b.Emit()
		}
	}
	return false
}

// taskBodyTail describes the last statement of a body when it is an
// expression statement: `bare` when it has no `;` (the parser refused it),
// `value` when it computes a value for its own sake — a literal, an operator
// expression, a read — rather than calling something for its effect.
type taskBodyTail struct {
	expr  ast.ExprID
	span  source.Span
	typ   types.TypeID
	bare  bool
	value bool
}

func (tc *typeChecker) taskBodyTail(body ast.StmtID) taskBodyTail {
	var tail taskBodyTail
	if tc.builder == nil || !body.IsValid() {
		return tail
	}
	block := tc.builder.Stmts.Block(body)
	if block == nil || len(block.Stmts) == 0 {
		return tail
	}
	last := block.Stmts[len(block.Stmts)-1]
	stmt := tc.builder.Stmts.Get(last)
	if stmt == nil || stmt.Kind != ast.StmtExpr {
		return tail
	}
	exprStmt := tc.builder.Stmts.Expr(last)
	if exprStmt == nil || !exprStmt.Expr.IsValid() {
		return tail
	}
	tail.expr = exprStmt.Expr
	tail.span = tc.exprSpan(exprStmt.Expr)
	tail.bare = exprStmt.MissingSemicolon
	tail.typ = tc.result.ExprTypes[exprStmt.Expr]
	if tail.typ == types.NoTypeID || tail.typ == tc.types.Builtins().Nothing {
		return tail
	}
	if expr := tc.builder.Exprs.Get(tc.unwrapGroupExpr(exprStmt.Expr)); expr != nil {
		switch expr.Kind {
		case ast.ExprCall, ast.ExprAwait, ast.ExprSpawn, ast.ExprOn, ast.ExprAsync, ast.ExprBlocking:
			// Called for its effect; a discarded result is not a surprise.
		default:
			tail.value = true
		}
	}
	return tail
}
