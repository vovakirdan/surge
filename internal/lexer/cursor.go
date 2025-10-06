package lexer

import (
	"surge/internal/source"
)

// Cursor представляет собой позицию в файле
type Cursor struct {
	File *source.File
	Off uint32
}

func NewCursor(f *source.File) Cursor {
	return Cursor{
		File: f,
		Off: 0,
	}
}

// EOF проверяет, достигнут ли конец файла
func (c *Cursor) EOF() bool {
	return c.Off >= uint32(len(c.File.Content))
}

// Peek читает текущий байт, если есть, иначе возвращает 0
func (c *Cursor) Peek() byte {
	if c.EOF() {
		return 0
	}
	return c.File.Content[c.Off]
}

// Peek2 читает текущий и следующий байт, если есть, иначе возвращает 0, 0, false
func (c *Cursor) Peek2() (b0, b1 byte, ok bool) {
	if c.Off + 1 >= uint32(len(c.File.Content)) {
		return 0, 0, false
	}
	return c.File.Content[c.Off], c.File.Content[c.Off + 1], true
}

// Bump перемещает курсор на один байт вперед и возвращает прочитанный байт
func (c *Cursor) Bump() byte {
	if c.EOF() {
		return 0
	}
	b := c.File.Content[c.Off]
	c.Off++
	return b
}

// Mark это метка, что бы быстро получать Span читаемого фрагмента
type Mark uint32

// Mark сохраняет текущую позицию курсора
func (c *Cursor) Mark() Mark {
	return Mark(c.Off)
}

// SpanFrom получает Span для фрагмента, начиная с метки
func (c *Cursor) SpanFrom(m Mark) source.Span {
	return source.Span{
		File: c.File.ID,
		Start: uint32(m),
		End: c.Off,
	}
}

// Reset возвращает курсор назад к метке
func (c *Cursor) Reset(m Mark) {
	c.Off = uint32(m)
}

func (c *Cursor) Eat(b byte) bool {
	if !c.EOF() && c.File.Content[c.Off] == b {
		c.Off++
		return true
	}
	return false
}
