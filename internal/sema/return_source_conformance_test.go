package sema

import (
	"context"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/lexer"
	"surge/internal/parser"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestReturnSourceDirectConformanceTransport(t *testing.T) {
	for _, name := range []string{"direct_zero_deferred", "wider_declared", "narrower_declared", "inferred_body_identity", "imported_wider_origin", "overload_identity", "generic_entailment", "repeat_check", "explicit_receiver", "implicit_receiver"} {
		t.Run(name, func(t *testing.T) {
			if name == "imported_wider_origin" {
				checkImportedDirectConformance(t)
				return
			}
			if name == "explicit_receiver" || name == "implicit_receiver" {
				checkDirectConformanceReceiver(t, name == "implicit_receiver")
				return
			}
			required := "self: T, @return_source left: &string, right: &string"
			actual := "self: Foo, @return_source left: &string, right: &string"
			prefix, overload := "", ""
			switch name {
			case "wider_declared":
				actual = "self: Foo, @return_source left: &string, @return_source right: &string"
			case "narrower_declared":
				required = "self: T, @return_source left: &string, @return_source right: &string"
			case "inferred_body_identity":
				actual = "self: Foo, left: &string, right: &string"
			case "overload_identity":
				prefix = "@overload "
				overload = "@overload fn choose(self: Foo, left: int64, @return_source right: &string) -> &string { return right; }"
			}
			src := "contract C<T> { fn choose(" + required + ") -> &string; }\n" +
				"type Foo = {}\nextern<Foo> { " + overload + prefix + "fn choose(" + actual + ") -> &string { return left; } }\n" +
				"fn require<E: C<E>>(e: E) {}\n"
			if name == "generic_entailment" {
				src += "fn relay<T: C<T>>(e: T) { require(e); }"
			} else {
				src += "fn demo(e: Foo) { require(e); }"
			}
			tc, bag, syms := newContractChecker(t, src)
			facts := tc.result.ReturnSourceConformances()
			if len(facts) != 1 || len(tc.result.InstantiationGraph.DeferredCallables()) != 0 {
				t.Fatalf("direct bound lost its independent obligation: facts=%d deferred=%d", len(facts), len(tc.result.InstantiationGraph.DeferredCallables()))
			}
			fact := cloneReturnSourceConformance(facts[0])
			require := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("require"))
			if name != "generic_entailment" && !slices.ContainsFunc(tc.result.InstantiationGraph.Roots(), func(root InstantiationRoot) bool { return root.Template == require }) {
				t.Fatal("zero deferred count did not accompany an actual concrete require instantiation")
			}
			owner := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("C"))
			member := syms.Table.Symbols.Get(owner).Contract.Methods[tc.builder.StringsInterner.Intern("choose")][0]
			if fact.Requirement.Contract != owner || fact.Requirement.Member != member.Span ||
				!fact.Requirement.Sources.Equal(member.ReturnSourceSyntax.Sources()) || fact.Requirement.ReceiverPrefix != 0 ||
				len(fact.Params) != 3 || fact.Result == types.NoTypeID || fact.Use.End <= fact.Use.Start || !fact.Caller.IsValid() {
				t.Fatal("direct bound lost original member, physical formals, result or use context")
			}
			if name == "generic_entailment" {
				relay := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("relay"))
				if !slices.ContainsFunc(tc.result.InstantiationGraph.Edges(), func(edge InstantiationEdge) bool { return edge.Caller == relay && edge.Callee == require }) {
					t.Fatal("generic assumption did not accompany an actual relay -> require edge")
				}
				if fact.Kind != ReturnSourceConformanceEntailed || fact.Actual.Symbol.IsValid() || fact.Actual.Declaration != (source.Span{}) ||
					fact.GenericParamOwner != relay || fact.GenericParamIndex != 0 || fact.Caller != relay || len(fact.CallerBindings) != 1 ||
					fact.CallerBindings[0].Owner != relay || fact.CallerBindings[0].Param != fact.Target ||
					slices.Equal(fact.Bound.GenericArgs, fact.ConcreteBound.GenericArgs) {
					t.Fatalf("generic assumption acquired a verdict or lost its original substitution: %+v", fact)
				}
				tc.types.RemapTypeParamOwners(map[uint32]uint32{uint32(relay): uint32(relay) + 1000})
				if got := tc.result.ReturnSourceConformances()[0]; got.GenericParamOwner != relay || got.CallerBindings[0].Owner != relay {
					t.Fatal("generic original owner was read from remapped interner metadata")
				}
				return
			}
			if fact.Kind != ReturnSourceConformanceSelected || !fact.Actual.Symbol.IsValid() {
				t.Fatalf("concrete source did not retain the selected local declaration: %+v", fact.Actual)
			}
			selected := syms.Table.Symbols.Get(fact.Actual.Symbol)
			if selected == nil || selected.Signature == nil || selected.Signature.ReturnSourceSyntax.Span() != fact.Actual.Declaration ||
				!selected.Signature.ReturnSourceSyntax.Sources().Equal(fact.Actual.Sources) {
				t.Fatal("selected symbol and original declaration provenance disagree")
			}
			body := src[fact.Actual.Declaration.Start:fact.Actual.Declaration.End]
			if !strings.Contains(body, "return left;") || strings.Contains(body, "return right;") {
				t.Fatalf("recorder selected another overload/body: %s", body)
			}
			want := types.ExplicitReturnSources(1)
			if name == "wider_declared" {
				want = types.ExplicitReturnSources(1, 2)
			} else if name == "inferred_body_identity" {
				want = types.ReturnSources{}
			}
			if !fact.Actual.Sources.Equal(want) {
				t.Fatal("transport changed declared sources before the separate inference/relation phase")
			}
			if name == "repeat_check" {
				pop := tc.pushFnSym(fact.Caller)
				defer pop()
				for range 2 {
					if !tc.checkContractSatisfactionWithOrigin(fact.Target, fact.ConcreteBound, fact.Bound, fact.Use, "") || bag.HasErrors() {
						t.Fatalf("repeated ordinary match changed admission: %s", diagnosticsSummary(bag))
					}
				}
				if len(tc.result.ReturnSourceConformances()) != 1 {
					t.Fatal("checker passes duplicated the same source obligation")
				}
				facts[0].Params[0], facts[0].Bound.GenericArgs[0], facts[0].ConcreteBound.GenericArgs[0] = types.NoTypeID, types.NoTypeID, types.NoTypeID
				facts[0].Actual.Symbol = symbols.NoSymbolID
				if !returnSourceConformancesEqual(tc.result.ReturnSourceConformances()[0], fact) {
					t.Fatal("detached getter exposed retained conformance slices")
				}
			}
			if name == "direct_zero_deferred" {
				bad := strings.Replace(src, "contract C<T> {", "contract C<T> { field missing: int64;", 1)
				builder, file, parseBag := parseSource(t, bad)
				if parseBag.HasErrors() {
					t.Fatalf("whole-contract negative did not parse: %s", diagnosticsSummary(parseBag))
				}
				reporter := diag.NewBag(32)
				result := Check(context.Background(), builder, file, Options{Symbols: resolveSymbols(t, builder, file), Reporter: &diag.BagReporter{Bag: reporter}})
				if !hasCodeContract(reporter, diag.SemaContractMissingField) || len(result.ReturnSourceConformances()) != 0 {
					t.Fatal("failed whole contract published a partial method conformance")
				}
				t.Log("completed direct bound with zero deferred calls and whole-contract failure control")
			}
		})
	}
}

