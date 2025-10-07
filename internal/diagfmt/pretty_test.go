package diagfmt

import (
	"bytes"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

// TestPathModes проверяет различные режимы форматирования путей
func TestPathModes(t *testing.T) {
	// Создаём FileSet
	fs := source.NewFileSet()

	// Добавляем тестовый файл
	content := []byte("let x = \"unterminated string\n")
	fileID := fs.AddVirtual("/home/user/project/src/test.sg", content)

	// Устанавливаем базовую директорию для relative paths
	fs.SetBaseDir("/home/user/project")

	// Создаём диагностику
	bag := diag.NewBag(10)
	d := diag.New(
		diag.SevError,
		diag.LexUnterminatedString,
		source.Span{File: fileID, Start: 8, End: 28},
		"Unterminated string literal",
	)
	bag.Add(d)

	tests := []struct {
		name     string
		mode     PathMode
		contains string
	}{
		{
			name:     "Absolute path",
			mode:     PathModeAbsolute,
			contains: "/home/user/project/src/test.sg",
		},
		{
			name:     "Relative path",
			mode:     PathModeRelative,
			contains: "src/test.sg",
		},
		{
			name:     "Basename only",
			mode:     PathModeBasename,
			contains: "test.sg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := PrettyOpts{
				Color:    false,
				Context:  1,
				PathMode: tt.mode,
			}

			Pretty(&buf, bag, fs, opts)
			output := buf.String()

			if !strings.Contains(output, tt.contains) {
				t.Errorf("Expected output to contain %q, got:\n%s", tt.contains, output)
			}

			// Проверяем что есть основные элементы
			if !strings.Contains(output, "ERROR") {
				t.Error("Expected ERROR in output")
			}
			if !strings.Contains(output, "LEX1002") {
				t.Error("Expected LEX1002 code in output")
			}
			if !strings.Contains(output, "Unterminated string") {
				t.Error("Expected error message in output")
			}
		})
	}
}

// TestPathModeAuto проверяет авто-режим выбора пути
func TestPathModeAuto(t *testing.T) {
	fs := source.NewFileSet()

	tests := []struct {
		name     string
		path     string
		expected string // что должно быть в выводе
	}{
		{
			name:     "Short path - as is",
			path:     "test.sg",
			expected: "test.sg",
		},
		{
			name:     "Long absolute path - basename",
			path:     "/very/long/absolute/path/to/some/nested/directory/file.sg",
			expected: "file.sg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte("let x = 42\n")
			fileID := fs.AddVirtual(tt.path, content)

			bag := diag.NewBag(10)
			d := diag.New(
				diag.SevWarning,
				diag.LexUnknownChar,
				source.Span{File: fileID, Start: 8, End: 10},
				"Test warning",
			)
			bag.Add(d)

			var buf bytes.Buffer
			opts := PrettyOpts{
				Color:    false,
				Context:  0,
				PathMode: PathModeAuto,
			}

			Pretty(&buf, bag, fs, opts)
			output := buf.String()

			if !strings.Contains(output, tt.expected) {
				t.Errorf("Expected output to contain %q, got:\n%s", tt.expected, output)
			}
		})
	}
}
