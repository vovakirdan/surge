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

func analyzeWorkspaceSnapshot(t *testing.T, files map[string]string, overlay map[string]string) (*diagnose.AnalysisSnapshot, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	paths := make(map[string]string, len(files))
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		paths[rel] = abs
	}
	overlayFiles := make(map[string]string, len(overlay))
	for rel, content := range overlay {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		overlayFiles[abs] = content
	}
	opts := diagnose.DiagnoseOptions{
		ProjectRoot:    dir,
		BaseDir:        dir,
		Stage:          driver.DiagnoseStageAll,
		MaxDiagnostics: 20,
		DirectiveMode:  parser.DirectiveModeOff,
	}
	snapshot, _, err := diagnose.AnalyzeWorkspace(context.Background(), &opts, diagnose.FileOverlay{Files: overlayFiles})
	if err != nil {
		t.Fatalf("analyze workspace: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	return snapshot, paths
}

func stdlibRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 8 {
		if _, statErr := os.Stat(filepath.Join(dir, "core", "intrinsics.sg")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("stdlib root not found from %s", dir)
	return ""
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
