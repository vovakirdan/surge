package diagfmt

import (
	"bytes"
	"encoding/json"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
)

func TestJSONIncludesSemantics(t *testing.T) {
	src := "fn demo(a: int) -> int { let value = a; return value; }"
	fs := source.NewFileSetWithBase("")
	fileID := fs.AddVirtual("test.sg", []byte(src))
	file := fs.Get(fileID)

	bag := diag.NewBag(16)
	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, nil)
	parseResult := parser.ParseFile(fs, lx, builder, parser.Options{Reporter: &diag.BagReporter{Bag: bag}})
	if parseResult.File == ast.NoFileID {
		t.Fatalf("parse failed")
	}

	res := symbols.ResolveFile(builder, parseResult.File, &symbols.ResolveOptions{
		Reporter:   &diag.BagReporter{Bag: bag},
		Validate:   true,
		ModulePath: "test",
		FilePath:   file.Path,
	})

	if bag.Len() != 0 {
		t.Fatalf("unexpected diagnostics during setup: %d", bag.Len())
	}

	jsonOpts := JSONOpts{
		IncludePositions: true,
		PathMode:         PathModeBasename,
		IncludeSemantics: true,
	}

	semantics := &SemanticsInput{
		Builder: builder,
		FileID:  parseResult.File,
		Result:  &res,
	}

	var buf bytes.Buffer
	if err := JSON(&buf, bag, fs, jsonOpts, semantics); err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	var output DiagnosticsOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if output.Semantics == nil {
		t.Fatalf("expected semantics block in JSON output")
	}
	if len(output.Semantics.Scopes) == 0 {
		t.Fatalf("expected scopes in semantics output")
	}
	if len(output.Semantics.Symbols) == 0 {
		t.Fatalf("expected symbols in semantics output")
	}
	if len(output.Semantics.ExprBindings) == 0 {
		t.Fatalf("expected expr bindings in semantics output")
	}
}
