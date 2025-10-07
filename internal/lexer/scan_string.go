package lexer

import (
	"surge/internal/diag"
	"surge/internal/token"
)

// Минимум: "..." (поддержка escape \' \" \\ \n \t \r \xNN \u{...} — можно частично; ошибки → Reporter).
func (lx *Lexer) scanString() token.Token {
	start := lx.cursor.Mark()
	lx.cursor.Bump() // opening '"'
	for !lx.cursor.EOF() {
		b := lx.cursor.Peek()
		if b == '"' {
			lx.cursor.Bump()
			sp := lx.cursor.SpanFrom(start)
			return token.Token{Kind: token.StringLit, Span: sp, Text: string(lx.file.Content[sp.Start:sp.End])}
		}
		if b == '\\' {
			// грубая обработка escape: съесть '\' и следующий байт, не валидируем глубоко здесь
			lx.cursor.Bump()
			if lx.cursor.EOF() {
				break
			}
			lx.cursor.Bump()
			continue
		}
		if b == '\n' {
			// в этой версии — ошибка: перевод строки в строковом литерале
			sp := lx.cursor.SpanFrom(start)
			lx.errLex(diag.LexUnterminatedString, sp, "newline in string literal")
			return token.Token{Kind: token.Invalid, Span: sp, Text: string(lx.file.Content[sp.Start:sp.End])}
		}
		lx.cursor.Bump()
	}
	// EOF без закрывающей кавычки
	sp := lx.cursor.SpanFrom(start)
	lx.errLex(diag.LexUnterminatedString, sp, "unterminated string literal")
	return token.Token{Kind: token.Invalid, Span: sp, Text: string(lx.file.Content[sp.Start:sp.End])}
}
