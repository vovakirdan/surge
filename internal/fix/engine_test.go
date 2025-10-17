package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
)

// createTestFile создает временный файл для тестирования
func createTestFile(t *testing.T, name string, content []byte) (string, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	cleanup := func() {
		os.Remove(path)
	}
	return path, cleanup
}

// TestApplyModeOnce_WithRequiresAll проверяет, что fix с RequiresAll=true пропускается в режиме Once
func TestApplyModeOnce_WithRequiresAll(t *testing.T) {
	path, cleanup := createTestFile(t, "test.sg", []byte("let x = 1"))
	defer cleanup()

	fs := source.NewFileSet()
	fileID := fs.Add(path, []byte("let x = 1"), 0)

	d := diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.Code(0x0001),
		Message:  "test error",
		Primary:  source.Span{File: fileID, Start: 0, End: 1},
		Fixes: []diag.Fix{
			{
				ID:            "fix-1",
				Title:         "Fix with RequiresAll",
				Applicability: diag.FixApplicabilityAlwaysSafe,
				RequiresAll:   true,
				Edits: []diag.TextEdit{
					{Span: source.Span{File: fileID, Start: 0, End: 0}, NewText: "// "},
				},
			},
		},
	}

	result, err := Apply(fs, []diag.Diagnostic{d}, ApplyOptions{Mode: ApplyModeOnce})

	// Ожидаем ErrNoFixes
	if err != ErrNoFixes {
		t.Errorf("expected ErrNoFixes, got %v", err)
	}

	// Проверяем, что fix был пропущен
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied fixes, got %d", len(result.Applied))
	}

	if len(result.Skipped) == 0 {
		t.Fatal("expected at least 1 skipped fix")
	}

	// Проверяем причину пропуска
	skip := result.Skipped[0]
	if !strings.Contains(skip.Reason, "requires all fixes") {
		t.Errorf("expected skip reason to mention 'requires all fixes', got %q", skip.Reason)
	}
}

// TestApplyModeID_WithRequiresAll проверяет, что fix с RequiresAll=true пропускается в режиме ID
func TestApplyModeID_WithRequiresAll(t *testing.T) {
	path, cleanup := createTestFile(t, "test.sg", []byte("let x = 1"))
	defer cleanup()

	fs := source.NewFileSet()
	fileID := fs.Add(path, []byte("let x = 1"), 0)

	d := diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.Code(0x0001),
		Message:  "test error",
		Primary:  source.Span{File: fileID, Start: 0, End: 1},
		Fixes: []diag.Fix{
			{
				ID:            "fix-1",
				Title:         "Fix with RequiresAll",
				Applicability: diag.FixApplicabilityAlwaysSafe,
				RequiresAll:   true,
				Edits: []diag.TextEdit{
					{Span: source.Span{File: fileID, Start: 0, End: 0}, NewText: "// "},
				},
			},
		},
	}

	result, err := Apply(fs, []diag.Diagnostic{d}, ApplyOptions{
		Mode:     ApplyModeID,
		TargetID: "fix-1",
	})

	// Ожидаем ErrNoFixes
	if err != ErrNoFixes {
		t.Errorf("expected ErrNoFixes, got %v", err)
	}

	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied fixes, got %d", len(result.Applied))
	}

	if len(result.Skipped) == 0 {
		t.Fatal("expected at least 1 skipped fix")
	}

	skip := result.Skipped[0]
	if skip.ID != "fix-1" {
		t.Errorf("expected skipped fix ID 'fix-1', got %q", skip.ID)
	}
	if !strings.Contains(skip.Reason, "requires all fixes") {
		t.Errorf("expected skip reason to mention 'requires all fixes', got %q", skip.Reason)
	}
}

