package lexer

import (
	"surge/internal/source"
	"testing"
)

// helper function to create a file
func createFile(content string) *source.File {
	fs := source.NewFileSet()
	id := fs.AddVirtual("test.sg", []byte(content))
	return fs.Get(id)
}

// TestSequentialReading проверяет последовательное чтение: "a\nb" → a, \n, b, EOF
func TestSequentialReading(t *testing.T) {
	file := createFile("a\nb")
	cursor := NewCursor(file)

	// Читаем первый символ 'a'
	if cursor.EOF() {
		t.Error("Expected not EOF at start")
	}
	if cursor.Peek() != 'a' {
		t.Errorf("Expected peek 'a', got %c", cursor.Peek())
	}
	b := cursor.Bump()
	if b != 'a' {
		t.Errorf("Expected bump 'a', got %c", b)
	}

	// Читаем символ новой строки '\n'
	if cursor.EOF() {
		t.Error("Expected not EOF after 'a'")
	}
	if cursor.Peek() != '\n' {
		t.Errorf("Expected peek '\\n', got %c", cursor.Peek())
	}
	b = cursor.Bump()
	if b != '\n' {
		t.Errorf("Expected bump '\\n', got %c", b)
	}

	// Читаем последний символ 'b'
	if cursor.EOF() {
		t.Error("Expected not EOF after '\\n'")
	}
	if cursor.Peek() != 'b' {
		t.Errorf("Expected peek 'b', got %c", cursor.Peek())
	}
	b = cursor.Bump()
	if b != 'b' {
		t.Errorf("Expected bump 'b', got %c", b)
	}

	// Проверяем EOF
	if !cursor.EOF() {
		t.Error("Expected EOF at end")
	}
	if cursor.Peek() != 0 {
		t.Errorf("Expected peek 0 at EOF, got %c", cursor.Peek())
	}
	b = cursor.Bump()
	if b != 0 {
		t.Errorf("Expected bump 0 at EOF, got %c", b)
	}
}

// TestPeek2 проверяет Peek2 на середине и конце файла
func TestPeek2(t *testing.T) {
	file := createFile("abc")
	cursor := NewCursor(file)

	// Peek2 в начале файла
	b0, b1, ok := cursor.Peek2()
	if !ok {
		t.Error("Expected Peek2 to succeed at start")
	}
	if b0 != 'a' || b1 != 'b' {
		t.Errorf("Expected Peek2('a', 'b'), got ('%c', '%c')", b0, b1)
	}

	// Перемещаемся на середину
	cursor.Bump() // 'a'

	// Peek2 в середине файла
	b0, b1, ok = cursor.Peek2()
	if !ok {
		t.Error("Expected Peek2 to succeed in middle")
	}
	if b0 != 'b' || b1 != 'c' {
		t.Errorf("Expected Peek2('b', 'c'), got ('%c', '%c')", b0, b1)
	}

	// Перемещаемся к концу
	cursor.Bump() // 'b'

	// Peek2 в конце файла (должен вернуть false)
	b0, b1, ok = cursor.Peek2()
	if ok {
		t.Error("Expected Peek2 to fail at end")
	}
	if b0 != 0 || b1 != 0 {
		t.Errorf("Expected Peek2(0, 0) at end, got ('%c', '%c')", b0, b1)
	}
}

