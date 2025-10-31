package fuzztests

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

func FuzzParserBuildsAST(f *testing.F) {
	addCorpusSeeds(f)
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > maxFuzzInput {
			input = append([]byte(nil), input[:maxFuzzInput]...)
		} else {
			input = append([]byte(nil), input...)
		}

		fs := source.NewFileSet()
		fileID := fs.AddVirtual("fuzz.sg", input)
		file := fs.Get(fileID)

		bag := diag.NewBag(128)
		reporter := diag.BagReporter{Bag: bag}
		lx := lexer.New(file, lexer.Options{Reporter: reporter})

		builder := ast.NewBuilder(ast.Hints{}, nil)
		opts := parser.Options{
			Reporter:  reporter,
			MaxErrors: 128,
		}

		_ = parser.ParseFile(fs, lx, builder, opts)
	})
}
