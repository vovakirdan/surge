package fix

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

// TestWithRequiresAll проверяет, что опция WithRequiresAll устанавливает флаг
func TestWithRequiresAll(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1"))

	span := source.Span{File: fileID, Start: 0, End: 0}
	fix := InsertText("Test fix", span, "// ", "", WithRequiresAll())

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}
}

// TestWithRequiresAll_DeleteSpan проверяет WithRequiresAll с DeleteSpan
func TestWithRequiresAll_DeleteSpan(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1;"))

	span := source.Span{File: fileID, Start: 9, End: 10}
	fix := DeleteSpan("Remove semicolon", span, ";", WithRequiresAll())

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}

	edit := fix.Edits[0]
	if edit.NewText != "" {
		t.Errorf("expected empty NewText for deletion, got %q", edit.NewText)
	}
	if edit.OldText != ";" {
		t.Errorf("expected OldText ';', got %q", edit.OldText)
	}
}

// TestWithRequiresAll_ReplaceSpan проверяет WithRequiresAll с ReplaceSpan
func TestWithRequiresAll_ReplaceSpan(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1"))

	span := source.Span{File: fileID, Start: 0, End: 3}
	fix := ReplaceSpan("Replace let with const", span, "const", "let", WithRequiresAll())

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}

	edit := fix.Edits[0]
	if edit.NewText != "const" {
		t.Errorf("expected NewText 'const', got %q", edit.NewText)
	}
	if edit.OldText != "let" {
		t.Errorf("expected OldText 'let', got %q", edit.OldText)
	}
}

// TestMultipleOptions проверяет комбинацию нескольких опций
func TestMultipleOptions(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1"))

	span := source.Span{File: fileID, Start: 0, End: 0}
	fix := InsertText(
		"Test fix",
		span,
		"// ",
		"",
		WithRequiresAll(),
		Preferred(),
		WithID("custom-id"),
		WithKind(diag.FixKindRefactor),
		WithApplicability(diag.FixApplicabilitySafeWithHeuristics),
	)

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if !fix.IsPreferred {
		t.Error("expected IsPreferred to be true")
	}

	if fix.ID != "custom-id" {
		t.Errorf("expected ID 'custom-id', got %q", fix.ID)
	}

	if fix.Kind != diag.FixKindRefactor {
		t.Errorf("expected Kind FixKindRefactor, got %v", fix.Kind)
	}

	if fix.Applicability != diag.FixApplicabilitySafeWithHeuristics {
		t.Errorf("expected Applicability SafeWithHeuristics, got %v", fix.Applicability)
	}
}

// TestWithRequiresAll_WrapWith проверяет WithRequiresAll с WrapWith
func TestWithRequiresAll_WrapWith(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("x + y"))

	span := source.Span{File: fileID, Start: 0, End: 5}
	fix := WrapWith("Wrap in parentheses", span, "(", ")", WithRequiresAll())

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if len(fix.Edits) != 2 {
		t.Fatalf("expected 2 edits (prefix and suffix), got %d", len(fix.Edits))
	}

	// Проверяем prefix
	if fix.Edits[0].NewText != "(" {
		t.Errorf("expected prefix '(', got %q", fix.Edits[0].NewText)
	}

	// Проверяем suffix
	if fix.Edits[1].NewText != ")" {
		t.Errorf("expected suffix ')', got %q", fix.Edits[1].NewText)
	}
}

// TestWithRequiresAll_CommentLine проверяет WithRequiresAll с CommentLine
func TestWithRequiresAll_CommentLine(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1\n"))

	span := source.Span{File: fileID, Start: 0, End: 10}
	fix := CommentLine("Comment out line", span, "let x = 1\n", WithRequiresAll())

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}

	// Проверяем, что строка закомментирована
	edit := fix.Edits[0]
	if edit.NewText != "// let x = 1\n" {
		t.Errorf("expected commented line, got %q", edit.NewText)
	}
}

