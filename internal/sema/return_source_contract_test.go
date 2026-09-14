package sema

import (
	"context"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestReturnSourceContractDeclarations(t *testing.T) {
	cases := []struct {
		name, src, message string
		code               diag.Code
		slot               uint32
	}{
		{"explicit_source", "contract C { fn choose(a: &string, @return_source b: &string) -> &string; }", "", 0, 1},
		{"explicit_self", "contract C<T> { fn choose(@return_source self: &T) -> &T; }", "", 0, 0},
		{"generic_deferred", "contract C<T> { fn choose(@return_source self: T) -> T; }", "", 0, 0},
		{"wrong_arity", "contract C { fn choose(@return_source(1) a: &string) -> &string; }", "@return_source does not accept arguments", diag.SemaError, 0},
		{"wrong_target", "contract C { @return_source fn choose(a: &string) -> &string; }", "attribute '@return_source' is not allowed here", diag.SemaContractUnknownAttr, 0},
		{"owned_parameter", "contract C { fn choose(@return_source a: string) -> &string; }", "@return_source requires a reference-bearing parameter", diag.SemaError, 0},
		{"owned_result", "contract C { fn choose(@return_source a: &string) -> string; }", "@return_source requires a reference-bearing result", diag.SemaError, 0},
		{"original_owner", "contract C<T> { fn choose(@return_source self: T) -> T; } fn unrelated(x: int) {}", "", 0, 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			builder, file, parseBag := parseSource(t, test.src)
			if parseBag.HasErrors() {
				t.Fatalf("contract source parse failed: %s", diagnosticsSummary(parseBag))
			}
			syms := resolveSymbols(t, builder, file)
			bag := diag.NewBag(32)
			result := Check(context.Background(), builder, file, Options{Symbols: syms, Reporter: &diag.BagReporter{Bag: bag}})
			if test.message != "" {
				if bag.Len() != 1 || bag.Items()[0].Code != test.code || bag.Items()[0].Message != test.message {
					t.Fatalf("want exactly %s %q, got %s", test.code, test.message, diagnosticsSummary(bag))
				}
				if site := bag.Items()[0].Primary; site.File != builder.Files.Get(file).Span.File || site.End <= site.Start {
					t.Fatal("declaration diagnostic lost its source location")
				}
				return
			}
			if bag.HasErrors() {
				t.Fatalf("valid contract rejected: %s", diagnosticsSummary(bag))
			}
			owner := lookupSymbolByName(syms, builder.StringsInterner.Intern("C"))
			sym := syms.Table.Symbols.Get(owner)
			if sym == nil || sym.Kind != symbols.SymbolContract || sym.Contract == nil {
				t.Fatal("accepted source has no typed contract")
			}
			methods := sym.Contract.Methods[builder.StringsInterner.Intern("choose")]
			if len(methods) != 1 || len(result.ReturnSourceDeclarations) != 1 {
				t.Fatalf("methods/typed original requests = %d/%d, want 1/1", len(methods), len(result.ReturnSourceDeclarations))
			}
			method, request := methods[0], result.ReturnSourceDeclarations[0]
			if request.Owner != owner || method.ReturnSourceOwner != owner || request.TypeExpr != ast.NoTypeID || request.Syntax.Span() != method.Span ||
				request.Syntax.Span().File != builder.Files.Get(file).Span.File || request.SourceKey != "" || request.TemplateKey != "" ||
				!slices.Equal(request.Params(), method.Params) || request.Result() != method.Result ||
				!slices.Equal(method.ReturnSourceSyntax.Sources().Slots(), []uint32{test.slot}) {
				t.Fatal("contract request lost its original owner, physical slots or typed roots")
			}
			state := ValidateDeclaredReturnSources(result.TypeInterner, request).Status
			if test.name == "generic_deferred" || test.name == "original_owner" {
				if state != ReturnSourcesDeferred || !types.ContainsGenericParam(result.TypeInterner, request.Result()) {
					t.Fatal("generic contract was replaced by a concrete declaration")
				}
				checkConditionalReturnSource(t, result.TypeInterner, request)
			} else if state != ReturnSourcesValid {
				t.Fatalf("eligible typed contract has status %v", state)
			}
		})
	}
}

