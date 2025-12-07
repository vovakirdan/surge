package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/token"
)

// parseTernaryExpr parses: condition ? true_expr : false_expr
// The condition has already been parsed and passed in as `cond`.
func (p *Parser) parseTernaryExpr(cond ast.ExprID) (ast.ExprID, bool) {
	p.advance() // consume '?'

	// Suspend colon cast to allow ':' in ternary expression
	p.suspendColonCast++
	defer func() { p.suspendColonCast-- }()

	// Parse true branch (ternary is right-associative at precTernary level)
	trueExpr, ok := p.parseBinaryExpr(precTernary)
	if !ok {
		p.err(diag.SynExpectExpression, "expected expression after '?'")
		return ast.NoExprID, false
	}

	// Expect ':'
	if !p.at(token.Colon) {
		p.err(diag.SynUnexpectedToken, "expected ':' in ternary expression")
		return ast.NoExprID, false
	}
	p.advance() // consume ':'

	// Parse false branch (right-associative at same precedence level)
	falseExpr, ok := p.parseBinaryExpr(precTernary)
	if !ok {
		p.err(diag.SynExpectExpression, "expected expression after ':'")
		return ast.NoExprID, false
	}

	// Span from condition to false branch
	condSpan := p.arenas.Exprs.Get(cond).Span
	falseSpan := p.arenas.Exprs.Get(falseExpr).Span

	return p.arenas.Exprs.NewTernary(condSpan.Cover(falseSpan), cond, trueExpr, falseExpr), true
}
