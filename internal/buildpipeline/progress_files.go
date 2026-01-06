package buildpipeline

import (
	"path/filepath"
	"sort"
	"strings"

	"surge/internal/driver"
)

func expandProgressFiles(req *CompileRequest, phase *phaseObserver, diagRes *driver.DiagnoseResult) {
	if req == nil || req.Progress == nil || diagRes == nil || req.DirInfo == nil {
		return
	}
	moduleFiles := diagRes.ModuleFiles()
	if len(moduleFiles) == 0 {
		return
	}
	rootDir := progressRootDir(req)
	if rootDir != "" {
		moduleFiles = filterFilesUnderRoot(moduleFiles, rootDir)
	}
	displayFiles := normalizeProgressFiles(moduleFiles, req.BaseDir)
	if len(displayFiles) == 0 {
		return
	}
	existing := make(map[string]struct{}, len(req.Files))
	for _, file := range req.Files {
		if file == "" {
			continue
		}
		existing[file] = struct{}{}
	}
	newFiles := make([]string, 0, len(displayFiles))
	for _, file := range displayFiles {
		if _, ok := existing[file]; ok {
			continue
		}
		newFiles = append(newFiles, file)
	}
	req.Files = displayFiles
	if phase != nil {
		phase.files = displayFiles
	}
	if len(newFiles) == 0 {
		return
	}
	emitQueued(req.Progress, newFiles)
	if phase != nil && phase.lowerStarted {
		emitStage(req.Progress, newFiles, StageLower, StatusWorking, nil, 0)
	}
}

func progressRootDir(req *CompileRequest) string {
	if req == nil {
		return ""
	}
	if req.BaseDir != "" {
		return req.BaseDir
	}
	if req.DirInfo != nil && req.DirInfo.Path != "" {
		return req.DirInfo.Path
	}
	if req.TargetPath != "" {
		return filepath.Dir(req.TargetPath)
	}
	return ""
}

func filterFilesUnderRoot(files []string, root string) []string {
	if len(files) == 0 || strings.TrimSpace(root) == "" {
		return files
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return files
	}
	absRoot = filepath.Clean(absRoot)
	prefix := absRoot + string(filepath.Separator)
	filtered := make([]string, 0, len(files))
	for _, file := range files {
		if file == "" {
			continue
		}
		absFile, err := filepath.Abs(file)
		if err != nil {
			continue
		}
		absFile = filepath.Clean(absFile)
		if absFile == absRoot || strings.HasPrefix(absFile, prefix) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func normalizeProgressFiles(files []string, baseDir string) []string {
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
