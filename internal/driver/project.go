package driver

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
)

func buildModuleMeta(
	fs *source.FileSet,
	builder *ast.Builder,
	fileID ast.FileID,
	baseDir string,
	reporter diag.Reporter,
) (project.ModuleMeta, bool) {
	fileNode := builder.Files.Get(fileID)
	if fileNode == nil {
		return project.ModuleMeta{}, false
	}

	fileSpan := fileNode.Span
	srcFile := fs.Get(fileSpan.File)

	fullModulePath, err := project.NormalizeModulePath(srcFile.Path)
	if err != nil {
		if reporter != nil {
			reporter.Report(
				diag.ProjInvalidModulePath,
				diag.SevError,
				fileSpan,
				fmt.Sprintf("invalid module path %q: %v", srcFile.Path, err),
				nil,
				nil,
			)
		}
		return project.ModuleMeta{}, false
	}

	meta := project.ModuleMeta{
		Path: fullModulePath,
		Span: fileSpan,
	}

	if len(fileNode.Items) == 0 {
		return meta, true
	}

	interner := builder.StringsInterner
	imports := make([]project.ImportMeta, 0, len(fileNode.Items))

	for _, itemID := range fileNode.Items {
		item := builder.Items.Get(itemID)
		if item == nil || item.Kind != ast.ItemImport {
			continue
		}
		importItem, ok := builder.Items.Import(itemID)
		if !ok {
			continue
		}
		if len(importItem.Module) == 0 {
			continue
		}
		segments := make([]string, 0, len(importItem.Module))
		valid := true
		for _, segID := range importItem.Module {
			if segID == source.NoStringID {
				valid = false
				break
			}
			seg, ok := interner.Lookup(segID)
			if !ok {
				valid = false
				break
			}
			segments = append(segments, seg)
		}
		if !valid || len(segments) == 0 {
			continue
		}

		rawPath := strings.Join(segments, "/")
		normImport, err := project.ResolveImportPath(fullModulePath, baseDir, segments)
		if err != nil {
			if reporter != nil {
				reporter.Report(
					diag.ProjInvalidImportPath,
					diag.SevError,
					item.Span,
					fmt.Sprintf("invalid import path %q: %v", rawPath, err),
					nil,
					nil,
				)
			}
			continue
		}

		imports = append(imports, project.ImportMeta{
			Path: normImport,
			Span: item.Span,
		})
	}

	if len(imports) > 0 {
		meta.Imports = imports
	}

	return meta, true
}
