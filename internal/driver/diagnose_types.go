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
	finalizationIndex finalizationPublicationIndex
	finalizationOwner *sema.Result
	// wholeProgramAuthority marks the two finalization sites that answer for the
	// entire program. Only they derive the operations a program's types require.
	//
	// The mark is carried rather than inferred. Record presence does not
	// identify these sites — a single-file build is the whole program and has no
	// records — and neither does an attached classifier: the per-file path
	// routinely finalizes a semantic result that a whole-program pass already
	// classified, and would then answer for the program out of one file's view
	// of it.
	wholeProgramAuthority bool
}
