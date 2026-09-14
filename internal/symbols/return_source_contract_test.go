package symbols

import (
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/types"
)

func TestContractReturnSourcePreservation(t *testing.T) {
	for _, name := range []string{"syntax", "add_method", "clone"} {
		t.Run(name, func(t *testing.T) {
			builder, _, bag := parseSnippet(t, `contract C<T> {
    fn choose(self: T, @return_source a: &string, @return_source() b: &string) -> &string;
}`)
			if bag.HasErrors() {
				t.Fatalf("contract source parse failed: %v", bag.Items())
			}
			fn := builder.Items.ContractFn(ast.ContractFnID(1))
			if fn == nil {
				t.Fatal("source has no contract member")
			}
			syntax := ContractReturnSourceSyntax(builder, fn)
			method := ContractMethod{Name: fn.Name, Span: fn.Span, Params: []types.TypeID{1, 2, 3}, Result: 2, ReturnSourceSyntax: syntax, ReturnSourceOwner: SymbolID(7)}
			spec := NewContractSpec()
			spec.AddMethod(&method)
			switch name {
			case "add_method":
				method.Params[0] = types.NoTypeID
				method.ReturnSourceSyntax = ReturnSourceSyntax{}
				syntax = spec.Methods[fn.Name][0].ReturnSourceSyntax
				if spec.Methods[fn.Name][0].Params[0] == types.NoTypeID || spec.Methods[fn.Name][0].ReturnSourceOwner != SymbolID(7) {
					t.Fatal("AddMethod aliased mutable parameters")
				}
			case "clone":
				copied := CloneContractSpec(spec)
				spec.Methods[fn.Name][0].ReturnSourceSyntax = ReturnSourceSyntax{}
				spec.Methods[fn.Name][0].Params[0] = types.NoTypeID
				syntax = copied.Methods[fn.Name][0].ReturnSourceSyntax
				if copied.Methods[fn.Name][0].Params[0] == types.NoTypeID || copied.Methods[fn.Name][0].ReturnSourceOwner != SymbolID(7) {
					t.Fatal("CloneContractSpec aliased mutable parameters")
				}
			}
			if syntax.Span() != fn.Span || syntax.Result() != fn.ReturnType || len(syntax.Params()) != 3 ||
				syntax.Sources().IsAllInputs() || !slices.Equal(syntax.Sources().Slots(), []uint32{1, 2}) {
				t.Fatalf("lost original member identity, roots or source slots: %+v", syntax)
			}
			params, markers := syntax.Params(), syntax.Markers()
			if len(markers) != 2 || markers[0].ArgumentCount != 0 || markers[1].ArgumentCount != 0 || markers[0].Span == markers[1].Span {
				t.Fatal("lost distinct original zero-arity marker locations")
			}
			params[0], markers[0].Slot = ast.NoTypeID, 99
			if syntax.Params()[0] == ast.NoTypeID || !slices.Equal(syntax.Sources().Slots(), []uint32{1, 2}) {
				t.Fatal("contract syntax exposed mutable storage")
			}
		})
	}
}
