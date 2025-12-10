package fuzztests

import (
	"context"
	"testing"
	"time"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

// parseTimeout is the maximum time allowed for parsing a single input.
// If parsing takes longer, it indicates a potential infinite loop.
const parseTimeout = 5 * time.Second

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

		_ = parser.ParseFile(context.Background(), fs, lx, builder, opts)
	})
}

// FuzzParserNoHang tests that the parser doesn't hang on any input.
// It uses a timeout to detect infinite loops that could be caused by
// malformed input or edge cases in error recovery.
func FuzzParserNoHang(f *testing.F) {
	addCorpusSeeds(f)

	// Add specific edge cases that previously caused hangs
	f.Add([]byte("fn test() { let x: int = 1\nlet y: int = 2; }"))  // missing semicolon
	f.Add([]byte("fn test() { x + y\nlet z: int = 3; }"))           // expression without semicolon
	f.Add([]byte("async fn producer<T>(channel: &Channel<T>) {}"))  // complex async signature
	f.Add([]byte("{ let x = 1 }"))                                  // block without semicolons
	f.Add([]byte("fn f() { { { { } } } }"))                         // deeply nested blocks
	f.Add([]byte("fn f() { compare x { } }"))                       // empty compare
	f.Add([]byte("fn f() { for (let i = 0 i < 10 i = i + 1) {} }")) // for without semicolons

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > maxFuzzInput {
			input = append([]byte(nil), input[:maxFuzzInput]...)
		} else {
			input = append([]byte(nil), input...)
		}

		// Create a context with timeout to detect hangs
		ctx, cancel := context.WithTimeout(context.Background(), parseTimeout)
		defer cancel()

		// Run parser in a goroutine
		done := make(chan struct{})
		go func() {
			defer close(done)

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

			_ = parser.ParseFile(ctx, fs, lx, builder, opts)
		}()

		// Wait for completion or timeout
		select {
		case <-done:
			// Parser completed successfully
		case <-ctx.Done():
			t.Fatalf("parser hang detected: parsing took longer than %v\ninput (%d bytes): %q",
				parseTimeout, len(input), truncateForLog(input, 200))
		}
	})
}

// truncateForLog truncates input for logging purposes
func truncateForLog(input []byte, maxLen int) []byte {
	if len(input) <= maxLen {
		return input
	}
	return append(input[:maxLen], []byte("...")...)
}
