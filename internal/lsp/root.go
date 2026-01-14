package lsp

import (
	"os"
	"path/filepath"

	"surge/internal/project"
)

type analysisMode uint8

const (
	modeProjectRoot analysisMode = iota
	modeOpenFiles
)

func detectAnalysisScope(workspaceRoot, firstFile string) (string, analysisMode) {
	if root := resolveStartDir(workspaceRoot); root != "" {
		if found, ok, err := project.FindProjectRoot(root); err == nil && ok {
			return found, modeProjectRoot
		}
	}
	if root := resolveStartDir(firstFile); root != "" {
		if found, ok, err := project.FindProjectRoot(root); err == nil && ok {
			return found, modeProjectRoot
		}
		return root, modeOpenFiles
	}
	return "", modeOpenFiles
}

func resolveStartDir(path string) string {
	if path == "" {
		return ""
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}
