package driver

import (
	"fmt"
	"os"
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
) (*project.ModuleMeta, bool) {
	fileNode := builder.Files.Get(fileID)
	if fileNode == nil {
		return nil, false
	}

	fileSpan := fileNode.Span
	srcFile := fs.Get(fileSpan.File)

	modulePath := srcFile.Path
	if baseDir != "" {
		if rel, err := source.RelativePath(modulePath, baseDir); err == nil {
			modulePath = rel
		}
	}

	fullModulePath, err := project.NormalizeModulePath(modulePath)
	if err != nil {
		if reporter != nil {
			reporter.Report(
				diag.ProjInvalidModulePath,
				diag.SevError,
				fileSpan,
				fmt.Sprintf("invalid module path %q: %v", modulePath, err),
				nil,
				nil,
			)
		}
		return nil, false
	}

	meta := &project.ModuleMeta{
		Path:        fullModulePath,
		Span:        fileSpan,
		ContentHash: srcFile.Hash,
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
		hasCandidate := false
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

		baseExists := moduleFileExists(fs, baseDir, normImport)

		// Добавляем кандидатов вида import foo::bar; -> foo/bar, если такой модуль реально существует.
		if importItem.HasOne {
			if name, ok := interner.Lookup(importItem.One.Name); ok && name != "" {
				candidateSegments := append(append([]string(nil), segments...), name)
				if candidatePath, err := project.ResolveImportPath(fullModulePath, baseDir, candidateSegments); err == nil {
					if moduleFileExists(fs, baseDir, candidatePath) {
						imports = append(imports, project.ImportMeta{
							Path: candidatePath,
							Span: item.Span,
						})
						hasCandidate = true
					}
				}
			}
		}

		if len(importItem.Group) > 0 {
			for _, pair := range importItem.Group {
				if pair.Name == source.NoStringID {
					continue
				}
				name, ok := interner.Lookup(pair.Name)
				if !ok || name == "" {
					continue
				}
				candidateSegments := append(append([]string(nil), segments...), name)
				candidatePath, err := project.ResolveImportPath(fullModulePath, baseDir, candidateSegments)
				if err != nil {
					continue
				}
				if moduleFileExists(fs, baseDir, candidatePath) {
					imports = append(imports, project.ImportMeta{
						Path: candidatePath,
						Span: item.Span,
					})
					hasCandidate = true
				}
			}
		}

		if hasCandidate && !baseExists {
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

func moduleFileExists(fs *source.FileSet, baseDir, modulePath string) bool {
	filePath := modulePathToFilePath(baseDir, modulePath)

	// Проверяем, загружен ли файл в текущем FileSet
	if _, ok := fs.GetLatest(filePath); ok {
		return true
	}
	if baseDir != "" {
		if rel, err := source.RelativePath(filePath, baseDir); err == nil {
			if _, ok := fs.GetLatest(rel); ok {
				return true
			}
		}
	}

	if _, err := os.Stat(filePath); err == nil {
		return true
	}

	return false
}
