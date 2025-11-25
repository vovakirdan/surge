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
	Exports  map[string]*symbols.ModuleExports
}

// Result stores semantic artefacts produced by the checker.
type Result struct {
	TypeInterner           *types.Interner
	ExprTypes              map[ast.ExprID]types.TypeID
	ExprBorrows            map[ast.ExprID]BorrowID
	Borrows                []BorrowInfo
	FunctionInstantiations map[symbols.SymbolID][][]types.TypeID
}

// Check performs semantic analysis (type inference, borrow checks, etc.).
// At this stage it handles literal typing and basic operator validation.
func Check(builder *ast.Builder, fileID ast.FileID, opts Options) Result {
	res := Result{
		ExprTypes:              make(map[ast.ExprID]types.TypeID),
		ExprBorrows:            make(map[ast.ExprID]BorrowID),
		FunctionInstantiations: make(map[symbols.SymbolID][][]types.TypeID),
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
		types:    res.TypeInterner,
		exports:  opts.Exports,
	}
	checker.run()
	return res
}