func checkDirectConformanceReceiver(t *testing.T, implicit bool) {
	t.Helper()
	src := `contract C<T> { fn choose(@return_source self: T, other: T) -> T; }
type Foo = {}
extern<Foo> { fn choose(@return_source self: &Foo, other: &Foo) -> &Foo; }`
	wantPrefix := uint8(0)
	if implicit {
		src = `contract C<T> { fn choose(@return_source other: T) -> T; }
type Foo = {}
extern<Foo> { fn choose(self: &Foo, @return_source other: &Foo) -> &Foo; }`
		wantPrefix = 1
	}
	tc, bag, syms := newContractChecker(t, src)
	owner := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("C"))
	contract := syms.Table.Symbols.Get(owner)
	foo := syms.Table.Symbols.Get(lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("Foo")))
	ref := tc.types.Intern(types.MakeReference(foo.Type, false))
	bound := symbols.BoundInstance{Contract: owner, GenericArgs: []types.TypeID{ref}, Span: contract.Span}
	if !tc.checkContractSatisfaction(ref, bound, contract.Span, "") || bag.HasErrors() {
		t.Fatalf("receiver source contract failed: %s", diagnosticsSummary(bag))
	}
	facts := tc.result.ReturnSourceConformances()
	if len(facts) != 1 || facts[0].Requirement.ReceiverPrefix != wantPrefix || len(facts[0].Params) != 2 ||
		!slices.Equal(facts[0].Requirement.Sources.Slots(), []uint32{0}) || !slices.Equal(facts[0].Actual.Sources.Slots(), []uint32{uint32(wantPrefix)}) ||
		facts[0].Params[0] != ref || facts[0].Params[1] != ref || facts[0].Requirement.Contract != owner {
		t.Fatalf("direct receiver conformance did not preserve exactly one physical alignment: %+v", facts)
	}
}

