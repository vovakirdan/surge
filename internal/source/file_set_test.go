package source

import (
	"testing"
	"os"
)

func TestFileSetVersioning(t *testing.T) {
	fs := NewFileSet()

	// Добавляем файл первый раз
	id1 := fs.Add("test.sg", []byte("hello world"), 0)
	if id1 != 0 {
		t.Errorf("Expected first FileID to be 0, got %d", id1)
	}

	// Проверяем, что GetLatest возвращает правильный ID
	latestID, exists := fs.GetLatest("test.sg")
	if !exists {
		t.Error("Expected file to exist after Add")
	}
	if latestID != id1 {
		t.Errorf("Expected latest ID to be %d, got %d", id1, latestID)
	}

	// Добавляем тот же файл с новым содержимым
	id2 := fs.Add("test.sg", []byte("hello universe"), 0)
	if id2 != 1 {
		t.Errorf("Expected second FileID to be 1, got %d", id2)
	}

	// Проверяем, что GetLatest теперь возвращает новый ID
	latestID, exists = fs.GetLatest("test.sg")
	if !exists {
		t.Error("Expected file to exist after second Add")
	}
	if latestID != id2 {
		t.Errorf("Expected latest ID to be %d, got %d", id2, latestID)
	}

	// Проверяем, что старый файл все еще доступен
	file1 := fs.Get(id1)
	if string(file1.Content) != "hello world" {
		t.Errorf("Expected first file content to be 'hello world', got '%s'", string(file1.Content))
	}

	// Проверяем, что новый файл имеет правильное содержимое
	file2 := fs.Get(id2)
	if string(file2.Content) != "hello universe" {
		t.Errorf("Expected second file content to be 'hello universe', got '%s'", string(file2.Content))
	}

	// Проверяем, что оба файла имеют одинаковый путь
	if file1.Path != "test.sg" || file2.Path != "test.sg" {
		t.Error("Expected both files to have the same path")
	}
}

// TestAddVirtualLineIdx проверяет правильность построения LineIdx для AddVirtual
func TestAddVirtualLineIdx(t *testing.T) {
	fs := NewFileSet()

	// Добавляем файл "a\nb\n" - должно быть LineIdx = [1,3]
	id := fs.AddVirtual("a.sg", []byte("a\nb\n"))
	file := fs.Get(id)

	expected := []uint32{1, 3} // позиции символов \n
	if len(file.LineIdx) != len(expected) {
		t.Errorf("Expected LineIdx length %d, got %d", len(expected), len(file.LineIdx))
	}

	for i, val := range expected {
		if file.LineIdx[i] != val {
			t.Errorf("Expected LineIdx[%d] = %d, got %d", i, val, file.LineIdx[i])
		}
	}

	// Проверяем флаг FileVirtual
	if file.Flags&FileVirtual == 0 {
		t.Error("Expected FileVirtual flag to be set")
	}
}

// TestCRLFNormalization проверяет нормализацию CRLF
func TestCRLFNormalization(t *testing.T) {
	fs := NewFileSet()

	// Тестируем "a\r\nb\r\n" → "a\nb\n"
	original := []byte("a\r\nb\r\n")
	normalized, changed := normalizeCRLF(original)

	if !changed {
		t.Error("Expected CRLF normalization to be detected")
	}

	expected := []byte("a\nb\n")
	if string(normalized) != string(expected) {
		t.Errorf("Expected normalized content %q, got %q", string(expected), string(normalized))
	}

	// Проверяем, что длина уменьшилась на количество замен
	originalLen := len(original)
	normalizedLen := len(normalized)
	expectedLen := originalLen - 2 // два \r\n заменены на \n
	if normalizedLen != expectedLen {
		t.Errorf("Expected length %d, got %d", expectedLen, normalizedLen)
	}

	// Тестируем через Load с флагом FileNormalizedCRLF
	id := fs.Add("test.sg", normalized, FileNormalizedCRLF)
	file := fs.Get(id)

	if file.Flags&FileNormalizedCRLF == 0 {
		t.Error("Expected FileNormalizedCRLF flag to be set")
	}
}

// TestBOMRemoval проверяет удаление BOM
func TestBOMRemoval(t *testing.T) {
	fs := NewFileSet()

	// Тестируем BOM + "x\n"
	bomContent := []byte{0xEF, 0xBB, 0xBF, 'x', '\n'}
	withoutBOM, hadBOM := removeBOM(bomContent)

	if !hadBOM {
		t.Error("Expected BOM to be detected")
	}

	expected := []byte{'x', '\n'}
	if string(withoutBOM) != string(expected) {
		t.Errorf("Expected content without BOM %q, got %q", string(expected), string(withoutBOM))
	}

	// Проверяем через Add с флагом FileHadBOM
	id := fs.Add("test.sg", withoutBOM, FileHadBOM)
	file := fs.Get(id)

	if file.Flags&FileHadBOM == 0 {
		t.Error("Expected FileHadBOM flag to be set")
	}
}

// TestResolveUTF8 проверяет разрешение позиций в UTF-8 тексте
func TestResolveUTF8(t *testing.T) {
	fs := NewFileSet()

	// Добавляем файл с UTF-8 символом "α\n" (α занимает 2 байта)
	content := []byte("α\n") // α = 2 байта, \n = 1 байт
	id := fs.AddVirtual("test.sg", content)

	// Resolve(Span{Start:0, End:1}) в "α\n"
	// Start=0 → позиция начала α (строка 1, колонка 1)
	// End=1 → позиция после первого байта α (строка 1, колонка 2)
	span := Span{File: id, Start: 0, End: 1}
	start, end := fs.Resolve(span)

	expectedStart := LineCol{Line: 1, Col: 1}
	expectedEnd := LineCol{Line: 1, Col: 2}

	if start != expectedStart {
		t.Errorf("Expected start %+v, got %+v", expectedStart, start)
	}

	if end != expectedEnd {
		t.Errorf("Expected end %+v, got %+v", expectedEnd, end)
	}
}

