package buildpipeline

import (
	"fmt"
	"strings"

	"surge/internal/driver"
)

// ValidateEntrypoints ensures there is exactly one @entrypoint in the root module.
func ValidateEntrypoints(result *driver.DiagnoseResult) error {
	if result == nil {
		return fmt.Errorf("missing compilation result")
	}
	entries := result.Entrypoints()
	rootMeta := result.RootModuleMeta()
	rootPath := ""
	if rootMeta != nil {
		rootPath = rootMeta.Path
	}
	if rootPath == "" {
		if len(entries) == 1 {
			return nil
		}
		if len(entries) == 0 {
			return fmt.Errorf("no @entrypoint found")
		}
		return fmt.Errorf("multiple @entrypoint functions found: %s", formatEntrypointList(entries))
	}
	var rootEntries []driver.EntrypointInfo
	for _, entry := range entries {
		if entry.ModulePath == rootPath {
			rootEntries = append(rootEntries, entry)
		}
	}
	if len(rootEntries) == 0 {
		return fmt.Errorf("no @entrypoint found in module %q", rootPath)
	}
	if len(rootEntries) > 1 {
		return fmt.Errorf("multiple @entrypoint functions found in module %q: %s", rootPath, formatEntrypointList(rootEntries))
	}
	return nil
}

func formatEntrypointList(entries []driver.EntrypointInfo) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		label := entry.ModulePath
		if label == "" {
			label = "<unknown module>"
		}
		if entry.Name != "" {
			label = fmt.Sprintf("%s::%s", label, entry.Name)
		}
		if entry.FilePath != "" {
			label = fmt.Sprintf("%s (%s)", label, entry.FilePath)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}
