package parser

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/token"
)

// bareGenericTail names what follows `Ident<T>` when the `<` was meant to open
// a type-argument list rather than to compare.
type bareGenericTail int

const (
	// bareGenericNone means the shape is not recognisable as generic
	// arguments, so it stays an ordinary comparison.
	bareGenericNone bareGenericTail = iota
	// bareGenericCall is `Object<T>(...)`.
	bareGenericCall
	// bareGenericPath is `Object<T>::f(...)`.
	bareGenericPath
)

// looksLikeBareGenericArgs recognises type arguments written without the
// turbofish, before the `<` is consumed as an operator.
//
// The scan is over source bytes rather than tokens because the lexer looks
// ahead two tokens and `< T >` needs three. It bails out on `;`, a newline,
// `)` or `{` so the window stays inside one expression.
//
// Only two tails are claimed. `(` after `>` is the historic case and can also
// be a legitimate comparison — `f(a<b, c>(d))` compares twice — so it is
// reported but never rewritten. `::` after `>` has no legal reading at all:
// `::` does not start an expression, so `a < b > ::c` cannot parse, which is
// what makes recovery safe there.
//
// `.` is deliberately NOT claimed: `(a<b)>c.f()` is an ordinary comparison
// against a method result, and no byte in the window tells the two apart.
func (p *Parser) looksLikeBareGenericArgs(expr ast.ExprID) (bareGenericTail, token.Token) {
	if expr == ast.NoExprID || p.lx.Peek().Kind != token.Lt || p.fs == nil {
		return bareGenericNone, token.Token{}
	}
	node := p.arenas.Exprs.Get(expr)
	if node == nil || (node.Kind != ast.ExprIdent && node.Kind != ast.ExprMember) {
		return bareGenericNone, token.Token{}
	}
	ltTok := p.lx.Peek()
	file := p.fs.Get(ltTok.Span.File)
	if file == nil {
		return bareGenericNone, ltTok
	}
	data := file.Content
	if int(ltTok.Span.End) >= len(data) {
		return bareGenericNone, ltTok
	}
	i := skipHorizontalSpace(data, int(ltTok.Span.End))
	if i >= len(data) || !isTypeStartByte(data[i]) {
		return bareGenericNone, ltTok
	}
	foundGt := false
	for i < len(data) {
		switch data[i] {
		case '>':
			foundGt = true
			i++
			goto afterGt
		case ';', '\n', '\r', ')', '{':
			return bareGenericNone, ltTok
		}
		i++
	}
afterGt:
	if !foundGt {
		return bareGenericNone, ltTok
	}
	i = skipHorizontalSpace(data, i)
	if i >= len(data) {
		return bareGenericNone, ltTok
	}
	if data[i] == '(' {
		return bareGenericCall, ltTok
	}
	if data[i] == ':' && i+1 < len(data) && data[i+1] == ':' {
		return bareGenericPath, ltTok
	}
	return bareGenericNone, ltTok
}

func skipHorizontalSpace(data []byte, i int) int {
	for i < len(data) {
		if data[i] != ' ' && data[i] != '\t' && data[i] != '\n' && data[i] != '\r' {
			break
		}
		i++
	}
	return i
}

func isTypeStartByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b == '_', b == '&', b == '*', b == '(':
		return true
	default:
		return false
	}
}

// reportBareGenericArgs names the edit that fixes the call, explains why the
// compiler read the line the way it did, and offers the insertion.
//
// The fix is Preferred rather than AlwaysSafe: the `(` tail shares its shape
// with a legitimate double comparison, and one applicability for both tails
// keeps a single answer to the same mistake.
func (p *Parser) reportBareGenericArgs(calleeSpan source.Span, ltTok token.Token) {
	p.emitDiagnostic(
		diag.SynGenericArgsNeedTurbofish,
		diag.SevError,
		ltTok.Span,
		"generic type arguments must use '::<' syntax",
		func(b *diag.ReportBuilder) {
			if b == nil {
				return
			}
			if calleeSpan != (source.Span{}) {
				b.WithFixSuggestion(fix.InsertText(
					"insert '::' for generic call",
					calleeSpan.ZeroideToEnd().ZeroideToStart(),
					"::",
					"",
					fix.WithApplicability(diag.FixApplicabilitySafeWithHeuristics),
					fix.Preferred(),
				))
			}
			b.WithNote(ltTok.Span, "without '::' this '<' is the less-than operator, so the line reads as a comparison")
		},
	)
}