// TestApplyModeAll_WithRequiresAll_Safe проверяет, что безопасный fix с RequiresAll применяется в режиме All
func TestApplyModeAll_WithRequiresAll_Safe(t *testing.T) {
	path, cleanup := createTestFile(t, "test.sg", []byte("let x = 1"))
	defer cleanup()

	fs := source.NewFileSet()
	fileID := fs.Add(path, []byte("let x = 1"), 0)

	d := diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.Code(0x0001),
		Message:  "test error",
		Primary:  source.Span{File: fileID, Start: 0, End: 1},
		Fixes: []diag.Fix{
			{
				ID:            "fix-1",
				Title:         "Fix with RequiresAll",
				Applicability: diag.FixApplicabilityAlwaysSafe,
				RequiresAll:   true,
				Edits: []diag.TextEdit{
					{Span: source.Span{File: fileID, Start: 0, End: 0}, NewText: "// "},
				},
			},
		},
	}

	result, err := Apply(fs, []diag.Diagnostic{d}, ApplyOptions{Mode: ApplyModeAll})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Проверяем, что fix был применен
	if len(result.Applied) != 1 {
		t.Fatalf("expected 1 applied fix, got %d", len(result.Applied))
	}

	applied := result.Applied[0]
	if applied.ID != "fix-1" {
		t.Errorf("expected applied fix ID 'fix-1', got %q", applied.ID)
	}

	// Проверяем, что файл был изменен
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read modified file: %v", err)
	}
	expected := "// let x = 1"
	if string(content) != expected {
		t.Errorf("expected file content %q, got %q", expected, string(content))
	}
}

// TestApplyModeAll_WithRequiresAll_Unsafe проверяет, что небезопасный fix с RequiresAll пропускается в режиме All
func TestApplyModeAll_WithRequiresAll_Unsafe(t *testing.T) {
	path, cleanup := createTestFile(t, "test.sg", []byte("let x = 1"))
	defer cleanup()

	fs := source.NewFileSet()
	fileID := fs.Add(path, []byte("let x = 1"), 0)

	d := diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.Code(0x0001),
		Message:  "test error",
		Primary:  source.Span{File: fileID, Start: 0, End: 1},
		Fixes: []diag.Fix{
			{
				ID:            "fix-1",
				Title:         "Fix with RequiresAll",
				Applicability: diag.FixApplicabilitySafeWithHeuristics,
				RequiresAll:   true,
				Edits: []diag.TextEdit{
					{Span: source.Span{File: fileID, Start: 0, End: 0}, NewText: "// "},
				},
			},
		},
	}

	result, err := Apply(fs, []diag.Diagnostic{d}, ApplyOptions{Mode: ApplyModeAll})

	// Ожидаем ErrNoFixes из-за applicability, а не RequiresAll
	if err != ErrNoFixes {
		t.Errorf("expected ErrNoFixes, got %v", err)
	}

	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied fixes, got %d", len(result.Applied))
	}

	if len(result.Skipped) == 0 {
		t.Fatal("expected at least 1 skipped fix")
	}

	skip := result.Skipped[0]
	// Причина пропуска должна быть связана с applicability
	if !strings.Contains(skip.Reason, "applicability") {
		t.Errorf("expected skip reason to mention 'applicability', got %q", skip.Reason)
	}
}

// TestApplyModeOnce_MixedFixes проверяет выбор fix без RequiresAll в режиме Once
func TestApplyModeOnce_MixedFixes(t *testing.T) {
	path, cleanup := createTestFile(t, "test.sg", []byte("let x = 1"))
	defer cleanup()

	fs := source.NewFileSet()
	fileID := fs.Add(path, []byte("let x = 1"), 0)

	diagnostics := []diag.Diagnostic{
		{
			Severity: diag.SevError,
			Code:     diag.Code(0x0001),
			Message:  "test error 1",
			Primary:  source.Span{File: fileID, Start: 0, End: 1},
			Fixes: []diag.Fix{
				{
					ID:            "fix-requires-all",
					Title:         "Fix with RequiresAll",
					Applicability: diag.FixApplicabilityAlwaysSafe,
					RequiresAll:   true,
					Edits: []diag.TextEdit{
						{Span: source.Span{File: fileID, Start: 0, End: 0}, NewText: "/* a */ "},
					},
				},
			},
		},
		{
			Severity: diag.SevError,
			Code:     diag.Code(0x0002),
			Message:  "test error 2",
			Primary:  source.Span{File: fileID, Start: 5, End: 6},
			Fixes: []diag.Fix{
				{
					ID:            "fix-normal",
					Title:         "Normal Fix",
					Applicability: diag.FixApplicabilityAlwaysSafe,
					RequiresAll:   false,
					Edits: []diag.TextEdit{
						{Span: source.Span{File: fileID, Start: 9, End: 9}, NewText: ";"},
					},
				},
			},
		},
	}

	result, err := Apply(fs, diagnostics, ApplyOptions{Mode: ApplyModeOnce})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Должен быть применен только обычный fix
	if len(result.Applied) != 1 {
		t.Fatalf("expected 1 applied fix, got %d", len(result.Applied))
	}

	applied := result.Applied[0]
	if applied.ID != "fix-normal" {
		t.Errorf("expected applied fix ID 'fix-normal', got %q", applied.ID)
	}

	// Файл должен содержать изменение от обычного fix
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read modified file: %v", err)
	}
	expected := "let x = 1;"
	if string(content) != expected {
		t.Errorf("expected file content %q, got %q", expected, string(content))
	}
}

