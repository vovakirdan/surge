package driver

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/directive"
	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/observ"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
)

// DiagnoseResult encapsulates the artifacts and diagnostics from a compilation phase.
type DiagnoseResult struct {
	FileSet           *source.FileSet
	File              *source.File
	FileID            ast.FileID
	Bag               *diag.Bag
	Builder           *ast.Builder
	Symbols           *symbols.Result
	Sema              *sema.Result
	Instantiations    *mono.InstantiationMap
	DirectiveRegistry *directive.Registry
	HIR               *hir.Module
	TimingReport      observ.Report
	rootRecord        *moduleRecord
	moduleRecords     map[string]*moduleRecord
}