// TestWithRequiresAll_DeleteLine проверяет WithRequiresAll с DeleteLine
func TestWithRequiresAll_DeleteLine(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1\nlet y = 2\n"))

	span := source.Span{File: fileID, Start: 0, End: 10}
	fix := DeleteLine("Delete line", span, "let x = 1\n", WithRequiresAll())

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}

	edit := fix.Edits[0]
	if edit.NewText != "" {
		t.Errorf("expected empty NewText for deletion, got %q", edit.NewText)
	}
}

// TestWithRequiresAll_DeleteSpans проверяет WithRequiresAll с DeleteSpans
func TestWithRequiresAll_DeleteSpans(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("a, b, c"))

	spans := []source.Span{
		{File: fileID, Start: 1, End: 3}, // ", "
		{File: fileID, Start: 4, End: 6}, // ", "
	}

	fix := DeleteSpans("Remove commas", spans, WithRequiresAll())

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if len(fix.Edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(fix.Edits))
	}
}

// TestWithRequiresAll_ReplaceSpans проверяет WithRequiresAll с ReplaceSpans
func TestWithRequiresAll_ReplaceSpans(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1; let y = 2;"))

	spans := []source.Span{
		{File: fileID, Start: 0, End: 3},   // "let"
		{File: fileID, Start: 11, End: 14}, // "let"
	}

	newTexts := []string{"const", "const"}
	expects := []string{"let", "let"}

	fix := ReplaceSpans("Replace let with const", spans, newTexts, expects, WithRequiresAll())

	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if len(fix.Edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(fix.Edits))
	}

	for i, edit := range fix.Edits {
		if edit.NewText != "const" {
			t.Errorf("edit %d: expected NewText 'const', got %q", i, edit.NewText)
		}
		if edit.OldText != "let" {
			t.Errorf("edit %d: expected OldText 'let', got %q", i, edit.OldText)
		}
	}
}

// TestWithThunk проверяет опцию WithThunk
func TestWithThunk(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1"))

	thunk := &mockThunk{
		id:          "test-thunk",
		requiresAll: false,
	}

	span := source.Span{File: fileID, Start: 0, End: 0}
	fix := InsertText("Test fix", span, "// ", "", WithThunk(thunk))

	if fix.Thunk == nil {
		t.Error("expected Thunk to be set")
	}

	if fix.Thunk.ID() != "test-thunk" {
		t.Errorf("expected thunk ID 'test-thunk', got %q", fix.Thunk.ID())
	}
}

// TestNilOption проверяет, что nil опции игнорируются
func TestNilOption(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1"))

	span := source.Span{File: fileID, Start: 0, End: 0}

	// Создаем nil опцию
	var nilOpt Option

	fix := InsertText("Test fix", span, "// ", "", nilOpt, WithRequiresAll())

	// Проверяем, что fix создан корректно несмотря на nil опцию
	if !fix.RequiresAll {
		t.Error("expected RequiresAll to be true")
	}

	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}
}

// TestDefaultApplicability проверяет значение по умолчанию для Applicability
func TestDefaultApplicability(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1"))

	span := source.Span{File: fileID, Start: 0, End: 0}
	fix := InsertText("Test fix", span, "// ", "")

	// По умолчанию должно быть AlwaysSafe
	if fix.Applicability != diag.FixApplicabilityAlwaysSafe {
		t.Errorf("expected default Applicability AlwaysSafe, got %v", fix.Applicability)
	}
}

// TestDefaultKind проверяет значение по умолчанию для Kind
func TestDefaultKind(t *testing.T) {
	fs := source.NewFileSet()
	fileID := fs.AddVirtual("test.sg", []byte("let x = 1"))

	span := source.Span{File: fileID, Start: 0, End: 0}
	fix := InsertText("Test fix", span, "// ", "")

	// По умолчанию должно быть QuickFix
	if fix.Kind != diag.FixKindQuickFix {
		t.Errorf("expected default Kind QuickFix, got %v", fix.Kind)
	}
}