// TestApplyModeAll_MixedFixes проверяет применение обоих safe fixes в режиме All
func TestApplyModeAll_MixedFixes(t *testing.T) {
	path, cleanup := createTestFile(t, "test.sg", []byte("let x = 1"))
	defer cleanup()

	fs := source.NewFileSet()
	fileID := fs.Add(path, []byte("let x = 1"), 0)

	diagnostics := []diag.Diagnostic{
		{
			Severity: diag.SevError,
			Code:     diag.Code(0x0001),
			Message:  "test error 1",
			Primary:  source.Span{File: fileID, Start: 0, End: 1},
			Fixes: []diag.Fix{
				{
					ID:            "fix-requires-all",
					Title:         "Fix with RequiresAll",
					Applicability: diag.FixApplicabilityAlwaysSafe,
					RequiresAll:   true,
					Edits: []diag.TextEdit{
						{Span: source.Span{File: fileID, Start: 0, End: 0}, NewText: "// "},
					},
				},
			},
		},
		{
			Severity: diag.SevError,
			Code:     diag.Code(0x0002),
			Message:  "test error 2",
			Primary:  source.Span{File: fileID, Start: 5, End: 6},
			Fixes: []diag.Fix{
				{
					ID:            "fix-normal",
					Title:         "Normal Fix",
					Applicability: diag.FixApplicabilityAlwaysSafe,
					RequiresAll:   false,
					Edits: []diag.TextEdit{
						{Span: source.Span{File: fileID, Start: 9, End: 9}, NewText: ";"},
					},
				},
			},
		},
	}

	result, err := Apply(fs, diagnostics, ApplyOptions{Mode: ApplyModeAll})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Оба fix должны быть применены
	if len(result.Applied) != 2 {
		t.Fatalf("expected 2 applied fixes, got %d", len(result.Applied))
	}

	// Проверяем ID примененных fixes
	appliedIDs := make(map[string]bool)
	for _, applied := range result.Applied {
		appliedIDs[applied.ID] = true
	}

	if !appliedIDs["fix-requires-all"] {
		t.Error("expected 'fix-requires-all' to be applied")
	}
	if !appliedIDs["fix-normal"] {
		t.Error("expected 'fix-normal' to be applied")
	}

	// Файл должен содержать оба изменения
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read modified file: %v", err)
	}
	expected := "// let x = 1;"
	if string(content) != expected {
		t.Errorf("expected file content %q, got %q", expected, string(content))
	}
}

// mockThunk реализует FixThunk для тестирования
type mockThunk struct {
	id          string
	requiresAll bool
	fileID      source.FileID
}

func (m *mockThunk) ID() string {
	return m.id
}

func (m *mockThunk) Build(ctx diag.FixBuildContext) (diag.Fix, error) {
	// Создаем простой fix с одной вставкой
	return diag.Fix{
		Title:         "Thunk-generated fix",
		Applicability: diag.FixApplicabilityAlwaysSafe,
		RequiresAll:   m.requiresAll,
		Edits: []diag.TextEdit{
			{
				Span:    source.Span{File: m.fileID, Start: 0, End: 0},
				NewText: "/* thunk */ ",
			},
		},
	}, nil
}

