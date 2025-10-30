package diagfmt

import (
	"bytes"
	"encoding/json"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

// TestJSONBasic проверяет базовое JSON форматирование
func TestJSONBasic(t *testing.T) {
	fs := source.NewFileSet()
	content := []byte(`fn main() {
	let x = "unterminated
}`)
	fileID := fs.AddVirtual("test.sg", content)

	bag := diag.NewBag(10)
	d := diag.New(
		diag.SevError,
		diag.LexUnterminatedString,
		source.Span{File: fileID, Start: 21, End: 33},
		"Unterminated string literal",
	)
	bag.Add(d)

	var buf bytes.Buffer
	opts := JSONOpts{
		IncludePositions: true,
		PathMode:         PathModeBasename,
		Max:              0,
		IncludeNotes:     true,
		IncludeFixes:     true,
	}

	err := JSON(&buf, bag, fs, opts)
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	// Парсим JSON чтобы убедиться что он валидный
	var output DiagnosticsOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Invalid JSON output: %v\nOutput: %s", err, buf.String())
	}

	// Проверяем базовые поля
	if output.Count != 1 {
		t.Errorf("Expected count=1, got %d", output.Count)
	}

	if len(output.Diagnostics) != 1 {
		t.Fatalf("Expected 1 diagnostic, got %d", len(output.Diagnostics))
	}

	diag := output.Diagnostics[0]
	if diag.Severity != "ERROR" {
		t.Errorf("Expected severity=ERROR, got %s", diag.Severity)
	}

	if diag.Code != "LEX1002" {
		t.Errorf("Expected code=LEX1002, got %s", diag.Code)
	}

	if diag.Message != "Unterminated string literal" {
		t.Errorf("Expected message='Unterminated string literal', got %s", diag.Message)
	}

	if diag.Location.File != "test.sg" {
		t.Errorf("Expected file=test.sg, got %s", diag.Location.File)
	}

	if diag.Location.StartByte != 21 {
		t.Errorf("Expected start_byte=21, got %d", diag.Location.StartByte)
	}

	if diag.Location.EndByte != 33 {
		t.Errorf("Expected end_byte=33, got %d", diag.Location.EndByte)
	}

	// Проверяем позиции
	if diag.Location.StartLine != 2 {
		t.Errorf("Expected start_line=2, got %d", diag.Location.StartLine)
	}

	if diag.Location.StartCol != 10 {
		t.Errorf("Expected start_col=10, got %d", diag.Location.StartCol)
	}
}

// TestJSONWithNotesAndFixes проверяет JSON с заметками и исправлениями
func TestJSONWithNotesAndFixes(t *testing.T) {
	fs := source.NewFileSet()
	content := []byte(`let x = 42`)
	fileID := fs.AddVirtual("test.sg", content)

	bag := diag.NewBag(10)
	d := diag.New(
		diag.SevWarning,
		diag.LexUnknownChar,
		source.Span{File: fileID, Start: 4, End: 5},
		"Unused variable",
	)

	// Добавляем заметку
	d = d.WithNote(
		source.Span{File: fileID, Start: 4, End: 5},
		"Consider removing this variable or prefixing with underscore",
	)

	// Добавляем исправление
	d = d.WithFix(
		"Remove unused variable",
		diag.FixEdit{
			Span:    source.Span{File: fileID, Start: 0, End: 10},
			NewText: "",
		},
	)

	bag.Add(d)

	var buf bytes.Buffer
	opts := JSONOpts{
		IncludePositions: true,
		PathMode:         PathModeBasename,
		Max:              0,
		IncludeNotes:     true,
		IncludeFixes:     true,
	}

	err := JSON(&buf, bag, fs, opts)
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	var output DiagnosticsOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	if len(output.Diagnostics) != 1 {
		t.Fatalf("Expected 1 diagnostic, got %d", len(output.Diagnostics))
	}

	diag := output.Diagnostics[0]

	// Проверяем заметки
	if len(diag.Notes) != 1 {
		t.Fatalf("Expected 1 note, got %d", len(diag.Notes))
	}

	note := diag.Notes[0]
	if note.Message != "Consider removing this variable or prefixing with underscore" {
		t.Errorf("Unexpected note message: %s", note.Message)
	}

	// Проверяем исправления
	if len(diag.Fixes) != 1 {
		t.Fatalf("Expected 1 fix, got %d", len(diag.Fixes))
	}

	fix := diag.Fixes[0]
	if fix.Title != "Remove unused variable" {
		t.Errorf("Unexpected fix title: %s", fix.Title)
	}

	if len(fix.Edits) != 1 {
		t.Fatalf("Expected 1 edit, got %d", len(fix.Edits))
	}

	edit := fix.Edits[0]
	if edit.NewText != "" {
		t.Errorf("Expected empty new_text, got %s", edit.NewText)
	}
	if edit.OldText != "" {
		t.Errorf("Expected old_text to be empty, got %s", edit.OldText)
	}
	if fix.Kind != "QUICK_FIX" {
		t.Errorf("Expected kind QUICK_FIX, got %s", fix.Kind)
	}
	if fix.Applicability != "ALWAYS_SAFE" {
		t.Errorf("Expected applicability ALWAYS_SAFE, got %s", fix.Applicability)
	}
	if fix.IsPreferred {
		t.Errorf("Expected is_preferred to be false")
	}
	if fix.BuildError != "" {
		t.Errorf("Unexpected build error: %s", fix.BuildError)
	}
}

