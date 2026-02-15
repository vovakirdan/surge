package driver

import (
	"context"
	"path/filepath"

	"surge/internal/ast"
	"surge/internal/source"
)

// enrichModuleResults reruns symbol/semantic resolution for module directories
// using the full module-graph path, while leaving non-module files unchanged
// from the initial per-file diagnostics.
func enrichModuleResults(ctx context.Context, _ string, fileSet *source.FileSet, results []DiagnoseDirResult, opts *DiagnoseOptions) error {
	if fileSet == nil {
		return nil
	}

	pathToIndex := make(map[string]int, len(results))
	moduleDirs := make(map[string]struct{})

	for i := range results {
		resPath := resultPath(fileSet, &results[i])
		if resPath == "" {
			continue
		}
		pathToIndex[filepath.ToSlash(resPath)] = i

		res := &results[i]
		if res.Builder != nil && hasModulePragma(res.Builder, res.ASTFile) {
			moduleDirs[filepath.ToSlash(filepath.Dir(resPath))] = struct{}{}
		}
	}

	if len(moduleDirs) == 0 {
		return nil
	}

	moduleResults := make([]DiagnoseDirResult, 0, len(results))
	for i := range results {
		resPath := resultPath(fileSet, &results[i])
		if resPath == "" {
			continue
		}
		if _, ok := moduleDirs[filepath.ToSlash(filepath.Dir(resPath))]; !ok {
			continue
		}
		moduleResults = append(moduleResults, results[i])
	}
	if len(moduleResults) == 0 {
		return nil
	}

	if err := resolveDirModuleGraph(ctx, fileSet, moduleResults, opts); err != nil {
		return err
	}

	for _, moduleRes := range moduleResults {
		path := resultPath(fileSet, &moduleRes)
		if path == "" {
			continue
		}
		idx, ok := pathToIndex[filepath.ToSlash(path)]
		if !ok {
			continue
		}
		results[idx].Bag = moduleRes.Bag
		results[idx].FileID = moduleRes.FileID
		results[idx].ASTFile = moduleRes.ASTFile
		results[idx].Builder = moduleRes.Builder
		results[idx].Symbols = moduleRes.Symbols
		results[idx].Sema = moduleRes.Sema
	}

	return nil
}

func hasModulePragma(builder *ast.Builder, fileID ast.FileID) bool {
	if builder == nil || fileID == ast.NoFileID {
		return false
	}
	node := builder.Files.Get(fileID)
	if node == nil || len(node.Pragma.Entries) == 0 {
		return false
	}
	interner := builder.StringsInterner
	for _, entry := range node.Pragma.Entries {
		name, ok := interner.Lookup(entry.Name)
		if !ok {
			continue
		}
		if name == "module" || name == "binary" {
			return true
		}
	}
	return false
}
