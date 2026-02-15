package driver

import (
	"bytes"
	"context"

	"surge/internal/ast"
	"surge/internal/source"
)

// enrichModuleResults runs a full module-graph pass for directory diagnostics
// when module directories are present.
func enrichModuleResults(ctx context.Context, _ string, fileSet *source.FileSet, results []DiagnoseDirResult, opts *DiagnoseOptions) error {
	if fileSet == nil {
		return nil
	}
	if opts == nil {
		opts = &DiagnoseOptions{}
	}
	if !hasModulePragmaInResults(fileSet, results) {
		return nil
	}
	return resolveDirModuleGraph(ctx, fileSet, results, opts)
}

func hasModulePragmaInResults(fileSet *source.FileSet, results []DiagnoseDirResult) bool {
	for i := range results {
		res := &results[i]
		if hasModulePragma(res.Builder, res.ASTFile) {
			return true
		}
		path := resultPath(fileSet, res)
		file := resultFile(fileSet, res, path)
		if file == nil {
			continue
		}
		if bytes.Contains(file.Content, []byte("pragma module::")) {
			return true
		}
		if bytes.Contains(file.Content, []byte("pragma module")) {
			return true
		}
		if bytes.Contains(file.Content, []byte("pragma binary::")) {
			return true
		}
		if bytes.Contains(file.Content, []byte("pragma binary")) {
			return true
		}
	}
	return false
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
