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
	return []string{targetPath}, nil
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
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		if filepath.Ext(ent.Name()) != ".sg" {
			continue
		}
		files = append(files, filepath.Join(dir, ent.Name()))
	}
	sort.Strings(files)
	return files, nil
}
