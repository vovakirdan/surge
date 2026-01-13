package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"surge/internal/driver"
	"surge/internal/driver/diagnose"
	"surge/internal/parser"
)

func analyzeSnapshot(t *testing.T, content string) (*diagnose.AnalysisSnapshot, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	opts := diagnose.DiagnoseOptions{
		ProjectRoot:    path,
		BaseDir:        dir,
		Stage:          driver.DiagnoseStageAll,
		MaxDiagnostics: 20,
		DirectiveMode:  parser.DirectiveModeOff,
	}
	snapshot, _, err := diagnose.AnalyzeWorkspace(context.Background(), &opts, diagnose.FileOverlay{})
	if err != nil {
		t.Fatalf("analyze workspace: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	return snapshot, pathToURI(path)
}

func analyzeSnapshotWithOverlay(t *testing.T, diskContent, overlayContent string) (*diagnose.AnalysisSnapshot, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.sg")
	if err := os.WriteFile(path, []byte(diskContent), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	opts := diagnose.DiagnoseOptions{
		ProjectRoot:    path,
		BaseDir:        dir,
		Stage:          driver.DiagnoseStageAll,
		MaxDiagnostics: 20,
		DirectiveMode:  parser.DirectiveModeOff,
	}
	overlay := diagnose.FileOverlay{
		Files: map[string]string{
			path: overlayContent,
		},
	}
	snapshot, _, err := diagnose.AnalyzeWorkspace(context.Background(), &opts, overlay)
	if err != nil {
		t.Fatalf("analyze workspace: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	return snapshot, pathToURI(path)
}

func positionForOffsetUTF16(text string, offset int) position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	line := strings.Count(text[:offset], "\n")
	lineStart := strings.LastIndex(text[:offset], "\n")
	if lineStart == -1 {
		lineStart = 0
	} else {
		lineStart++
	}
	units := 0
	for _, r := range text[lineStart:offset] {
		n := utf16.RuneLen(r)
		if n < 0 {
			n = 1
		}
		units += n
	}
	return position{Line: line, Character: units}
}
