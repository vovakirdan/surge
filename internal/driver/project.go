package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/project"
	"surge/internal/source"
)

func buildModuleMeta(
	fs *source.FileSet,
	builder *ast.Builder,
	fileIDs []ast.FileID,
	baseDir string,
	mapping *project.ModuleMapping,
	reporter diag.Reporter,
) (*project.ModuleMeta, bool) {
	if builder == nil || len(fileIDs) == 0 {
		return nil, false
	}

	type moduleFile struct {
		id   ast.FileID
		node *ast.File
	}
	files := make([]moduleFile, 0, len(fileIDs))
	for _, id := range fileIDs {
		if node := builder.Files.Get(id); node != nil {
			files = append(files, moduleFile{id: id, node: node})
		}
	}
	if len(files) == 0 {
		return nil, false
	}

	dirPath := filepath.Dir(fs.Get(files[0].node.Span.File).Path)
	logicalDir := logicalPathForDir(dirPath, baseDir, mapping)
	normDir := filepath.ToSlash(logicalDir)
	dirName := filepath.Base(dirPath)
	if normDir != "" && normDir != "." {
		dirName = filepath.Base(normDir)
	}

	moduleName := ""
	var kind project.ModuleKind
	hasPragma := false
	pragmaKinds := make(map[project.ModuleKind]source.Span)
	explicitNames := make(map[string]source.Span)
	hasNoStd := false
	hasStd := false
	filesWithPragma := make(map[ast.FileID]source.Span)
	filesWithExplicit := make(map[ast.FileID]struct{})
	interner := builder.StringsInterner
	for _, mf := range files {
		node := mf.node
		if node == nil {
			continue
		}
		fileHasPragma := false
		if !node.Pragma.IsEmpty() {
			for _, entry := range node.Pragma.Entries {
				name, _ := interner.Lookup(entry.Name)
				switch name {
				case "module", "binary":
					hasPragma = true
					fileHasPragma = true
					entryKind := project.ModuleKindModule
					if name == "binary" {
						entryKind = project.ModuleKindBinary
					}
					pragmaKinds[entryKind] = entry.Span
					raw, _ := interner.Lookup(entry.Raw)
					if raw == "" {
						raw = name
					}
					if strings.Contains(raw, "::") {
						parts := strings.SplitN(raw, "::", 2)
						if len(parts) == 2 && parts[1] != "" {
							explicit := strings.TrimSpace(parts[1])
							explicit = strings.TrimRight(explicit, ";,")
							explicit = strings.TrimSpace(explicit)
							if explicit != "" {
								explicitNames[explicit] = entry.Span
							}
							filesWithExplicit[mf.id] = struct{}{}
						}
					}
				}
			}
		}
		if node.Pragma.Flags&ast.PragmaFlagNoStd != 0 {
			hasNoStd = true
		} else {
			hasStd = true
		}
		if fileHasPragma {
			filesWithPragma[mf.id] = node.Span
		}
	}
	if hasPragma && hasNoStd && hasStd && reporter != nil {
		reporter.Report(diag.ProjInconsistentNoStd, diag.SevError, files[0].node.Span, "pragma no_std must be consistent across all files in a module", nil, nil)
	}

	if hasPragma {
		if len(filesWithPragma) != len(files) && reporter != nil {
			for _, mf := range files {
				if _, ok := filesWithPragma[mf.id]; ok {
					continue
				}
				reporter.Report(diag.ProjMissingModulePragma, diag.SevError, mf.node.Span, "all .sg files in a directory with pragma module/binary must declare a module pragma", nil, nil)
			}
		}
		if len(explicitNames) > 1 && reporter != nil {
			reporter.Report(diag.ProjInconsistentModuleName, diag.SevError, files[0].node.Span, "inconsistent module names within the same directory", nil, nil)
		}
		if len(explicitNames) == 1 && len(filesWithExplicit) != len(filesWithPragma) && reporter != nil {
			reporter.Report(diag.ProjInconsistentModuleName, diag.SevError, files[0].node.Span, "all files must use the same explicit module name", nil, nil)
		}
		if len(explicitNames) == 1 {
			for name := range explicitNames {
				moduleName = name
				break
			}
		}
		if moduleName == "" {
			moduleName = dirName
		}
		if !project.IsValidModuleIdent(moduleName) {
			if reporter != nil {
				msg := fmt.Sprintf("directory name %q is not a valid module identifier; specify an explicit name with ::", dirName)
				reporter.Report(diag.ProjInvalidModulePath, diag.SevError, files[0].node.Span, msg, nil, nil)
			}
			return nil, false
		}
		if len(pragmaKinds) > 1 && reporter != nil {
			reporter.Report(diag.ProjInvalidModulePath, diag.SevError, files[0].node.Span, "cannot mix module and binary pragmas in one directory", nil, nil)
		}
		if _, ok := pragmaKinds[project.ModuleKindBinary]; ok {
			kind = project.ModuleKindBinary
		} else {
			kind = project.ModuleKindModule
		}
	} else {
		filePath := logicalPathForFile(fs.Get(files[0].node.Span.File).Path, baseDir, mapping)
		if norm, err := project.NormalizeModulePath(filePath); err == nil {
			moduleName = filepath.Base(norm)
			normDir = filepath.Dir(norm)
			kind = project.ModuleKindModule
		} else {
			moduleName = filepath.Base(filePath)
			kind = project.ModuleKindModule
		}
	}

	pathSegments := []string{}
	if normDir != "" && normDir != "." {
		pathSegments = append(pathSegments, strings.Split(filepath.ToSlash(normDir), "/")...)
	}
	if len(pathSegments) == 0 || pathSegments[len(pathSegments)-1] != moduleName {
		pathSegments = append(pathSegments, moduleName)
	}
	fullPath, err := project.NormalizeModulePath(strings.Join(pathSegments, "/"))
	if err != nil {
		if reporter != nil {
			reporter.Report(
				diag.ProjInvalidModulePath,
				diag.SevError,
				files[0].node.Span,
				fmt.Sprintf("invalid module path %q: %v", strings.Join(pathSegments, "/"), err),
				nil,
				nil,
			)
		}
		return nil, false
	}

	imports := make([]project.ImportMeta, 0, 8)
	for _, mf := range files {
		node := mf.node
		if node == nil {
			continue
		}
		fileImports := collectImports(fs, builder, node, baseDir, mapping, reporter)
		imports = append(imports, fileImports...)
	}

	type fileInfo struct {
		path string
		span source.Span
		hash project.Digest
	}
	fileInfos := make([]fileInfo, 0, len(files))
	for _, mf := range files {
		node := mf.node
		if node == nil {
			continue
		}
		src := fs.Get(node.Span.File)
		filePath := logicalPathForFile(src.Path, baseDir, mapping)
		fileInfos = append(fileInfos, fileInfo{
			path: filepath.ToSlash(filePath),
			span: node.Span,
			hash: src.Hash,
		})
	}
	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].path < fileInfos[j].path
	})

	var contentHash project.Digest
	if len(fileInfos) == 1 {
		contentHash = fileInfos[0].hash
	} else {
		digests := make([]project.Digest, 0, len(fileInfos))
		for _, info := range fileInfos {
			digests = append(digests, info.hash)
		}
		contentHash = combineModuleContent(digests)
	}

	fileMetas := make([]project.ModuleFileMeta, 0, len(fileInfos))
	for _, info := range fileInfos {
		fileMetas = append(fileMetas, project.ModuleFileMeta{
			Path: info.path,
			Span: info.span,
			Hash: info.hash,
		})
	}

	meta := &project.ModuleMeta{
		Name:            moduleName,
		Path:            fullPath,
		Dir:             strings.Trim(filepath.ToSlash(normDir), "/"),
		Kind:            kind,
		NoStd:           hasNoStd && !hasStd,
		HasModulePragma: hasPragma,
		Span:            files[0].node.Span,
		Imports:         imports,
		Files:           fileMetas,
		ContentHash:     contentHash,
	}

	return meta, true
}

