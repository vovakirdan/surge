package symbols

import (
	"testing"

	"surge/internal/source"
)

func TestReturnSourceSyntheticImportIdentity(t *testing.T) {
	for _, name := range []string{"distinct_sources", "same_sources_reused", "signature_preserved"} {
		t.Run(name, func(t *testing.T) {
			first := returnSourceKeyTestSignature(t, "fn f(@return_source a: &string, b: &string) -> &string;")
			second := returnSourceKeyTestSignature(t, "fn f(a: &string, @return_source b: &string) -> &string;")
			// The importer owns a different AST. It must use exported metadata.
			importer, _, bag := parseSnippet(t, "fn unrelated() {}")
			if bag.HasErrors() {
				t.Fatal(bag.Items())
			}
			result := &Result{Table: NewTable(Hints{}, importer.StringsInterner)}
			resolver := fileResolver{builder: importer, result: result}
			exported := &ExportedSymbol{Kind: SymbolFunction, Signature: first, Flags: SymbolFlagPublic}
			left := resolver.syntheticSymbolForExport("remote", "f", exported, source.Span{})
			if !left.IsValid() {
				t.Fatal("no imported symbol")
			}
			if name == "same_sources_reused" {
				second = first
			}
			exported.Signature = second
			right := resolver.syntheticSymbolForExport("remote", "f", exported, source.Span{})
			if (left == right) != (name == "same_sources_reused") {
				t.Fatalf("wrong synthetic identity: %d, %d", left, right)
			}
			if name == "signature_preserved" {
				got := result.Table.Symbols.Get(right)
				if got.Signature != second || !got.Signature.ReturnSourceSyntax.Sources().Equal(second.ReturnSourceSyntax.Sources()) || got.ModulePath != "remote" {
					t.Fatal("import rebuilt or lost original signature provenance")
				}
			}
		})
	}
}
