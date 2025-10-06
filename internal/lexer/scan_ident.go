package lexer

import (
	"surge/internal/token"
)

const utf8RuneSelf = 0x80

// scanIdentOrKeyword сканирует [Ident] и проверяет через LookupKeyword.
// Ключевые слова регистрозависимые (только lowercase). Token.Text — ровно исходный срез.
func (lx *Lexer) scanIdentOrKeyword() token.Token {
	start := lx.cursor.Mark()

	// Первый символ: ASCII fast-path или Unicode
	r, sz := lx.peekRune()
	if sz == 0 {
		sp := lx.cursor.SpanFrom(start)
		return token.Token{Kind: token.Invalid, Span: sp, Text: ""}
	}
	if r < utf8RuneSelf {
		// ASCII
		if !isIdentStartByte(byte(r)) {
			// fallback на оператор
			return lx.scanOperatorOrPunct()
		}
		lx.cursor.Bump()
		for {
			b := lx.cursor.Peek()
			if !(isIdentContinueByte(b)) {
				break
			}
			lx.cursor.Bump()
		}
	} else {
		// Unicode
		if !isIdentStartRune(r) {
			return lx.scanOperatorOrPunct()
		}
		lx.bumpRune()
		for {
			r2, sz2 := lx.peekRune()
			if sz2 == 0 || !isIdentContinueRune(r2) {
				break
			}
			lx.bumpRune()
		}
	}

	sp := lx.cursor.SpanFrom(start)
	lex := lx.file.Content[sp.Start:sp.End]

	if len(lex) == 1 && lex[0] == '_' {
		return token.Token{Kind: token.Underscore, Span: sp, Text: string(lex)}
	}

	// Проверка на ключевое слово (регистрозависимо)
	if k, ok := token.LookupKeyword(string(lex)); ok {
		return token.Token{Kind: k, Span: sp, Text: string(lex)}
	}
	return token.Token{Kind: token.Ident, Span: sp, Text: string(lex)}
}
