package lexer

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/token"
)

const maxTokenLength = 64 * 1024 // hard limit in bytes to avoid pathological tokens

type Lexer struct {
	file   *source.File
	cursor Cursor
	opts   Options
	look   *token.Token   // 1 элементный буфер для токена
	hold   []token.Trivia // накопленные leading trivia
}

func New(file *source.File, opts Options) *Lexer {
	return &Lexer{
		file:   file,
		cursor: NewCursor(file),
		opts:   opts,
		look:   nil,
		hold:   nil,
	}
}

// Next возвращает следующий **значимый** токен с уже собранным Leading.
// После EOF всегда возвращает EOF.
func (lx *Lexer) Next() token.Token {
	// 1) Если есть look — вернуть его и очистить
	if lx.look != nil {
		tok := *lx.look
		lx.look = nil
		return tok
	}

	// 2) collectLeadingTrivia() — набить lx.hold
	lx.collectLeadingTrivia()

	// 3) Если EOF → вернуть EOF (Leading из hold не приклеиваем к EOF)
	if lx.cursor.EOF() {
		return token.Token{
			Kind: token.EOF,
			Span: lx.EmptySpan(),
			Text: "",
		}
	}

	// 4) Посмотреть текущий байт и выбрать сканер
	ch := lx.cursor.Peek()
	var tok token.Token

	switch {
	case isIdentStartByte(ch):
		// ASCII буква → scanIdentOrKeyword()
		tok = lx.scanIdentOrKeyword()

	case ch >= utf8RuneSelf:
		// Возможный Unicode идентификатор → scanIdentOrKeyword() разберётся
		tok = lx.scanIdentOrKeyword()

	case isDec(ch):
		// цифра → scanNumber()
		tok = lx.scanNumber()

	case ch == '.' && lx.isNumberAfterDot():
		// . за которым цифра → scanNumber()
		tok = lx.scanNumber()

	case ch == '"':
		// " → scanString()
		tok = lx.scanString()

	default:
		// иначе → scanOperatorOrPunct() (включая @, скобки, запятые и т.д.)
		tok = lx.scanOperatorOrPunct()
	}

	// 5) В полученный token.Token положить Leading: lx.hold, обнулить hold
	tok.Leading = lx.hold
	lx.hold = nil

	lx.enforceTokenLength(&tok)

	// 6) Вернуть токен
	return tok
}

// Peek возвращает следующий токен, не потребляя его.
func (lx *Lexer) Peek() token.Token {
	t := lx.Next()
	lx.look = &t
	return t
}

// Push injects a token back into the lookahead buffer.
func (lx *Lexer) Push(tok token.Token) {
	lx.look = &tok
}

func (lx *Lexer) EmptySpan() source.Span {
	return source.Span{File: lx.file.ID, Start: lx.cursor.Off, End: lx.cursor.Off}
}

func (lx *Lexer) enforceTokenLength(tok *token.Token) {
	if tok == nil {
		return
	}
	length := tok.Span.End - tok.Span.Start
	if length <= maxTokenLength {
		return
	}
	msg := fmt.Sprintf("token length %d exceeds limit %d", length, maxTokenLength)
	lx.errLex(diag.LexTokenTooLong, tok.Span, msg)
	tok.Kind = token.Invalid
	if tok.Text == "" && tok.Span.End > tok.Span.Start && int(tok.Span.End) <= len(lx.file.Content) {
		tok.Text = string(lx.file.Content[tok.Span.Start:tok.Span.End])
	}
	// Fast-forward to EOF to avoid cascading work on a pathological token.
	if off, err := safecast.Conv[uint32](len(lx.file.Content)); err == nil {
		lx.cursor.Off = off
	}
}
