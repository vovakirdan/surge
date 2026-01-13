package lsp

import (
	"os"
	"path/filepath"

	"surge/internal/project"
)

func detectProjectRoot(workspaceRoot, firstFile string) string {
	if root := resolveStartDir(workspaceRoot); root != "" {
		if found, ok, err := project.FindProjectRoot(root); err == nil && ok {
			return found
		}
	}
	if root := resolveStartDir(firstFile); root != "" {
		if found, ok, err := project.FindProjectRoot(root); err == nil && ok {
			return found
		}
		return root
	}
	return ""
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
