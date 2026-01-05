package driver

import (
	"context"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

type ParseResult struct {
	FileSet *source.FileSet
	File    *source.File
	Builder *ast.Builder
	FileID  ast.FileID
	Bag     *diag.Bag
}

func Parse(filePath string, maxDiagnostics int) (*ParseResult, error) {
	fs := source.NewFileSet()
	fileID, err := fs.Load(filePath)
	if err != nil {
		return nil, err
	}
	file := fs.Get(fileID)

	bag := diag.NewBag(maxDiagnostics)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, nil)

	var maxErrors uint
	maxErrors, err = safecast.Conv[uint](maxDiagnostics)
	if err != nil {
		return nil, err
	}

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: maxErrors,
	}

	result := parser.ParseFile(context.Background(), fs, lx, builder, opts)

	return &ParseResult{
		FileSet: fs,
		File:    file,
		Builder: builder,
		FileID:  result.File,
		Bag:     bag,
	}, nil
}
