package driver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"surge/internal/project"
	"surge/internal/source"
)

// resolveModuleDir resolves a module path to a directory on the filesystem.
// It tries stdlib root first for stdlib/core modules, then falls back to baseDir.
func resolveModuleDir(modulePath, baseDir, stdlibRoot string) (string, error) {
	if stdlibRoot != "" && isStdlibModulePath(modulePath) {
		dir, err := resolveModuleDirFromBase(modulePath, stdlibRoot)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, errModuleNotFound) {
			return "", err
		}
	}
	return resolveModuleDirFromBase(modulePath, baseDir)
}

// resolveModuleDirFromBase resolves a module path relative to baseDir.
// It tries multiple strategies:
// 1. Check if modulePath is a file path and return its directory
// 2. Check if modulePath is a directory
// 3. Search for explicit module declarations in the codebase
func resolveModuleDirFromBase(modulePath, baseDir string) (string, error) {
	filePath := modulePathToFilePath(baseDir, modulePath)
	if st, err := os.Stat(filePath); err == nil && !st.IsDir() {
		return filepath.Dir(filePath), nil
	}
	dirCandidate := filepath.FromSlash(modulePath)
	if baseDir != "" {
		dirCandidate = filepath.Join(baseDir, dirCandidate)
	}
	if st, err := os.Stat(dirCandidate); err == nil && st.IsDir() {
		return dirCandidate, nil
	}
	if name := filepath.Base(modulePath); name != "" {
		if dir := findExplicitModuleDir(baseDir, modulePath, name); dir != "" {
			return dir, nil
		}
	}
	return "", errModuleNotFound
}

func isStdlibModulePath(modulePath string) bool {
	if modulePath == "" {
		return false
	}
	trimmed := strings.Trim(modulePath, "/")
	return trimmed == "core" || strings.HasPrefix(trimmed, "core/") || trimmed == "stdlib" || strings.HasPrefix(trimmed, "stdlib/")
}

var explicitModuleDirCache struct {
	mu      sync.Mutex
	byBase  map[string]map[string][]string // baseDir -> name -> dirs
	scanned map[string]bool                // baseDir -> scanned
}

// findExplicitModuleDir searches for a module directory by scanning for
// explicit module declarations (pragma module:: or pragma binary::) in .sg files.
func findExplicitModuleDir(baseDir, modulePath, name string) string {
	if baseDir == "" || name == "" {
		return ""
	}
	cacheKey := baseDir
	explicitModuleDirCache.mu.Lock()
	if explicitModuleDirCache.byBase != nil {
		if m := explicitModuleDirCache.byBase[cacheKey]; m != nil {
			if dirs := m[name]; len(dirs) > 0 {
				explicitModuleDirCache.mu.Unlock()
				return bestExplicitModuleDir(baseDir, modulePath, dirs)
			}
		}
		if explicitModuleDirCache.scanned != nil && explicitModuleDirCache.scanned[cacheKey] {
			explicitModuleDirCache.mu.Unlock()
			return ""
		}
	}
	explicitModuleDirCache.mu.Unlock()

	found := make(map[string][]string)
	walkErr := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".sg" {
			return nil
		}
		// #nosec G304 -- path comes from filesystem walk
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		dir := filepath.Dir(path)
		for _, prefix := range []string{"pragma module::", "pragma binary::"} {
			if strings.Contains(string(content), prefix+name) {
				found[name] = append(found[name], dir)
				break
			}
		}
		return nil
	})
	if walkErr != nil && len(found) == 0 {
		return ""
	}

	explicitModuleDirCache.mu.Lock()
	if explicitModuleDirCache.byBase == nil {
		explicitModuleDirCache.byBase = make(map[string]map[string][]string)
	}
	if explicitModuleDirCache.scanned == nil {
		explicitModuleDirCache.scanned = make(map[string]bool)
	}
	target := explicitModuleDirCache.byBase[cacheKey]
	if target == nil {
		target = make(map[string][]string)
		explicitModuleDirCache.byBase[cacheKey] = target
	}
	for k, v := range found {
		target[k] = append(target[k], v...)
	}
	explicitModuleDirCache.scanned[cacheKey] = true
	dirs := target[name]
	explicitModuleDirCache.mu.Unlock()
	return bestExplicitModuleDir(baseDir, modulePath, dirs)
}

// bestExplicitModuleDir selects the best matching directory from a list of candidates
// based on the longest common prefix with the target module path.
func bestExplicitModuleDir(baseDir, modulePath string, dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	if modulePath == "" {
		return dirs[0]
	}
	targetSegs := splitPathSegments(modulePath)
	bestDir := ""
	bestScore := -1
	bestDepth := 0
	for _, dir := range dirs {
		rel := dir
		if baseDir != "" {
			if relPath, err := source.RelativePath(dir, baseDir); err == nil {
				rel = relPath
			}
		}
		seg := splitPathSegments(rel)
		score := commonPrefixLen(targetSegs, seg)
		if score > bestScore || (score == bestScore && (bestDir == "" || len(seg) < bestDepth)) {
			bestScore = score
			bestDepth = len(seg)
			bestDir = dir
		}
	}
	return bestDir
}

// splitPathSegments splits a file path into segments for comparison.
func splitPathSegments(path string) []string {
	path = strings.TrimLeft(filepath.ToSlash(filepath.Clean(path)), "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// commonPrefixLen returns the length of the common prefix between two string slices.
func commonPrefixLen(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// modulePathToFilePath converts a module path to a file path.
func modulePathToFilePath(baseDir, modulePath string) string {
	rel := filepath.FromSlash(modulePath) + ".sg"
	if baseDir == "" {
		return rel
	}
	return filepath.Join(baseDir, rel)
}

// modulePathForFile extracts a module path from a source file.
func modulePathForFile(fs *source.FileSet, file *source.File) string {
	if fs == nil || file == nil {
		return ""
	}
	path := file.Path
	baseDir := fs.BaseDir()
	if baseDir != "" {
		if rel, err := source.RelativePath(path, baseDir); err == nil {
			path = rel
		}
	}
	if norm, err := project.NormalizeModulePath(path); err == nil {
		return norm
	}
	return ""
}

// normalizeExportsKey normalizes a module path for use as an exports map key.
func normalizeExportsKey(path string) string {
	if norm, err := project.NormalizeModulePath(path); err == nil {
		return norm
	}
	return strings.Trim(path, "/")
}
