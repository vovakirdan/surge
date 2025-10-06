package lexer

import (
	"bytes"
	"surge/internal/token"
)

// scanIdentOrKeyword сканирует [Ident] и мапит через LookupKeyword.
// Важно: Token.Text — ровно исходный срез, lower-casing делаем временно для Lookup.
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

	// Вызов LookupKeyword — понижая ASCII без лишних аллокаций
	lower := toLowerASCIIBytes(lex)
	if k, ok := token.LookupKeyword(string(lower)); ok {
		return token.Token{Kind: k, Span: sp, Text: string(lex)}
	}
	return token.Token{Kind: token.Ident, Span: sp, Text: string(lex)}
}

const utf8RuneSelf = 0x80

func toLowerASCIIBytes(b []byte) []byte {
	// Если нет верхних ASCII, вернём исходный срез (без копий).
	if bytes.IndexFunc(b, func(r rune) bool {
		return r >= 'A' && r <= 'Z'
	}) == -1 {
		return b
	}
	out := make([]byte, len(b))
	for i := range b {
		c := b[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		out[i] = c
	}
	return out
}
