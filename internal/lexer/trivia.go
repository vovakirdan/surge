package lexer

import (
	"surge/internal/diag"
	"surge/internal/token"
)

// collectLeadingTrivia собирает подряд идущие trivia перед значимым токеном.
// - ' ' и '\t' коалесцируются в один TriviaSpace
// - последовательные '\n' коалесцируются в один TriviaNewline
// - //... до \n -> TriviaLineComment
// - /* ... */ -> TriviaBlockComment (поддерживает вложенность; если не закрыта — репорт и обрезаем на EOF)
// - /// ... до \n -> TriviaDocLine (ДИРЕКТИВЫ ПОКА НЕ РАЗБИРАЕМ)
func (lx *Lexer) collectLeadingTrivia() {
	lx.hold = lx.hold[:0]
	for !lx.cursor.EOF() {
		start := lx.cursor.Mark()
		b := lx.cursor.Peek()

		// space/tabs
		if b == ' ' || b == '\t' {
			for {
				b2 := lx.cursor.Peek()
				if b2 != ' ' && b2 != '\t' {
					break
				}
				lx.cursor.Bump()
			}
			sp := lx.cursor.SpanFrom(start)
			lx.hold = append(lx.hold, token.Trivia{
				Kind: token.TriviaSpace,
				Span: sp,
				Text: string(lx.file.Content[sp.Start:sp.End]),
			})
			continue
		}

		// newlines (коалесцируем подряд)
		if b == '\n' {
			for lx.cursor.Peek() == '\n' {
				lx.cursor.Bump()
			}
			sp := lx.cursor.SpanFrom(start)
			lx.hold = append(lx.hold, token.Trivia{
				Kind: token.TriviaNewline,
				Span: sp,
				Text: string(lx.file.Content[sp.Start:sp.End]),
			})
			continue
		}

		// comments/doc
		if b == '/' {
			if lx.scanCommentOrDocLineIntoHold() {
				continue
			}
		}

		// нет больше trivia
		break
	}
}

// //... , /*...*/ , ///...
func (lx *Lexer) scanCommentOrDocLineIntoHold() bool {
	start := lx.cursor.Mark()
	if !lx.cursor.Eat('/') {
		return false
	}
	// "//" или "/*" или "/?"
	b := lx.cursor.Peek()
	switch b {
	case '/': // "//" или "///"
		lx.cursor.Bump()
		kind := token.TriviaLineComment
		// Если третий '/', считаем это DocLine (в этом шаге — не директива)
		if lx.cursor.Peek() == '/' {
			lx.cursor.Bump()
			kind = token.TriviaDocLine
		}
		for !lx.cursor.EOF() && lx.cursor.Peek() != '\n' {
			lx.cursor.Bump()
		}
		sp := lx.cursor.SpanFrom(start)
		lx.hold = append(lx.hold, token.Trivia{
			Kind: kind,
			Span: sp,
			Text: string(lx.file.Content[sp.Start:sp.End]),
		})
		return true

	case '*': // "/* ... */" (with nesting)
		lx.cursor.Bump()
		depth := 1
		for !lx.cursor.EOF() && depth > 0 {
			if b0, b1, ok := lx.cursor.Peek2(); ok {
				if b0 == '/' && b1 == '*' {
					lx.cursor.Bump()
					lx.cursor.Bump()
					depth++
					continue
				}
				if b0 == '*' && b1 == '/' {
					lx.cursor.Bump()
					lx.cursor.Bump()
					depth--
					continue
				}
			}
			lx.cursor.Bump()
		}
		sp := lx.cursor.SpanFrom(start)
		if depth > 0 {
			lx.errLex(diag.LexUnterminatedBlockComment, sp, "unterminated block comment")
		}
		lx.hold = append(lx.hold, token.Trivia{
			Kind: token.TriviaBlockComment,
			Span: sp,
			Text: string(lx.file.Content[sp.Start:sp.End]),
		})
		return true
	default:
		// это не комментарий — вернёмся, пусть сканируется как оператор '/'
		lx.cursor.Reset(start)
		return false
	}
}
