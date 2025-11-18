package lexer

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"fortio.org/safecast"
)

// ===== Работа с рунами поверх Cursor =====

// peekRune читает текущий байт как руну
func (lx *Lexer) peekRune() (r rune, size int) {
	if lx.cursor.EOF() {
		return utf8.RuneError, 0
	}
	b := lx.cursor.Peek()
	if b < utf8.RuneSelf { // fast-path ASCII
		return rune(b), 1
	}
	r, sz := utf8.DecodeRune(lx.file.Content[lx.cursor.Off:])
	return r, sz
}

// bumpRune читает текущий байт как руну и перемещает курсор на размер руны
func (lx *Lexer) bumpRune() {
	_, sz := lx.peekRune()
	if sz == 0 {
		return
	}
	usz, err := safecast.Conv[uint32](sz)
	if err != nil {
		panic(fmt.Errorf("bumpRune overflow: %w", err))
	}
	lx.cursor.Off += usz
}

// ===== Классификаторы =====

// ASCII fast-path для идентификаторов; Unicode — через isIdentStartRune/Continue.
func isIdentStartByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
func isIdentContinueByte(b byte) bool {
	return isIdentStartByte(b) || (b >= '0' && b <= '9')
}
func isIdentStartRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}
func isIdentContinueRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isDec(b byte) bool { return b >= '0' && b <= '9' }
func isHex(b byte) bool {
	return (b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'f') ||
		(b >= 'A' && b <= 'F')
}

// Проверка для кейса ".5": текущая точка, дальше цифра?
func (lx *Lexer) isNumberAfterDot() bool {
	b0, b1, ok := lx.cursor.Peek2()
	return ok && b0 == '.' && isDec(b1)
}

// ===== Матчеры последовательностей операторов (жадность) =====

// try2/try3 пробуют "съесть" 2/3 байта, если совпадает.
func (lx *Lexer) try3(a, b, c byte) bool {
	b0, b1, b2, ok := lx.cursor.Peek3()
	if !ok || b0 != a || b1 != b || b2 != c {
		return false
	}
	lx.cursor.Bump()
	lx.cursor.Bump()
	lx.cursor.Bump()
	return true
}

func (lx *Lexer) try2(a, b byte) bool {
	b0, b1, ok := lx.cursor.Peek2()
	if !ok || b0 != a || b1 != b {
		return false
	}
	lx.cursor.Bump()
	lx.cursor.Bump()
	return true
}
