package driver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	stdModuleCore = "core"
)

func detectStdlibRoot() string {
	// Prefer a local checkout in the current working directory to keep
	// diagnostics consistent with the sources we edit.
	if root := resolveStdlibRoot("."); root != "" {
		return root
	}

	if root := resolveStdlibRoot(os.Getenv("SURGE_STDLIB")); root != "" {
		return root
	}
	if exe, err := os.Executable(); err == nil {
		if root := resolveStdlibRoot(filepath.Dir(exe)); root != "" {
			return root
		}
	}
	if root := resolveStdlibRoot("/usr/local/share/surge"); root != "" {
		return root
	}
	if root := resolveStdlibRoot("/usr/share/surge"); root != "" {
		return root
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
	candidate := filepath.Join(root, "core", "intrinsics.sg")
	info, err := os.Stat(candidate)
	return err == nil && !info.IsDir()
}

func stdModuleFilePath(root, module string) (string, bool) {
	if root == "" {
		return "", false
	}
	if module == stdModuleCore {
		candidate := filepath.Join(root, "core", "intrinsics.sg")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		// fallback: pick any .sg in core dir
		dir := filepath.Join(root, "core")
		if entries, err := os.ReadDir(dir); err == nil {
			for _, ent := range entries {
				if ent.IsDir() || filepath.Ext(ent.Name()) != ".sg" {
					continue
				}
				candidate = filepath.Join(dir, ent.Name())
				return candidate, true
			}
		}
		return "", false
	}

	// Handle nested stdlib paths like "stdlib/directives/test"
	moduleParts := strings.Split(module, "/")
	if len(moduleParts) >= 2 && moduleParts[0] == "stdlib" {
		dirPath := filepath.Join(root, filepath.FromSlash(module))
		baseName := moduleParts[len(moduleParts)-1]

		// Try <baseName>.sg (e.g., stdlib/directives/test/test.sg)
		candidate := filepath.Join(dirPath, baseName+".sg")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}

		// Try _<baseName>.sg (underscore prefix convention)
		candidate = filepath.Join(dirPath, "_"+baseName+".sg")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
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
