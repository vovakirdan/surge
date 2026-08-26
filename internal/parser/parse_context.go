package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/token"
)

// This file holds the parser's region-scoped toggles: state that a construct
// switches on for the extent of one sub-parse and that must not leak into the
// constructs nested inside it.

func (p *Parser) pushColonCastSuspension() {
	p.suspendColonCastDepths = append(p.suspendColonCastDepths, p.exprDepth+1)
}

func (p *Parser) popColonCastSuspension() {
	if n := len(p.suspendColonCastDepths); n > 0 {
		p.suspendColonCastDepths = p.suspendColonCastDepths[:n-1]
	}
}

func (p *Parser) colonCastSuspended() bool {
	if p.suspendColonCast > 0 {
		return true
	}
	for i := len(p.suspendColonCastDepths) - 1; i >= 0; i-- {
		if p.suspendColonCastDepths[i] == p.exprDepth {
			return true
		}
	}
	return false
}

// taskBodyScope tracks which async/blocking body, if any, the statements
// being parsed belong to DIRECTLY. `pending` is set by the async/blocking
// parser just before it opens the body's block; the next statement-list
// parser consumes it into `owner` for its own statements and clears it for
// every block nested inside, because an if body, a loop body or a block
// expression inside the body is not the body — a trailing expression there is
// its own business.
type taskBodyScope struct {
	pending string
	owner   string
}

// enterBlock is called on entry by every parser of a statement list; the
// value it returns restores the enclosing scope through leaveBlock.
func (p *Parser) enterBlock() string {
	saved := p.taskBody.owner
	p.taskBody.owner = p.taskBody.pending
	p.taskBody.pending = ""
	return saved
}

func (p *Parser) leaveBlock(saved string) {
	p.taskBody.owner = saved
}

// parseTaskBody parses the `{ ... }` body of an `async`/`blocking` block;
// `owner` is the keyword, named back to the author in SynTaskBodyBareValue.
func (p *Parser) parseTaskBody(owner string) (ast.StmtID, bool) {
	p.taskBody.pending = owner
	bodyID, ok := p.parseBlock()
	p.taskBody.pending = ""
	return bodyID, ok
}

// expectExprStmtSemicolon asks for the `;` that ends an expression
// statement. When the expression is the last thing in an async/blocking body
// — nothing but the body's `}` follows it — the missing `;` is not the
// mistake: the body gives its value with `ret <expr>;`, and inserting a `;`
// would make the task silently yield nothing. The parser is the first stage
// that knows which body this is, so it names that edit here
// (SynTaskBodyBareValue) and leaves the statement marked as missing its `;`,
// which keeps sema from diagnosing the same site again.
func (p *Parser) expectExprStmtSemicolon(exprID ast.ExprID) (token.Token, bool) {
	if owner := p.taskBody.owner; owner != "" && p.at(token.RBrace) {
		p.reportTaskBodyBareValue(owner, exprID)
		return token.Token{}, false
	}
	insertSpan := p.lastSpan.ZeroideToEnd()
	return p.expect(
		token.Semicolon,
		diag.SynExpectSemicolon,
		"expected ';' after expression statement",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			fixID := fix.MakeFixID(diag.SynExpectSemicolon, insertSpan)
			suggestion := fix.InsertText(
				"insert ';' after expression statement",
				insertSpan,
				";",
				"",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindRefactor),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
			)
			b.WithFixSuggestion(suggestion)
			b.WithNote(insertSpan, "insert missing semicolon")
		},
	)
}

func (p *Parser) reportTaskBodyBareValue(owner string, exprID ast.ExprID) {
	expr := p.arenas.Exprs.Get(exprID)
	if expr == nil {
		return
	}
	span := expr.Span
	text := "<expr>"
	if p.fs != nil {
		if file := p.fs.Get(span.File); file != nil && int(span.End) <= len(file.Content) && span.Start <= span.End {
			text = string(file.Content[span.Start:span.End])
		}
	}
	p.emitDiagnostic(
		diag.SynTaskBodyBareValue,
		diag.SevError,
		span,
		"the `"+owner+"` body gives its value with `ret`: write `ret "+text+";` — a trailing expression is not the body's value",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			// The only reading of a bare trailing expression in a task body is
			// "this is the body's value"; the edit that says so is the fix, and
			// it is applied without asking because no other program was meant.
			fixID := fix.MakeFixID(diag.SynTaskBodyBareValue, span)
			b.WithFixSuggestion(fix.WrapWith(
				"write `ret "+text+";`",
				span,
				"ret ",
				";",
				fix.WithID(fixID),
				fix.WithKind(diag.FixKindQuickFix),
				fix.WithApplicability(diag.FixApplicabilityAlwaysSafe),
				fix.Preferred(),
			))
			b.WithNote(span, "the `"+owner+"` body runs as its own task: `ret` leaves it with this value; `return` would try to leave the enclosing function and is refused there")
		},
	)
}
