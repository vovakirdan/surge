package fuzztests

import (
	"testing"

	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/source"
)

const maxFuzzInput = 1 << 16 // 64 KiB

func FuzzLexerTokens(f *testing.F) {
	addCorpusSeeds(f)
	f.Fuzz(func(_ *testing.T, input []byte) {
		if len(input) > maxFuzzInput {
			input = append([]byte(nil), input[:maxFuzzInput]...)
		} else {
			input = append([]byte(nil), input...)
		}

		fs := source.NewFileSet()
		fileID := fs.AddVirtual("fuzz.sg", input)
		file := fs.Get(fileID)

		bag := diag.NewBag(64)
		reporter := diag.BagReporter{Bag: bag}
		lx := lexer.New(file, lexer.Options{Reporter: reporter})
		for {
			tok := lx.Next()
			if tok.Kind.IsEOF() {
				break
			}
		}
	})
}