func collectImports(
	fs *source.FileSet,
	builder *ast.Builder,
	fileNode *ast.File,
	baseDir string,
	mapping *project.ModuleMapping,
	reporter diag.Reporter,
) []project.ImportMeta {
	if builder == nil || fileNode == nil {
		return nil
	}
	fileSpan := fileNode.Span
	srcFile := fs.Get(fileSpan.File)

	modulePath := logicalPathForFile(srcFile.Path, baseDir, mapping)

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
		return nil
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

		baseExists := moduleFileExists(fs, baseDir, normImport, mapping)
		if baseExists {
			imports = append(imports, project.ImportMeta{
				Path: normImport,
				Span: item.Span,
			})
			continue
		}

		// Добавляем кандидатов вида import foo::bar; -> foo/bar, если такой модуль реально существует.
		if importItem.HasOne {
			if name, ok := interner.Lookup(importItem.One.Name); ok && name != "" {
				candidateSegments := append(append([]string(nil), segments...), name)
				if candidatePath, err := project.ResolveImportPath(fullModulePath, baseDir, candidateSegments); err == nil {
					if moduleFileExists(fs, baseDir, candidatePath, mapping) {
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
				if moduleFileExists(fs, baseDir, candidatePath, mapping) {
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

	return imports
}

func moduleFileExists(fs *source.FileSet, baseDir, modulePath string, mapping *project.ModuleMapping) bool {
	if moduleFileExistsInBase(fs, baseDir, modulePath) {
		return true
	}
	if root, rest, ok := resolveMappedModulePath(modulePath, mapping); ok {
		return moduleFileExistsInBase(fs, root, rest)
	}
	return false
}

func moduleFileExistsInBase(fs *source.FileSet, baseDir, modulePath string) bool {
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
	dirPath := filepath.FromSlash(modulePath)
	if baseDir != "" {
		dirPath = filepath.Join(baseDir, dirPath)
	}
	if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
		if dirHasModuleFiles(dirPath) {
			return true
		}
	}

	return false
}

func dirHasModuleFiles(dirPath string) bool {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return false
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		if filepath.Ext(ent.Name()) == ".sg" {
			return true
		}
	}
	return false
}

func combineModuleContent(parts []project.Digest) project.Digest {
	if len(parts) == 0 {
		return project.Digest{}
	}
	acc := parts[0]
	for i := 1; i < len(parts); i++ {
		acc = combineDigest(acc, parts[i])
	}
	return acc
}
