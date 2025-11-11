package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Options configure a semantic pass over a file.
type Options struct {
	Reporter diag.Reporter
	Symbols  *symbols.Result
	Types    *types.Interner
}

// Result stores semantic artefacts produced by the checker.
type Result struct {
	TypeInterner *types.Interner
	ExprTypes    map[ast.ExprID]types.TypeID
}

// Check performs semantic analysis (type inference, borrow checks, etc.).
// At this stage it only initializes bookkeeping structures; future iterations
// will populate ExprTypes and emit diagnostics.
func Check(builder *ast.Builder, fileID ast.FileID, opts Options) Result {
	res := Result{
		ExprTypes: make(map[ast.ExprID]types.TypeID),
	}
	if opts.Types != nil {
		res.TypeInterner = opts.Types
	} else {
		res.TypeInterner = types.NewInterner()
	}
	if builder == nil || fileID == ast.NoFileID {
		return res
	}

	checker := typeChecker{
		builder:  builder,
		fileID:   fileID,
		reporter: opts.Reporter,
		symbols:  opts.Symbols,
		result:   &res,
	}
	checker.run()
	return res
}

type typeChecker struct {
	builder  *ast.Builder
	fileID   ast.FileID
	reporter diag.Reporter
	symbols  *symbols.Result
	result   *Result
}

func (tc *typeChecker) run() {
	if tc.builder == nil || tc.result == nil {
		return
	}
	file := tc.builder.Files.Get(tc.fileID)
	if file == nil {
		return
	}
	// Future: walk AST items/statements to populate ExprTypes and diagnostics.
	_ = file
}