// TestSpanFromResolve проверяет SpanFrom и Resolve с UTF-8
func TestSpanFromResolve(t *testing.T) {
	// Создаем файл с UTF-8 символом "α\nβ" (α=2 байта, \n=1 байт, β=2 байта)
	file := createFile("α\nβ")
	fs := source.NewFileSet()
	fs.AddVirtual("test.sg", []byte("α\nβ"))

	cursor := NewCursor(file)

	// Ставим метку в начале
	mark := cursor.Mark()

	// Читаем первый символ α (2 байта)
	cursor.Bump() // первый байт α
	cursor.Bump() // второй байт α

	// Получаем Span для прочитанного фрагмента
	span := cursor.SpanFrom(mark)

	// Проверяем Span
	if span.Start != 0 {
		t.Errorf("Expected span.Start = 0, got %d", span.Start)
	}
	if span.End != 2 {
		t.Errorf("Expected span.End = 2, got %d", span.End)
	}

	// Проверяем Resolve через FileSet
	start, end := fs.Resolve(span)
	expectedStart := source.LineCol{Line: 1, Col: 1}
	expectedEnd := source.LineCol{Line: 2, Col: 0} // позиция символа \n

	if start != expectedStart {
		t.Errorf("Expected start %+v, got %+v", expectedStart, start)
	}
	if end != expectedEnd {
		t.Errorf("Expected end %+v, got %+v", expectedEnd, end)
	}

	// Тестируем Span для символа новой строки
	mark2 := cursor.Mark()
	cursor.Bump() // '\n'
	span2 := cursor.SpanFrom(mark2)

	if span2.Start != 2 || span2.End != 3 {
		t.Errorf("Expected span2 (2,3), got (%d,%d)", span2.Start, span2.End)
	}

	start2, end2 := fs.Resolve(span2)
	expectedStart2 := source.LineCol{Line: 2, Col: 0} // позиция символа \n (строка 2, колонка 0)
	expectedEnd2 := source.LineCol{Line: 2, Col: 1}   // после \n

	if start2 != expectedStart2 {
		t.Errorf("Expected start2 %+v, got %+v", expectedStart2, start2)
	}
	if end2 != expectedEnd2 {
		t.Errorf("Expected end2 %+v, got %+v", expectedEnd2, end2)
	}
}

// TestEatNewline проверяет поведение Eat('\n')
func TestEatNewline(t *testing.T) {
	file := createFile("a\nb")
	cursor := NewCursor(file)

	// Пытаемся съесть 'a' - должно сработать
	if !cursor.Eat('a') {
		t.Error("Expected Eat('a') to succeed")
	}
	if cursor.Peek() != '\n' {
		t.Errorf("Expected peek '\\n' after Eat('a'), got %c", cursor.Peek())
	}

	// Пытаемся съесть '\n' - должно сработать
	if !cursor.Eat('\n') {
		t.Error("Expected Eat('\\n') to succeed")
	}
	if cursor.Peek() != 'b' {
		t.Errorf("Expected peek 'b' after Eat('\\n'), got %c", cursor.Peek())
	}

	// Пытаемся съесть 'b' - должно сработать
	if !cursor.Eat('b') {
		t.Error("Expected Eat('b') to succeed")
	}
	if !cursor.EOF() {
		t.Error("Expected EOF after Eat('b')")
	}

	// Пытаемся съесть что-то в EOF - не должно сработать
	if cursor.Eat('x') {
		t.Error("Expected Eat('x') at EOF to fail")
	}

	// Пытаемся съесть неправильный символ
	cursor.Reset(Mark(0)) // возвращаемся к началу
	if cursor.Eat('x') {
		t.Error("Expected Eat('x') to fail when current char is 'a'")
	}
	if cursor.Peek() != 'a' {
		t.Errorf("Expected cursor position unchanged after failed Eat, got %c", cursor.Peek())
	}
}

// TestMarkReset проверяет работу Mark и Reset
func TestMarkReset(t *testing.T) {
	file := createFile("abc")
	cursor := NewCursor(file)

	// Ставим метку в начале
	mark1 := cursor.Mark()

	// Читаем первый символ
	cursor.Bump()

	// Ставим вторую метку
	mark2 := cursor.Mark()

	// Читаем еще символ
	cursor.Bump()

	// Возвращаемся ко второй метке
	cursor.Reset(mark2)
	if cursor.Peek() != 'b' {
		t.Errorf("Expected peek 'b' after reset to mark2, got %c", cursor.Peek())
	}

	// Возвращаемся к первой метке
	cursor.Reset(mark1)
	if cursor.Peek() != 'a' {
		t.Errorf("Expected peek 'a' after reset to mark1, got %c", cursor.Peek())
	}
}
