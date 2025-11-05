package symbols

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
)

func TestResolveFileDeclaresTopLevelSymbols(t *testing.T) {
	src := `
        import foo::Bar;
        let answer = 42;
        fn compute() {}
        type ID = nothing;
    `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	semaBag := diag.NewBag(16)
	res := ResolveFile(builder, fileID, ResolveOptions{
		Reporter: &diag.BagReporter{Bag: semaBag},
		Validate: true,
	})

	if semaBag.Len() != 0 {
		t.Fatalf("unexpected semantic diagnostics: %d", semaBag.Len())
	}
	if res.Table == nil {
		t.Fatalf("expected table in result")
	}
	if res.Table.Symbols.Len() != 4 { // Bar, answer, compute, ID
		t.Fatalf("expected 4 symbols, got %d", res.Table.Symbols.Len())
	}
}

func TestResolveFileDuplicateLetReported(t *testing.T) {
	src := `
        let value = 1;
        let value = 2;
    `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(8)
	_ = ResolveFile(builder, fileID, ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
		Validate: true,
	})

	if bag.Len() != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", bag.Len())
	}
	if got := bag.Items()[0].Code; got != diag.SemaDuplicateSymbol {
		t.Fatalf("expected SemaDuplicateSymbol, got %v", got)
	}
}

func TestResolveAllowsFunctionOverloads(t *testing.T) {
	src := `
        fn compute() {}
        fn compute(a: int) {}
    `
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(8)
	res := ResolveFile(builder, fileID, ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
		Validate: true,
	})

	if bag.Len() != 0 {
		t.Fatalf("did not expect diagnostics, got %d", bag.Len())
	}

	nameID := builder.StringsInterner.Intern("compute")
	scope := res.Table.Scopes.Get(res.FileScope)
	if scope == nil {
		t.Fatalf("missing file scope")
	}
	candidates := scope.NameIndex[nameID]
	if len(candidates) != 2 {
		t.Fatalf("expected 2 overloads, got %d", len(candidates))
	}
}

func TestResolveFunctionParamDuplicates(t *testing.T) {
	src := `
	    fn f(a: int, a: int) {}
	`
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(8)
	_ = ResolveFile(builder, fileID, ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
		Validate: true,
	})

	if bag.Len() != 1 {
		t.Fatalf("expected exactly 1 diagnostic, got %d", bag.Len())
	}
	if bag.Items()[0].Code != diag.SemaDuplicateSymbol {
		t.Fatalf("expected SemaDuplicateSymbol, got %v", bag.Items()[0].Code)
	}
}

func TestResolveLocalShadowingWarning(t *testing.T) {
	src := `
	    fn f(a: int) {
	        let a = 1;
	    }
	`
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(8)
	_ = ResolveFile(builder, fileID, ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
		Validate: true,
	})

	if bag.Len() != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", bag.Len())
	}
	d := bag.Items()[0]
	if d.Code != diag.SemaShadowSymbol {
		t.Fatalf("expected SemaShadowSymbol, got %v", d.Code)
	}
	if d.Severity != diag.SevWarning {
		t.Fatalf("expected warning severity, got %v", d.Severity)
	}
}

func TestResolveLocalDuplicateLet(t *testing.T) {
	src := `
	    fn f() {
	        let value = 0;
	        let value = 1;
	    }
	`
	builder, fileID, parseBag := parseSnippet(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("unexpected parse diagnostics: %d", parseBag.Len())
	}

	bag := diag.NewBag(8)
	_ = ResolveFile(builder, fileID, ResolveOptions{
		Reporter: &diag.BagReporter{Bag: bag},
		Validate: true,
	})

	if bag.Len() != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", bag.Len())
	}
	if bag.Items()[0].Code != diag.SemaDuplicateSymbol {
		t.Fatalf("expected SemaDuplicateSymbol, got %v", bag.Items()[0].Code)
	}
}

func parseSnippet(t *testing.T, src string) (*ast.Builder, ast.FileID, *diag.Bag) {
	t.Helper()
	fs := source.NewFileSetWithBase("")
	fileID := fs.AddVirtual("snippet.sg", []byte(src))
	file := fs.Get(fileID)

	bag := diag.NewBag(32)

	lx := lexer.New(file, lexer.Options{})
	builder := ast.NewBuilder(ast.Hints{}, nil)

	opts := parser.Options{
		Reporter:  &diag.BagReporter{Bag: bag},
		MaxErrors: uint(bag.Cap()),
	}
	result := parser.ParseFile(fs, lx, builder, opts)

	return builder, result.File, bag
}
