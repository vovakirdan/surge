package lexer

import (
	"surge/internal/token"
)
// Жадность: сначала 3-символьные, затем 2-символьные,
// затем 1-символьные. Набор из token.Kind (.., ..=, ::, ->, =>, &&, ||, ==, !=, <=, >=, <<, >>, ...).
func (lx *Lexer) scanOperatorOrPunct() token.Token {
	start := lx.cursor.Mark()
	emit := func(k token.Kind) token.Token {
		sp := lx.cursor.SpanFrom(start)
		return token.Token{
			Kind: k,
			Span: sp,
			Text: string(lx.file.Content[sp.Start:sp.End]),
		}
	}

	// диапазоны и стрелки, логика И/ИЛИ, сравнения, сдвиги, namespace
	switch {
	case lx.try3('.', '.', '='):
		return emit(token.DotDotEq)
	case lx.try3('.', '.', '.'):
		return emit(token.DotDotDot)
	case lx.try2('.', '.'):
		return emit(token.DotDot) // ..
	case lx.try2(':', ':'):
		return emit(token.ColonColon)
	case lx.try2('-', '>'):
		return emit(token.Arrow)
	case lx.try2('=', '>'):
		return emit(token.FatArrow)
	case lx.try2('&', '&'):
		return emit(token.AndAnd)
	case lx.try2('|', '|'):
		return emit(token.OrOr)
	case lx.try2('=', '='):
		return emit(token.EqEq)
	case lx.try2('!', '='):
		return emit(token.BangEq)
	case lx.try2('<', '='):
		return emit(token.LtEq)
	case lx.try2('>', '='):
		return emit(token.GtEq)
	case lx.try2('<', '<'):
		return emit(token.Shl)
	case lx.try2('>', '>'):
		return emit(token.Shr)
	}

	// односимвольные
	ch := lx.cursor.Bump()
	switch ch {
	case '+':
		return emit(token.Plus)
	case '-':
		return emit(token.Minus)
	case '*':
		return emit(token.Star)
	case '/':
		return emit(token.Slash)
	case '%':
		return emit(token.Percent)
	case '=':
		return emit(token.Assign)
	case '!':
		return emit(token.Bang)
	case '<':
		return emit(token.Lt)
	case '>':
		return emit(token.Gt)
	case '&':
		return emit(token.Amp)
	case '|':
		return emit(token.Pipe)
	case '^':
		return emit(token.Caret)
	case '?':
		return emit(token.Question)
	case ':':
		return emit(token.Colon)
	case ';':
		return emit(token.Semicolon)
	case ',':
		return emit(token.Comma)
	case '.':
		return emit(token.Dot)
	case '(':
		return emit(token.LParen)
	case ')':
		return emit(token.RParen)
	case '{':
		return emit(token.LBrace)
	case '}':
		return emit(token.RBrace)
	case '[':
		return emit(token.LBracket)
	case ']':
		return emit(token.RBracket)
	case '@':
		return emit(token.At)
	case '_':
		return emit(token.Underscore)
	default:
		// неизвестный символ
		sp := lx.cursor.SpanFrom(start)
		lx.report("UnknownChar", sp, "unknown character")
		return token.Token{Kind: token.Invalid, Span: sp, Text: string(lx.file.Content[sp.Start:sp.End])}
	}
}
