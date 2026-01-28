package driver

import (
	"path/filepath"
	"strings"

	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
)

func ensureModuleMapping(opts *DiagnoseOptions, startDir string) error {
	if opts == nil || opts.ModuleMapping != nil {
		return nil
	}
	if strings.TrimSpace(startDir) == "" {
		return nil
	}
	mapping, ok, err := project.LoadModuleMapping(startDir)
	if err != nil {
		return err
	}
	if ok {
		opts.ModuleMapping = mapping
	}
	return nil
}

func splitModulePath(modulePath string) (alias, rest string) {
	modulePath = strings.Trim(modulePath, "/")
	if modulePath == "" {
		return "", ""
	}
	parts := strings.SplitN(modulePath, "/", 2)
	alias = parts[0]
	if len(parts) > 1 {
		rest = parts[1]
	}
	return alias, rest
}

func resolveMappedModulePath(modulePath string, mapping *project.ModuleMapping) (root, rest string, ok bool) {
	if mapping == nil || len(mapping.Roots) == 0 {
		return "", "", false
	}
	alias, segRest := splitModulePath(modulePath)
	rest = segRest
	if alias == "" {
		return "", "", false
	}
	root, ok = mapping.Roots[alias]
	if !ok {
		return "", "", false
	}
	return root, rest, true
}

func logicalPathForFile(path, baseDir string, mapping *project.ModuleMapping) string {
	if mapping != nil {
		if logical, ok := mapping.LogicalPath(path); ok {
			return logical
		}
	}
	rel := path
	if baseDir != "" {
		if relPath, err := source.RelativePath(path, baseDir); err == nil {
			rel = relPath
		}
	}
	return filepath.ToSlash(rel)
}

func logicalPathForDir(path, baseDir string, mapping *project.ModuleMapping) string {
	return logicalPathForFile(path, baseDir, mapping)
}

type overrideReporter struct {
	base      diag.Reporter
	overrides map[source.Span]string
}

// Report implements diag.Reporter, overriding missing-module messages when needed.
func (r *overrideReporter) Report(code diag.Code, sev diag.Severity, span source.Span, msg string, notes []diag.Note, fixes []*diag.Fix) {
	if code == diag.ProjMissingModule {
		if override, ok := r.overrides[span]; ok {
			msg = override
		}
	}
	r.base.Report(code, sev, span, msg, notes, fixes)
}

func missingModuleOverrides(records map[string]*moduleRecord, mapping *project.ModuleMapping) map[source.Span]string {
	if mapping == nil || len(mapping.Missing) == 0 {
		return nil
	}
	overrides := make(map[source.Span]string)
	for _, rec := range records {
		if rec == nil || rec.Meta == nil {
			continue
		}
		for _, imp := range rec.Meta.Imports {
			alias, _ := splitModulePath(imp.Path)
			if alias == "" {
				continue
			}
			if msg, ok := mapping.Missing[alias]; ok {
				overrides[imp.Span] = msg
			}
		}
	}
	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

func wrapMissingModuleReporter(reporter diag.Reporter, overrides map[source.Span]string) diag.Reporter {
	if reporter == nil || len(overrides) == 0 {
		return reporter
	}
	return &overrideReporter{base: reporter, overrides: overrides}
}
