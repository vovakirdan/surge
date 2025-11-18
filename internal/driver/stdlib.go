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
	if env := os.Getenv("SURGE_STDLIB"); env != "" {
		if hasStdModule(env, stdModuleCoreIntrinsics) {
			return env
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if hasStdModule(dir, stdModuleCoreIntrinsics) {
			return dir
		}
	}
	if baseDir != "" && hasStdModule(baseDir, stdModuleCoreIntrinsics) {
		return baseDir
	}
	dir := baseDir
	for dir != "" {
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
		if hasStdModule(dir, stdModuleCoreIntrinsics) {
			return dir
		}
	}
	return ""
}

func hasStdModule(root, module string) bool {
	if root == "" {
		return false
	}
	candidate := filepath.Join(root, filepath.FromSlash(module)+".sg")
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
