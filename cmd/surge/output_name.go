package main

import (
	"path/filepath"
	"strings"
)

func outputNameFromPath(inputPath string, dirInfo *runDirInfo) string {
	if dirInfo != nil {
		return filepath.Base(dirInfo.path)
	}
	base := filepath.Base(inputPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
