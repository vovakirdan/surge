package parser

import (
	"strings"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
	"surge/internal/token"
)

func (p *Parser) parseFString() (ast.ExprID, bool) {
	tok := p.advance()
	if tok.Kind != token.FStringLit {
		p.err(diag.SynUnexpectedToken, "expected f-string literal")
		return ast.NoExprID, false
	}
	raw := tok.Text
	if len(raw) < 3 || raw[0] != 'f' || raw[1] != '"' || raw[len(raw)-1] != '"' {
		p.err(diag.SynUnexpectedToken, "invalid f-string literal")
		return ast.NoExprID, false
	}
	content := raw[2 : len(raw)-1]
	contentStart := tok.Span.Start + 2
	contentEnd := tok.Span.End - 1

	var format strings.Builder
	format.Grow(len(content))
	args := make([]ast.ExprID, 0, 4)
	offset := func(pos int) (uint32, bool) {
		off, err := safecast.Conv[uint32](pos)
		if err != nil {
			p.err(diag.SynUnexpectedToken, "f-string literal too large")
			return 0, false
		}
		return contentStart + off, true
	}

	for i := 0; i < len(content); {
		ch := content[i]
		if ch == '{' {
			if i+1 < len(content) && content[i+1] == '{' {
				format.WriteString("{{")
				i += 2
				continue
			}
			exprStart, ok := offset(i + 1)
			if !ok {
				return ast.NoExprID, false
			}
			exprID, closeSpan, ok := p.parseFStringExpr(tok.Span.File, exprStart, contentEnd)
			if !ok {
				return ast.NoExprID, false
			}
			format.WriteString("{}")
			args = append(args, p.wrapFmtArg(exprID))
			if closeSpan.End < contentStart {
				return ast.NoExprID, false
			}
			i = int(closeSpan.End - contentStart)
			continue
		}
		if ch == '}' {
			if i+1 < len(content) && content[i+1] == '}' {
				format.WriteString("}}")
				i += 2
				continue
			}
			start, ok := offset(i)
			if !ok {
				return ast.NoExprID, false
			}
			end, ok := offset(i + 1)
			if !ok {
				return ast.NoExprID, false
			}
			sp := source.Span{
				File:  tok.Span.File,
				Start: start,
				End:   end,
			}
			p.emitDiagnostic(diag.SynUnexpectedToken, diag.SevError, sp, "unmatched '}' in f-string", nil)
			return ast.NoExprID, false
		}
		format.WriteByte(ch)
		i++
	}

	formatLiteral := `"` + format.String() + `"`
	formatID := p.arenas.StringsInterner.Intern(formatLiteral)
	formatExpr := p.arenas.Exprs.NewLiteral(tok.Span, ast.ExprLitString, formatID)

	callArgs := make([]ast.CallArg, 0, len(args)+1)
	callArgs = append(callArgs, ast.CallArg{Name: source.NoStringID, Value: formatExpr})
	for _, arg := range args {
		callArgs = append(callArgs, ast.CallArg{Name: source.NoStringID, Value: arg})
	}

	formatName := p.arenas.StringsInterner.Intern("format")
	formatIdent := p.arenas.Exprs.NewIdent(tok.Span, formatName)
	callExpr := p.arenas.Exprs.NewCall(tok.Span, formatIdent, callArgs, nil, nil, false)
	return callExpr, true
}

func (p *Parser) wrapFmtArg(exprID ast.ExprID) ast.ExprID {
	expr := p.arenas.Exprs.Get(exprID)
	span := source.Span{}
	if expr != nil {
		span = expr.Span
	}
	fmtArgName := p.arenas.StringsInterner.Intern("fmt_arg")
	fmtArgIdent := p.arenas.Exprs.NewIdent(span, fmtArgName)
	args := []ast.CallArg{{Name: source.NoStringID, Value: exprID}}
	return p.arenas.Exprs.NewCall(span, fmtArgIdent, args, nil, nil, false)
}

func (p *Parser) parseFStringExpr(fileID source.FileID, start, limit uint32) (ast.ExprID, source.Span, bool) {
	if p.fs == nil {
		return ast.NoExprID, source.Span{}, false
	}
	file := p.fs.Get(fileID)
	if file == nil {
		return ast.NoExprID, source.Span{}, false
	}
	subLexer := lexer.New(file, lexer.Options{Reporter: p.opts.Reporter})
	subLexer.SetRange(start, limit)
	subParser := Parser{
		lx:       subLexer,
		arenas:   p.arenas,
		file:     p.file,
		fs:       p.fs,
		opts:     p.opts,
		lastSpan: source.Span{File: fileID, Start: start, End: start},
	}
	exprID, ok := subParser.parseExpr()
	if !ok || !exprID.IsValid() {
		return ast.NoExprID, source.Span{}, false
	}
	closeTok := subParser.lx.Peek()
	if closeTok.Kind != token.RBrace {
		sp := closeTok.Span
		if closeTok.Kind == token.EOF {
			sp = source.Span{File: fileID, Start: limit, End: limit}
		}
		p.emitDiagnostic(diag.SynUnclosedBrace, diag.SevError, sp, "expected '}' to close f-string expression", nil)
		return ast.NoExprID, source.Span{}, false
	}
	return exprID, closeTok.Span, true
}
