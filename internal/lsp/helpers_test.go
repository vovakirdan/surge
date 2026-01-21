package lsp

import (
	"context"
	"fmt"
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

func tempProjectDir(t *testing.T) string {
	t.Helper()
	root := stdlibRoot(t)
	dir, err := os.MkdirTemp(root, "lsp-prelude-")
	if err != nil {
		t.Fatalf("mkdir temp project: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func hasDiagCode(diags []diagnose.Diagnostic, path, code string) bool {
	if len(diags) == 0 || path == "" || code == "" {
		return false
	}
	path = canonicalPath(path)
	for _, d := range diags {
		if canonicalPath(d.FilePath) != path {
			continue
		}
		if d.Code == code {
			return true
		}
	}
	return false
}

func diagSummary(diags []diagnose.Diagnostic) string {
	if len(diags) == 0 {
		return "<none>"
	}
	var b strings.Builder
	for i, d := range diags {
		if i > 0 {
			b.WriteString("; ")
		}
		path := d.FilePath
		if path == "" {
			path = "<unknown>"
		}
		code := d.Code
		if code == "" {
			code = "<no-code>"
		}
		fmt.Fprintf(&b, "%s:%s:%s", path, code, d.Message)
	}
	return b.String()
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