// TestFileVersioning проверяет версионирование файлов
func TestFileVersioning(t *testing.T) {
	fs := NewFileSet()

	// Первый вызов Add
	content1 := []byte("version 1")
	id1 := fs.Add("test.sg", content1, 0)

	// Проверяем, что index[path] указывает на первый файл
	latestID, exists := fs.GetLatest("test.sg")
	if !exists {
		t.Error("Expected file to exist")
	}
	if latestID != id1 {
		t.Errorf("Expected latest ID to be %d, got %d", id1, latestID)
	}

	// Второй вызов Add с тем же путем, но другим содержимым
	content2 := []byte("version 2")
	id2 := fs.Add("test.sg", content2, 0)

	// Проверяем, что получили новый FileID
	if id2 == id1 {
		t.Error("Expected different FileID for second Add")
	}

	// Проверяем, что index[path] теперь указывает на второй файл
	latestID, exists = fs.GetLatest("test.sg")
	if !exists {
		t.Error("Expected file to exist after second Add")
	}
	if latestID != id2 {
		t.Errorf("Expected latest ID to be %d, got %d", id2, latestID)
	}

	// Проверяем, что оба файла доступны и имеют правильное содержимое
	file1 := fs.Get(id1)
	file2 := fs.Get(id2)

	if string(file1.Content) != "version 1" {
		t.Errorf("Expected first file content 'version 1', got %q", string(file1.Content))
	}

	if string(file2.Content) != "version 2" {
		t.Errorf("Expected second file content 'version 2', got %q", string(file2.Content))
	}

	// Проверяем, что оба файла имеют одинаковый путь
	if file1.Path != file2.Path {
		t.Error("Expected both files to have the same path")
	}
}

// TestEdgeCases проверяет граничные случаи
func TestEdgeCases(t *testing.T) {
	fs := NewFileSet()

	// Пустой файл
	id1 := fs.AddVirtual("empty.sg", []byte{})
	file1 := fs.Get(id1)
	if len(file1.LineIdx) != 0 {
		t.Errorf("Expected empty LineIdx for empty file, got length %d", len(file1.LineIdx))
	}

	// Файл без переводов строк
	id2 := fs.AddVirtual("no_newlines.sg", []byte("hello"))
	file2 := fs.Get(id2)
	if len(file2.LineIdx) != 0 {
		t.Errorf("Expected empty LineIdx for file without newlines, got length %d", len(file2.LineIdx))
	}

	// Файл только с переводом строки
	id3 := fs.AddVirtual("only_newline.sg", []byte("\n"))
	file3 := fs.Get(id3)
	expected := []uint32{0}
	if len(file3.LineIdx) != 1 || file3.LineIdx[0] != expected[0] {
		t.Errorf("Expected LineIdx [0] for file with only newline, got %v", file3.LineIdx)
	}
}

func TestLoad(t *testing.T) {
	fs := NewFileSet()
	// создадим временный файл
	tempFile, err := os.CreateTemp("", "testdata")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	
	// запишем в него "a\nb\n"
	_, err = tempFile.WriteString("a\nb\n")
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	err = tempFile.Close()
	if err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	fs.Load(tempFile.Name())
	file := fs.Get(0)
	if string(file.Content) != "a\nb\n" {
		t.Errorf("Expected file content 'a\nb\n', got %q", string(file.Content))
	}
	if file.LineIdx[0] != 1 {
		t.Errorf("Expected LineIdx[0] to be 1, got %d", file.LineIdx[0])
	}
	if file.LineIdx[1] != 3 {
		t.Errorf("Expected LineIdx[1] to be 3, got %d", file.LineIdx[1])
	}
}

func TestLoadBOM(t *testing.T) {
	fs := NewFileSet()
	// создадим временный файл
	tempFile, err := os.CreateTemp("", "testdata")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())
	// запишем в него BOM + "a\nb\n"
	_, err = tempFile.WriteString("\xEF\xBB\xBFa\nb\n")
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	err = tempFile.Close()
	if err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	fs.Load(tempFile.Name())
	file := fs.Get(0)
	if string(file.Content) != "a\nb\n" {
		t.Errorf("Expected file content 'a\nb\n', got %q", string(file.Content))
	}
	if file.Flags&FileHadBOM == 0 {
		t.Error("Expected FileHadBOM flag to be set")
	}
}

func TestLoadCRLF(t *testing.T) {
	fs := NewFileSet()
	// создадим временный файл
	tempFile, err := os.CreateTemp("", "testdata")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// запишем в него "a\r\nb\r\n"
	_, err = tempFile.WriteString("a\r\nb\r\n")
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	err = tempFile.Close()
	if err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	fs.Load(tempFile.Name())
	file := fs.Get(0)
	if string(file.Content) != "a\nb\n" {
		t.Errorf("Expected file content 'a\nb\n', got %q", string(file.Content))
	}
	if file.Flags&FileNormalizedCRLF == 0 {
		t.Error("Expected FileNormalizedCRLF flag to be set")
	}
}
