package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func collectProjectFiles(targetPath string, dirInfo *runDirInfo) ([]string, error) {
	if dirInfo != nil && dirInfo.path != "" {
		return listSGFiles(dirInfo.path)
	}
	if targetPath == "" {
		return nil, nil
	}
	dir := filepath.Dir(targetPath)
	files, err := listSGFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return []string{targetPath}, nil
	}
	targetPath = filepath.Clean(targetPath)
	found := false
	for _, file := range files {
		if filepath.Clean(file) == targetPath {
			found = true
			break
		}
	}
	if !found {
		files = append(files, targetPath)
		sort.Strings(files)
	}
	return files, nil
}

func displayFileList(files []string, baseDir string) []string {
	if len(files) == 0 {
		return files
	}
	normalized := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))

	base := strings.TrimSpace(baseDir)
	if base != "" {
		if abs, err := filepath.Abs(base); err == nil {
			base = abs
		}
	}

	for _, file := range files {
		if file == "" {
			continue
		}
		path := filepath.Clean(file)
		if base != "" {
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
			if rel, err := filepath.Rel(base, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				path = rel
			}
		}
		path = filepath.ToSlash(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}
	sort.Strings(normalized)
	return normalized
}

func listSGFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Skip hidden directories and common build folders
			if len(name) > 1 && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if name == "target" || name == "build" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".sg" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
