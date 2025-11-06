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
	expected := map[string]bool{
		"Bar":     false,
		"answer":  false,
		"compute": false,
		"ID":      false,
	}
	for _, sym := range res.Table.Symbols.Data() {
		name := builder.StringsInterner.MustLookup(sym.Name)
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
	}
	for name, ok := range expected {
		if !ok {
			t.Fatalf("expected symbol %s to be declared", name)
		}
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
        @overload fn compute(a: int) {}
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

func TestResolveDuplicateFunctionWithoutAttribute(t *testing.T) {
	src := `
        fn compute() {}
        fn compute() {}
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
	item := bag.Items()[0]
	if item.Code != diag.SemaFnOverride {
		t.Fatalf("expected SemaFnOverride, got %v", item.Code)
	}
	if len(item.Fixes) == 0 {
		t.Fatalf("expected quick-fix suggestion")
	}
	f := item.Fixes[0]
	if f.Title != "mark function as override" {
		t.Fatalf("expected override suggestion, got %q", f.Title)
	}
	if len(f.Edits) != 1 {
		t.Fatalf("expected single edit, got %d", len(f.Edits))
	}
	if f.Edits[0].NewText != "@override " {
		t.Fatalf("expected override insertion, got %q", f.Edits[0].NewText)
	}
}

func TestResolveOverrideRequiresExistingFunction(t *testing.T) {
	src := `
        @override fn compute() {}
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
	if bag.Items()[0].Code != diag.SemaFnOverride {
		t.Fatalf("expected SemaFnOverride, got %v", bag.Items()[0].Code)
	}
}

func TestResolveDuplicateFunctionWithoutAttributeSuggestsOverload(t *testing.T) {
	src := `
        fn compute(a: int) {}
        fn compute(a: int, b: int) {}
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
	item := bag.Items()[0]
	if item.Code != diag.SemaFnOverride {
		t.Fatalf("expected SemaFnOverride, got %v", item.Code)
	}
	if len(item.Fixes) == 0 {
		t.Fatalf("expected quick-fix suggestion")
	}
	f := item.Fixes[0]
	if f.Title != "mark function as overload" {
		t.Fatalf("expected overload suggestion, got %q", f.Title)
	}
	if len(f.Edits) != 1 {
		t.Fatalf("expected single edit, got %d", len(f.Edits))
	}
	if f.Edits[0].NewText != "@overload " {
		t.Fatalf("expected overload insertion, got %q", f.Edits[0].NewText)
	}
}

func TestResolveOverloadDuplicateSignature(t *testing.T) {
	src := `
	    fn compute(a: int) {}
	    @overload fn compute(a: int) {}
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
	if bag.Items()[0].Code != diag.SemaFnOverride {
		t.Fatalf("expected SemaFnOverride, got %v", bag.Items()[0].Code)
	}
}

func TestResolveOverrideMismatchedSignature(t *testing.T) {
	src := `
	    fn compute(a: int) {}
	    @override fn compute(a: int, b: int) {}
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
	if bag.Items()[0].Code != diag.SemaFnOverride {
		t.Fatalf("expected SemaFnOverride, got %v", bag.Items()[0].Code)
	}
}

func TestResolveOverrideMatchingSignature(t *testing.T) {
	src := `
	    fn compute(a: int) {}
	    @override fn compute(a: int) {}
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

	if bag.Len() != 0 {
		t.Fatalf("expected no diagnostics, got %d", bag.Len())
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

func TestResolveExprIdentifierMapping(t *testing.T) {
	src := `
	    fn f(a: int) -> int {
	        return a;
	    }
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
		t.Fatalf("unexpected diagnostics: %d", bag.Len())
	}

	file := builder.Files.Get(fileID)
	if file == nil || len(file.Items) == 0 {
		t.Fatalf("expected items in file")
	}
	fnItemData, ok := builder.Items.Fn(file.Items[0])
	if !ok || fnItemData == nil {
		t.Fatalf("failed to fetch function item")
	}
	block := builder.Stmts.Block(fnItemData.Body)
	if block == nil || len(block.Stmts) == 0 {
		t.Fatalf("expected statements in function body")
	}
	ret := builder.Stmts.Return(block.Stmts[0])
	if ret == nil {
		t.Fatalf("expected return statement")
	}
	symID, ok := res.ExprSymbols[ret.Expr]
	if !ok || !symID.IsValid() {
		t.Fatalf("identifier not resolved")
	}
}

func TestResolveUnresolvedIdentifier(t *testing.T) {
	src := `
	    fn f() {
	        return missing;
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
	if bag.Items()[0].Code != diag.SemaUnresolvedSymbol {
		t.Fatalf("expected SemaUnresolvedSymbol, got %v", bag.Items()[0].Code)
	}
}

func TestResolveBuiltinTypes(t *testing.T) {
	src := `
	    fn f(a: int) -> bool {
	        let ok = a is int;
	        return ok;
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

	if bag.Len() != 0 {
		for _, d := range bag.Items() {
			t.Logf("diagnostic: %s", d.Message)
		}
		t.Fatalf("expected no diagnostics, got %d", bag.Len())
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