// TestJSONWithoutPositions проверяет JSON без позиций строк/колонок
func TestJSONWithoutPositions(t *testing.T) {
	fs := source.NewFileSet()
	content := []byte("let x = 42")
	fileID := fs.AddVirtual("test.sg", content)

	bag := diag.NewBag(10)
	d := diag.New(
		diag.SevInfo,
		diag.LexUnknownChar,
		source.Span{File: fileID, Start: 4, End: 5},
		"Info message",
	)
	bag.Add(d)

	var buf bytes.Buffer
	opts := JSONOpts{
		IncludePositions: false,
		PathMode:         PathModeBasename,
		Max:              0,
	}

	err := JSON(&buf, bag, fs, opts)
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	var output DiagnosticsOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	diag := output.Diagnostics[0]

	// Проверяем что позиций нет в JSON (omitempty должен их скрыть)
	if diag.Location.StartLine != 0 {
		t.Errorf("Expected start_line to be omitted (0), got %d", diag.Location.StartLine)
	}

	// Но байтовые позиции должны быть всегда
	if diag.Location.StartByte != 4 {
		t.Errorf("Expected start_byte=4, got %d", diag.Location.StartByte)
	}
}

// TestJSONMaxLimit проверяет ограничение количества диагностик
func TestJSONMaxLimit(t *testing.T) {
	fs := source.NewFileSet()
	content := []byte("test content")
	fileID := fs.AddVirtual("test.sg", content)

	bag := diag.NewBag(10)

	// Добавляем 5 диагностик
	for i := range 5 {
		d := diag.New(
			diag.SevError,
			diag.LexUnknownChar,
			source.Span{File: fileID, Start: uint32(i), End: uint32(i + 1)},
			"Error message",
		)
		bag.Add(d)
	}

	var buf bytes.Buffer
	opts := JSONOpts{
		IncludePositions: false,
		PathMode:         PathModeBasename,
		Max:              3, // Ограничение в 3 диагностики
	}

	err := JSON(&buf, bag, fs, opts)
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	var output DiagnosticsOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	if output.Count != 3 {
		t.Errorf("Expected count=3 (limited), got %d", output.Count)
	}

	if len(output.Diagnostics) != 3 {
		t.Errorf("Expected 3 diagnostics (limited), got %d", len(output.Diagnostics))
	}
}

// TestJSONPathModes проверяет различные режимы путей
func TestJSONPathModes(t *testing.T) {
	fs := source.NewFileSet()
	fs.SetBaseDir("/home/user/project")

	content := []byte("test")
	fileID := fs.AddVirtual("/home/user/project/src/main.sg", content)

	bag := diag.NewBag(10)
	d := diag.New(
		diag.SevError,
		diag.LexUnknownChar,
		source.Span{File: fileID, Start: 0, End: 1},
		"Error",
	)
	bag.Add(d)

	tests := []struct {
		name     string
		pathMode PathMode
		expected string
	}{
		{"Absolute", PathModeAbsolute, "/home/user/project/src/main.sg"},
		{"Relative", PathModeRelative, "src/main.sg"},
		{"Basename", PathModeBasename, "main.sg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := JSONOpts{
				IncludePositions: false,
				PathMode:         tt.pathMode,
				Max:              0,
			}

			err := JSON(&buf, bag, fs, opts)
			if err != nil {
				t.Fatalf("JSON() error: %v", err)
			}

			var output DiagnosticsOutput
			if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
				t.Fatalf("Invalid JSON output: %v", err)
			}

			if output.Diagnostics[0].Location.File != tt.expected {
				t.Errorf("Expected file=%s, got %s", tt.expected, output.Diagnostics[0].Location.File)
			}
		})
	}
}

func TestJSONFixPreview(t *testing.T) {
	fs := source.NewFileSet()
	content := []byte("let a = 42 // missing semicolon")
	fileID := fs.AddVirtual("example.sg", content)

	bag := diag.NewBag(2)
	insertSpan := source.Span{File: fileID, Start: 10, End: 10}
	d := diag.New(diag.SevWarning, diag.LexUnknownChar, insertSpan, "missing semicolon")
	d = d.WithFix("insert semicolon", diag.FixEdit{
		Span:    insertSpan,
		NewText: ";",
	})
	bag.Add(d)

	var buf bytes.Buffer
	opts := JSONOpts{
		IncludePositions: true,
		PathMode:         PathModeBasename,
		IncludeFixes:     true,
		IncludePreviews:  true,
	}

	if err := JSON(&buf, bag, fs, opts); err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	var output DiagnosticsOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	if len(output.Diagnostics) != 1 {
		t.Fatalf("Expected 1 diagnostic, got %d", len(output.Diagnostics))
	}

	diagJSON := output.Diagnostics[0]
	if len(diagJSON.Fixes) != 1 {
		t.Fatalf("Expected 1 fix, got %d", len(diagJSON.Fixes))
	}

	fixJSON := diagJSON.Fixes[0]
	if len(fixJSON.Edits) != 1 {
		t.Fatalf("Expected 1 edit, got %d", len(fixJSON.Edits))
	}

	editJSON := fixJSON.Edits[0]
	if len(editJSON.BeforeLines) != 1 {
		t.Fatalf("Expected 1 before line, got %d", len(editJSON.BeforeLines))
	}
	if editJSON.BeforeLines[0] != "let a = 42 // missing semicolon" {
		t.Errorf("Unexpected before line: %q", editJSON.BeforeLines[0])
	}

	if len(editJSON.AfterLines) != 1 {
		t.Fatalf("Expected 1 after line, got %d", len(editJSON.AfterLines))
	}
	if editJSON.AfterLines[0] != "let a = 42; // missing semicolon" {
		t.Errorf("Unexpected after line: %q", editJSON.AfterLines[0])
	}
}