// TestThunk_WithRequiresAll проверяет, что RequiresAll сохраняется после материализации thunk
func TestThunk_WithRequiresAll(t *testing.T) {
	path, cleanup := createTestFile(t, "test.sg", []byte("let x = 1"))
	defer cleanup()

	fs := source.NewFileSet()
	fileID := fs.Add(path, []byte("let x = 1"), 0)

	thunk := &mockThunk{
		id:          "thunk-fix",
		requiresAll: false, // Внутри thunk RequiresAll=false
		fileID:      fileID,
	}

	d := diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.Code(0x0001),
		Message:  "test error",
		Primary:  source.Span{File: fileID, Start: 0, End: 1},
		Fixes: []diag.Fix{
			{
				ID:            "thunk-fix",
				Title:         "Thunk Fix",
				Applicability: diag.FixApplicabilityAlwaysSafe,
				RequiresAll:   true, // Но на уровне Fix RequiresAll=true
				Thunk:         thunk,
			},
		},
	}

	// Проверяем, что в режиме Once fix пропускается (т.к. RequiresAll=true на уровне Fix)
	result, err := Apply(fs, []diag.Diagnostic{d}, ApplyOptions{Mode: ApplyModeOnce})

	if err != ErrNoFixes {
		t.Errorf("expected ErrNoFixes, got %v", err)
	}

	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied fixes, got %d", len(result.Applied))
	}

	if len(result.Skipped) == 0 {
		t.Fatal("expected at least 1 skipped fix")
	}

	// Проверяем, что в режиме All fix применяется
	path2, cleanup2 := createTestFile(t, "test2.sg", []byte("let x = 1"))
	defer cleanup2()

	fs2 := source.NewFileSet()
	fileID2 := fs2.Add(path2, []byte("let x = 1"), 0)

	thunk2 := &mockThunk{
		id:          "thunk-fix",
		requiresAll: false,
		fileID:      fileID2,
	}

	d2 := diag.Diagnostic{
		Severity: diag.SevError,
		Code:     diag.Code(0x0001),
		Message:  "test error",
		Primary:  source.Span{File: fileID2, Start: 0, End: 1},
		Fixes: []diag.Fix{
			{
				ID:            "thunk-fix",
				Title:         "Thunk Fix",
				Applicability: diag.FixApplicabilityAlwaysSafe,
				RequiresAll:   true,
				Thunk:         thunk2,
			},
		},
	}

	result2, err2 := Apply(fs2, []diag.Diagnostic{d2}, ApplyOptions{Mode: ApplyModeAll})

	if err2 != nil {
		t.Fatalf("unexpected error in ApplyModeAll: %v", err2)
	}

	if len(result2.Applied) != 1 {
		t.Fatalf("expected 1 applied fix in ApplyModeAll, got %d", len(result2.Applied))
	}

	// Проверяем, что thunk был материализован и применен
	content2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatalf("failed to read modified file: %v", err)
	}
	expected := "/* thunk */ let x = 1"
	if string(content2) != expected {
		t.Errorf("expected file content %q, got %q", expected, string(content2))
	}
}

// TestThunk_MaterializationPreservesRequiresAll проверяет прямую материализацию
func TestThunk_MaterializationPreservesRequiresAll(t *testing.T) {
	thunk := &mockThunk{
		id:          "test-thunk",
		requiresAll: false,
		fileID:      source.FileID(0), // Для этого теста fileID не важен
	}

	parentFix := diag.Fix{
		ID:            "parent-fix",
		Title:         "Parent Fix",
		Applicability: diag.FixApplicabilityAlwaysSafe,
		RequiresAll:   true,
		Thunk:         thunk,
	}

	ctx := diag.FixBuildContext{FileSet: source.NewFileSet()}
	resolved, err := parentFix.Resolve(ctx)

	if err != nil {
		t.Fatalf("unexpected error during Resolve: %v", err)
	}

	// Проверяем, что RequiresAll был унаследован
	if !resolved.RequiresAll {
		t.Error("expected RequiresAll to be true after materialization")
	}

	// Проверяем, что другие поля также унаследованы
	if resolved.ID != "parent-fix" {
		t.Errorf("expected ID 'parent-fix', got %q", resolved.ID)
	}
}