func checkImportedDirectConformance(t *testing.T) {
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
			t.Fatalf("%s parse failed: %s", module, diagnosticsSummary(bag))
		}
		syms := symbols.ResolveFile(builder, parsed.File, &symbols.ResolveOptions{Reporter: reporter, ModulePath: module, FilePath: file.Path, BaseDir: "/", ModuleExports: exports})
		if bag.HasErrors() {
			t.Fatalf("%s resolution failed: %s", module, diagnosticsSummary(bag))
		}
		return builder, parsed.File, &syms, bag
	}
	provider, providerFile, providerSyms, providerBag := parse("provider", `pub type Foo = {}
extern<Foo> { pub fn choose(self: Foo, @return_source left: &string, @return_source right: &string) -> &string { return left; } }`)
	providerResult := Check(context.Background(), provider, providerFile, Options{Symbols: providerSyms, Types: typesIn, Reporter: &diag.BagReporter{Bag: providerBag}, ModulePath: stringsIn.Intern("provider")})
	if providerBag.HasErrors() || len(providerResult.ReturnSourceDeclarations) != 1 {
		t.Fatalf("provider did not retain its original method promise: %s", diagnosticsSummary(providerBag))
	}
	original := providerResult.ReturnSourceDeclarations[0]
	exports["provider"] = symbols.CollectExports(provider, *providerSyms, "provider")
	consumer, consumerFile, consumerSyms, consumerBag := parse("consumer", `import provider::Foo;
contract C<T> { fn choose(self: T, @return_source left: &string, right: &string) -> &string; }
fn require<E: C<E>>(e: E) {}
fn demo(e: Foo) { require(e); }`)
	result := Check(context.Background(), consumer, consumerFile, Options{Symbols: consumerSyms, Types: typesIn, Exports: exports, Reporter: &diag.BagReporter{Bag: consumerBag}, ModulePath: stringsIn.Intern("consumer")})
	if consumerBag.HasErrors() {
		t.Fatalf("consumer direct bound failed ordinary checking: %s", diagnosticsSummary(consumerBag))
	}
	facts := result.ReturnSourceConformances()
	if len(facts) != 1 || len(result.InstantiationGraph.DeferredCallables()) != 0 {
		t.Fatalf("imported direct bound depended on deferred method calls: facts=%d deferred=%d", len(facts), len(result.InstantiationGraph.DeferredCallables()))
	}
	fact := facts[0]
	require := lookupSymbolByName(consumerSyms, stringsIn.Intern("require"))
	if !slices.ContainsFunc(result.InstantiationGraph.Roots(), func(root InstantiationRoot) bool { return root.Template == require }) {
		t.Fatal("imported zero deferred count has no actual concrete require instantiation")
	}
	if fact.Kind != ReturnSourceConformanceSelected || fact.Actual.Declaration != original.Syntax.Span() ||
		fact.Actual.Declaration.File == consumer.Files.Get(consumerFile).Span.File || !fact.Actual.Sources.Equal(original.Syntax.Sources()) ||
		!slices.Equal(fact.Requirement.Sources.Slots(), []uint32{1}) || !slices.Equal(fact.Actual.Sources.Slots(), []uint32{1, 2}) {
		t.Fatal("imported actual was reconstructed in caller scope or lost its wider source promise")
	}
	t.Logf("completed original provider method -> CollectExports -> consumer concrete bound with zero deferred calls: actual=%v requirement=%v", fact.Actual.Declaration, fact.Requirement.Member)
}
