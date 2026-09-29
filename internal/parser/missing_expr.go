package parser

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

// exprRecovery is the parser's memory of failed expressions: what is missing,
// how many failures the lexer already reported, whether the parser is in the
// rest of a statement whose failure is reported, and where the last error is.
type exprRecovery struct {
	missingExpr   missingExprMark
	lexedFailures uint
	quiet         bool
	lastErrorSpan source.Span
}

// missingExprMark remembers the last place where an expression was expected
// but none began. parsePrimaryExpr fails there without a diagnostic, because a
// caller that knows the context ("expected expression after '='") reports it
// better. A caller that recovers from the failure without such a report calls
// reportMissingExpr, so an accepted file never silently loses code.
type missingExprMark struct {
	span source.Span
	errs uint // reportedFailures() when the expression was missing
	set  bool
}

// reportedFailures counts the errors reported so far: the parser's own and the
// Invalid tokens a failed parse stopped at, which the lexer reported.
func (p *Parser) reportedFailures() uint {
	return p.opts.CurrentErrors + p.recovery.lexedFailures
}

// missingExpr records that no expression begins at the current token and
// fails the expression.
func (p *Parser) missingExpr() (ast.ExprID, bool) {
	if p.at(token.Invalid) {
		p.recovery.lexedFailures++
		p.recovery.missingExpr = missingExprMark{}
		return ast.NoExprID, false
	}
	p.recovery.missingExpr = missingExprMark{
		span: p.currentErrorSpan(),
		errs: p.reportedFailures(),
		set:  true,
	}
	return ast.NoExprID, false
}

// forgetMissingExpr drops the mark where a missing expression is legal.
func (p *Parser) forgetMissingExpr() {
	p.recovery.missingExpr = missingExprMark{}
}

// reportMissingExpr reports the recorded missing expression unless some error
// was reported after it, then forgets it. Recovery points call it before they
// skip tokens, so the diagnostic points at the token that could not start an
// expression.
//
// While recovery.quiet is set the parser is in the rest of a statement whose
// failure is already reported (resyncStatement stopped inside it), and a
// missing expression there is a cascade of that failure, not a new one.
func (p *Parser) reportMissingExpr() {
	mark := p.recovery.missingExpr
	p.recovery.missingExpr = missingExprMark{}
	if !mark.set || p.recovery.quiet || p.reportedFailures() != mark.errs || p.errorReportedAt(mark.span) {
		return
	}
	p.emitDiagnostic(diag.SynExpectExpression, diag.SevError, mark.span, "expected expression", nil)
}

// reportSilentFailure makes sure a construct that began when reportedFailures()
// was `before` and failed leaves a diagnostic: the missing expression if one
// was recorded, otherwise the token the parser stopped at.
func (p *Parser) reportSilentFailure(before uint) {
	p.reportMissingExpr()
	if p.recovery.quiet || p.reportedFailures() != before || p.at(token.Invalid) || p.errorReportedAt(p.currentErrorSpan()) {
		return
	}
	msg := "unexpected end of file"
	if tok := p.lx.Peek(); tok.Kind != token.EOF {
		msg = fmt.Sprintf("unexpected token '%s'", tok.Text)
	}
	p.emitDiagnostic(diag.SynUnexpectedToken, diag.SevError, p.currentErrorSpan(), msg, nil)
}

// parseStmtReported parses a statement of a statement list and makes sure a
// failed one leaves a diagnostic. A stray ';' holds no code: it fails without
// one, and the list's resync consumes it, so it stays an empty statement.
func (p *Parser) parseStmtReported() (ast.StmtID, bool) {
	if p.at(token.Semicolon) {
		return ast.NoStmtID, false
	}
	before := p.reportedFailures()
	stmtID, ok := p.parseStmt()
	if ok {
		p.reportMissingExpr()
	} else {
		p.reportSilentFailure(before)
	}
	p.recovery.quiet = false
	return stmtID, ok
}

// resyncBrokenStmt skips the rest of a failed statement, remembering whether
// it stopped inside that statement.
func (p *Parser) resyncBrokenStmt() {
	p.recovery.quiet = p.inRestOfBrokenStmt(p.resyncStatement())
}

// parseItemReported parses a top-level item and reports an expression that it
// failed to find and did not report. Only the mark is reported here: an item
// may fail without a diagnostic on purpose (a pragma).
func (p *Parser) parseItemReported() (ast.ItemID, bool) {
	itemID, ok := p.parseItem()
	p.reportMissingExpr()
	return itemID, ok
}

// inRestOfBrokenStmt reports whether the parser, after resyncStatement, stands
// inside the statement that failed rather than at the start of the next one.
func (p *Parser) inRestOfBrokenStmt(midStatement bool) bool {
	if !midStatement {
		return false
	}
	k := p.lx.Peek().Kind
	return k != token.Semicolon && k != token.RBrace && k != token.EOF && !isBlockStatementStarter(k)
}

// errorReportedAt reports whether the last error the parser reported starts
// where sp starts: a token that already carries an error (a missing ';' before
// a stray ')') needs no second one.
func (p *Parser) errorReportedAt(sp source.Span) bool {
	return p.opts.CurrentErrors > 0 && p.recovery.lastErrorSpan.File == sp.File && p.recovery.lastErrorSpan.Start == sp.Start
}
