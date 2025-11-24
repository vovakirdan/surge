package driver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	stdModuleCoreIntrinsics = "core/intrinsics"
	stdModuleCoreBase       = "core/base"
	stdModuleCoreOption     = "core/option"
	stdModuleCoreResult     = "core/result"
)

func detectStdlibRoot(baseDir string) string {
	if root := resolveStdlibRoot(os.Getenv("SURGE_STDLIB")); root != "" {
		return root
	}
	if exe, err := os.Executable(); err == nil {
		if root := resolveStdlibRoot(filepath.Dir(exe)); root != "" {
			return root
		}
	}
	if root := resolveStdlibRoot(baseDir); root != "" {
		return root
	}
	dir := baseDir
	for dir != "" {
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
		if root := resolveStdlibRoot(dir); root != "" {
			return root
		}
	}
	return ""
}

func resolveStdlibRoot(candidate string) string {
	if candidate == "" {
		return ""
	}
	if hasStdModule(candidate) {
		return candidate
	}
	alt := filepath.Join(candidate, "stdlib")
	if hasStdModule(alt) {
		return alt
	}
	return ""
}

func hasStdModule(root string) bool {
	if root == "" {
		return false
	}
	candidate := filepath.Join(root, filepath.FromSlash(stdModuleCoreIntrinsics)+".sg")
	info, err := os.Stat(candidate)
	return err == nil && !info.IsDir()
}

func stdModuleFilePath(root, module string) (string, bool) {
	if root == "" {
		return "", false
	}
	candidate := filepath.Join(root, filepath.FromSlash(module)+".sg")
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return "", false
	}
	return candidate, true
}

func pathWithin(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

var errStdModuleMissing = errors.New("std module missing")
