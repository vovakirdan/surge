package sema

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

// This tests ordinary shape compatibility only. The opposite origin relation
// must still be refused by finalization; accepting its shape is not publication.
func TestReturnOriginCallableAliasOrdinaryShape(t *testing.T) {
	const aliases = `type FirstRef = fn(@return_source &string, &string) -> &string;
type AnyRef = fn(&string, &string) -> &string;
`
	for _, tc := range []struct{ name, src string }{
		{"narrow_to_wide", aliases + `fn probe(f: FirstRef) -> nothing { let wide: AnyRef = f; }
`},
		{"wide_to_narrow", aliases + `fn probe(f: AnyRef) -> nothing { let narrow: FirstRef = f; }
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_ALIAS_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			builder, file, parseBag := parseSnippet(t, tc.src)
			logReturnOriginAliasShape(t, map[string]any{"case": tc.name, "parse_diagnostics": parseBag.Items()})
			if parseBag.HasErrors() {
				t.Fatal("alias source did not parse")
			}
			bag := diag.NewBag(64)
			resolved := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}})
			checked := Check(t.Context(), builder, file, Options{Symbols: &resolved, Reporter: &diag.BagReporter{Bag: bag}})
			var aliases []map[string]any
			var infos []*types.FnInfo
			var aliasIDs, targetIDs []types.TypeID
			var bindings []map[string]any
			for id, typed := range checked.BindingTypes {
				if sym := resolved.Table.Symbols.Get(id); sym != nil {
					name, _ := builder.StringsInterner.Lookup(sym.Name)
					bindings = append(bindings, map[string]any{"symbol": id, "name": name, "type": typed,
						"kind": sym.Kind, "declaration": sym.Decl, "span": sym.Span})
				}
			}
			for _, item := range builder.Files.Get(file).Items {
				if _, ok := builder.Items.Type(item); !ok {
					continue
				}
				for _, id := range resolved.ItemSymbols[item] {
					sym := resolved.Table.Symbols.Get(id)
					if sym == nil || checked.TypeInterner == nil {
						continue
					}
					alias, ok := checked.TypeInterner.AliasInfo(sym.Type)
					if !ok || alias == nil {
						continue
					}
					info, typed := checked.TypeInterner.FnInfo(alias.Target)
					name, _ := builder.StringsInterner.Lookup(sym.Name)
					row := map[string]any{"item": item, "symbol": id, "name": name, "alias_type": sym.Type,
						"alias": alias, "target_is_function": typed, "target_function": info}
					if info != nil {
						row["all_inputs"], row["slots"] = info.ReturnSources().IsAllInputs(), info.ReturnSources().Slots()
					}
					aliases = append(aliases, row)
					infos, aliasIDs, targetIDs = append(infos, info), append(aliasIDs, sym.Type), append(targetIDs, alias.Target)
				}
			}
			logReturnOriginAliasShape(t, map[string]any{"case": tc.name, "source": tc.src,
				"diagnostics": bag.Items(), "aliases": aliases, "expr_types": checked.ExprTypes,
				"binding_types": checked.BindingTypes, "binding_symbols": bindings, "expr_symbols": resolved.ExprSymbols,
				"declarations": checked.ReturnSourceDeclarations, "instantiations": checked.ReturnSourceInstantiations})
			if len(infos) != 2 || infos[0] == nil || infos[1] == nil || aliasIDs[0] == aliasIDs[1] || targetIDs[0] == targetIDs[1] {
				t.Fatal("source did not produce two distinct aliases and callable contracts")
			}
			if !slices.Equal(infos[0].Params, infos[1].Params) || infos[0].Result != infos[1].Result ||
				infos[0].ReturnSources().IsAllInputs() || !slices.Equal(infos[0].ReturnSources().Slots(), []uint32{0}) ||
				!infos[1].ReturnSources().IsAllInputs() {
				t.Fatal("fixture differs in ordinary shape or lost its exact source promises")
			}
			if bag.HasErrors() {
				t.Fatal("ordinary alias shape rejected a return-source-only difference; full diagnostics logged above")
			}
		})
	}
}

func logReturnOriginAliasShape(t *testing.T, evidence any) {
	t.Helper()
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RETURN_ORIGIN_ALIAS_EVIDENCE=%s", data)
}
