package lexer

import (
	"surge/internal/source"
	"surge/internal/token"
)

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
			Span: lx.emptySpan(),
			Text: "",
		}
	}

	// 4) Посмотреть текущий байт и выбрать сканер
	ch := lx.cursor.Peek()
	var tok token.Token

	switch {
	case ch == '_':
		// Специальная обработка для underscore: если следующий символ не продолжение идента, то это токен Underscore
		b0, b1, ok := lx.cursor.Peek2()
		if ok && b0 == '_' && isIdentContinueByte(b1) {
			// "__foo" или "_123" → идентификатор
			tok = lx.scanIdentOrKeyword()
		} else {
			// одиночный "_" → токен Underscore
			tok = lx.scanOperatorOrPunct()
		}

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

	// 6) Вернуть токен
	return tok
}

// Peek возвращает следующий токен, не потребляя его.
func (lx *Lexer) Peek() token.Token {
	t := lx.Next()
	lx.look = &t
	return t
}

func (lx *Lexer) emptySpan() source.Span {
	return source.Span{File: lx.file.ID, Start: lx.cursor.Off, End: lx.cursor.Off}
}
