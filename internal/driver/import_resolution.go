package driver

import (
	"path"

	"surge/internal/project"
)

func shouldReportWrongExplicitImport(imp project.ImportMeta, meta *project.ModuleMeta, importedPath, actualPath string) bool {
	if meta == nil || !meta.HasExplicitName || importedPath == "" || actualPath == "" || importedPath == actualPath {
		return false
	}
	if imp.IsRelative {
		return true
	}
	if path.Base(importedPath) != meta.Name {
		return true
	}
	return imp.SegmentCount > 1
}