func checkImportedReturnSourceContract(t *testing.T) {
	t.Helper()
	fs, stringsIn, typesIn := source.NewFileSet(), source.NewInterner(), types.NewInterner()
	exports := make(map[string]*symbols.ModuleExports)
	parse := func(module, text string) (*ast.Builder, ast.FileID, *symbols.Result, *diag.Bag) {
		t.Helper()
		file := fs.Get(fs.AddVirtual("/"+module+".sg", []byte(text)))
		builder, bag := ast.NewBuilder(ast.Hints{}, stringsIn), diag.NewBag(64)
		reporter := &diag.BagReporter{Bag: bag}
		parsed := parser.ParseFile(context.Background(), fs, lexer.New(file, lexer.Options{Reporter: reporter}), builder, parser.Options{Reporter: reporter})
		if bag.HasErrors() {
			t.Fatalf("%s source parse failed: %s", module, diagnosticsSummary(bag))
		}
		syms := symbols.ResolveFile(builder, parsed.File, &symbols.ResolveOptions{
			Reporter: reporter, ModulePath: module, FilePath: file.Path, BaseDir: "/", ModuleExports: exports,
		})
		if bag.HasErrors() {
			t.Fatalf("%s source resolution failed: %s", module, diagnosticsSummary(bag))
		}
		return builder, parsed.File, &syms, bag
	}
	provider, providerFile, providerSyms, providerBag := parse("provider", `pub contract C<T> {
    fn choose(self: T, @return_source value: &string) -> &string;
}`)
	providerResult := Check(context.Background(), provider, providerFile, Options{
		Symbols: providerSyms, Types: typesIn, Reporter: &diag.BagReporter{Bag: providerBag}, ModulePath: stringsIn.Intern("provider"),
	})
	if providerBag.HasErrors() || len(providerResult.ReturnSourceDeclarations) != 1 {
		t.Fatalf("provider did not publish its original typed promise: %s", diagnosticsSummary(providerBag))
	}
	original := providerResult.ReturnSourceDeclarations[0]
	exports["provider"] = symbols.CollectExports(provider, *providerSyms, "provider")
	public := exports["provider"].Lookup("C")
	if len(public) != 1 || public[0].Contract == nil {
		t.Fatal("producer exports have no typed contract")
	}
	member := public[0].Contract.Methods[stringsIn.Intern("choose")][0]
	if member.ReturnSourceOwner != original.Owner || member.Span != original.Syntax.Span() {
		t.Fatal("CollectExports lost the original member owner or span")
	}
	consumer, consumerFile, consumerSyms, consumerBag := parse("consumer", `import provider::C;
fn padding0() {} fn padding1() {} fn padding2() {}
fn use<E: C<E>>(e: E, value: &string) -> &string { return e.choose(value); }`)
	consumerResult := Check(context.Background(), consumer, consumerFile, Options{
		Symbols: consumerSyms, Types: typesIn, Exports: exports, Reporter: &diag.BagReporter{Bag: consumerBag}, ModulePath: stringsIn.Intern("consumer"),
	})
	if consumerBag.HasErrors() {
		t.Fatalf("consumer source rejected: %s", diagnosticsSummary(consumerBag))
	}
	edges := consumerResult.InstantiationGraph.DeferredCallables()
	if len(edges) != 1 || len(edges[0].Requirement.Contracts) != 1 {
		t.Fatalf("consumer did not reach its imported bound method: %d edges", len(edges))
	}
	records := edges[0].Requirement.ReturnSourceRequirements()
	if len(records) != 1 || records[0].Contract != original.Owner || records[0].Member != original.Syntax.Span() ||
		!records[0].Sources.Equal(original.Syntax.Sources()) || records[0].Member.File == consumer.Files.Get(consumerFile).Span.File ||
		edges[0].Requirement.Contracts[0] == original.Owner || len(consumerResult.ReturnSourceDeclarations) != 0 {
		t.Fatal("imported source promise was rebound to caller ID, file, or declaration context")
	}
	t.Logf("completed source producer -> SEMA -> CollectExports -> consumer ResolveFile/SEMA: owner=%d member=%v imported-contract=%d", original.Owner, original.Syntax.Span(), edges[0].Requirement.Contracts[0])
}
