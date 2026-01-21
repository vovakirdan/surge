package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/driver/diagnose"
	"surge/internal/parser"
)

func TestPreludeOptionAvailable(t *testing.T) {
	dir := tempProjectDir(t)
	src := strings.Join([]string{
		"@entrypoint",
		"fn main() -> int {",
		"    let foo = Some(1);",
		"    let bar: Option<int> = nothing;",
		"    return 0;",
		"}",
		"",
	}, "\n")
	path := filepath.Join(dir, "main.sg")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("write main.sg: %v", err)
	}

	opts := diagnose.DiagnoseOptions{
		ProjectRoot:    dir,
		BaseDir:        dir,
		Stage:          driver.DiagnoseStageAll,
		MaxDiagnostics: 20,
		DirectiveMode:  parser.DirectiveModeOff,
	}
	snapshot, diags, err := diagnose.AnalyzeWorkspace(context.Background(), &opts, diagnose.FileOverlay{})
	if err != nil {
		t.Fatalf("analyze workspace: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %s", diagSummary(diags))
	}

	fullRange := lspRange{
		Start: position{Line: 0, Character: 0},
		End:   position{Line: 200, Character: 0},
	}
	hints := buildInlayHints(snapshot, pathToURI(path), fullRange, defaultInlayHintConfig())
	if len(hints) == 0 {
		t.Fatal("expected inlay hints for prelude types")
	}
}

func TestNoStdDisablesPrelude(t *testing.T) {
	dir := tempProjectDir(t)
	src := strings.Join([]string{
		"pragma no_std",
		"@entrypoint",
		"fn main() -> int {",
		"    let foo: Option<int> = nothing;",
		"    return 0;",
		"}",
		"",
	}, "\n")
	path := filepath.Join(dir, "main.sg")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("write main.sg: %v", err)
	}

	opts := diagnose.DiagnoseOptions{
		ProjectRoot:    dir,
		BaseDir:        dir,
		Stage:          driver.DiagnoseStageAll,
		MaxDiagnostics: 20,
		DirectiveMode:  parser.DirectiveModeOff,
	}
	_, diags, err := diagnose.AnalyzeWorkspace(context.Background(), &opts, diagnose.FileOverlay{})
	if err != nil {
		t.Fatalf("analyze workspace: %v", err)
	}
	if !hasDiagCode(diags, path, "SEM3005") {
		t.Fatalf("expected SEM3005 for no_std, got %s", diagSummary(diags))
	}
}
