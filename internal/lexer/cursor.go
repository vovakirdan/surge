package lexer

import (
	"fmt"
	"surge/internal/source"

	"fortio.org/safecast"
)

// Cursor представляет собой позицию в файле
type Cursor struct {
	File *source.File
	Off  uint32
}

func NewCursor(f *source.File) Cursor {
	return Cursor{
		File: f,
		Off:  0,
	}
}

// EOF проверяет, достигнут ли конец файла
func (c *Cursor) EOF() bool {
	lenFileContent, err := safecast.Conv[uint32](len(c.File.Content))
	if err != nil {
		panic(fmt.Errorf("len file content overflow: %w", err))
	}
	return c.Off >= lenFileContent
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
	lenFileContent, err := safecast.Conv[uint32](len(c.File.Content))
	if err != nil {
		panic(fmt.Errorf("len file content overflow: %w", err))
	}
	if c.Off+1 >= lenFileContent {
		return 0, 0, false
	}
	return c.File.Content[c.Off], c.File.Content[c.Off+1], true
}

// Peek3 читает текущий, следующий и следующий за ним байт, если есть, иначе возвращает 0, 0, 0, false
func (c *Cursor) Peek3() (b0, b1, b2 byte, ok bool) {
	lenFileContent, err := safecast.Conv[uint32](len(c.File.Content))
	if err != nil {
		panic(fmt.Errorf("len file content overflow: %w", err))
	}
	if c.Off+2 >= lenFileContent {
		return 0, 0, 0, false
	}
	return c.File.Content[c.Off], c.File.Content[c.Off+1], c.File.Content[c.Off+2], true
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
		File:  c.File.ID,
		Start: uint32(m),
		End:   c.Off,
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
