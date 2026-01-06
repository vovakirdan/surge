package diagfmt

import (
	"bytes"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/fix"
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

type staticFixThunk struct {
	fix *diag.Fix
}

func (t staticFixThunk) ID() string {
	if t.fix.ID != "" {
		return t.fix.ID
	}
	return "static-fix"
}

func (t staticFixThunk) Build(_ diag.FixBuildContext) (diag.Fix, error) {
	return *t.fix, nil
}

func TestPrettyNotesAndFixes(t *testing.T) {
	fs := source.NewFileSet()
	content := []byte("import core::util\n")
	fileID := fs.AddVirtual("test.sg", content)

	bag := diag.NewBag(4)
	primary := source.Span{File: fileID, Start: 6, End: 10}
	d := diag.New(diag.SevWarning, diag.SynUnexpectedToken, primary, "unexpected token")

	noteSpan := source.Span{File: fileID, Start: 11, End: 15}
	d = d.WithNote(noteSpan, "remove trailing identifier")

	insertSpan := source.Span{File: fileID, Start: primary.End, End: primary.End}
	d = d.WithFix("insert semicolon", diag.FixEdit{Span: insertSpan, NewText: ";"})

	staticFix := fix.WrapWith(
		"wrap import block",
		source.Span{File: fileID, Start: 0, End: uint32(len(content))},
		"/* ",
		" */",
		fix.WithID("wrap-import-001"),
	)

	lazyFix := &diag.Fix{
		Title:         "wrap import block",
		Kind:          diag.FixKindRefactor,
		Applicability: diag.FixApplicabilitySafeWithHeuristics,
		Thunk: staticFixThunk{
			fix: staticFix,
		},
	}
	d = d.WithFixSuggestion(lazyFix)

	bag.Add(d)

	var buf bytes.Buffer
	opts := PrettyOpts{
		Color:     false,
		Context:   0,
		PathMode:  PathModeBasename,
		ShowNotes: true,
		ShowFixes: true,
	}
	Pretty(&buf, bag, fs, opts)

	output := buf.String()

	if !strings.Contains(output, "note: test.sg:1:12") {
		t.Fatalf("expected note with location, got:\n%s", output)
	}

	if !strings.Contains(output, "fix #1: insert semicolon") {
		t.Fatalf("expected first fix entry, got:\n%s", output)
	}

	if !strings.Contains(output, "apply=\";\"") {
		t.Fatalf("expected fix edit apply preview, got:\n%s", output)
	}

	if !strings.Contains(output, "id=wrap-import-001") {
		t.Fatalf("expected lazy fix id in output, got:\n%s", output)
	}
}

func TestPrettyFixPreview(t *testing.T) {
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
	opts := PrettyOpts{
		Color:       false,
		Context:     0,
		PathMode:    PathModeBasename,
		ShowFixes:   true,
		ShowPreview: true,
	}
	Pretty(&buf, bag, fs, opts)

	output := buf.String()
	if !strings.Contains(output, "preview:") {
		t.Fatalf("expected preview header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "- let a = 42 // missing semicolon") {
		t.Fatalf("expected before line in preview, got:\n%s", output)
	}
	if !strings.Contains(output, "+ let a = 42; // missing semicolon") {
		t.Fatalf("expected after line in preview, got:\n%s", output)
	}
}
