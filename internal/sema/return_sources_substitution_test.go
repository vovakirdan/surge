package sema

import (
	"slices"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestReturnSourcesSurviveSemanticSubstitution(t *testing.T) {
	for _, route := range []string{"imported", "instantiation"} {
		t.Run(route, func(t *testing.T) {
			in := types.NewInterner()
			in.Strings = source.NewInterner()
			param := in.RegisterTypeParam(in.Strings.Intern("T"), 17, 0, false, types.NoTypeID)
			ref := in.Intern(types.MakeReference(param, false))
			concrete := in.Intern(types.MakeReference(in.Builtins().Int64, false))
			for _, sources := range []types.ReturnSources{{}, types.ExplicitReturnSources(), types.ExplicitReturnSources(1)} {
				inner := in.RegisterFnWithReturnSources([]types.TypeID{ref, ref}, ref, sources)
				original := in.RegisterFnWithReturnSources([]types.TypeID{inner}, inner, types.ExplicitReturnSources())
				var got types.TypeID
				if route == "imported" {
					checker := typeChecker{types: in}
					got = checker.substituteImportedType(original, []types.TypeID{in.Builtins().Int64})
				} else {
					subst, err := newInstantiationSubstitution(in, []InstantiationParamBinding{{
						Owner: symbols.SymbolID(17), Param: param, ParamIndex: 0, ArgIndex: 0,
					}}, []types.TypeID{in.Builtins().Int64})
					if err != nil {
						t.Fatal(err)
					}
					got, err = subst.typeID(original)
					if err != nil {
						t.Fatal(err)
					}
				}
				outer, ok := in.FnInfo(got)
				if !ok || got == original || len(outer.Params) != 1 || outer.Params[0] != outer.Result ||
					!outer.ReturnSources().Equal(types.ExplicitReturnSources()) {
					t.Fatal("outer callback did not preserve its explicit empty contract")
				}
				after, ok := in.FnInfo(outer.Result)
				if !ok || !slices.Equal(after.Params, []types.TypeID{concrete, concrete}) || after.Result != concrete ||
					!after.ReturnSources().Equal(sources) {
					t.Fatal("substituted callback lost its declared sources or concrete signature")
				}
				before, _ := in.FnInfo(inner)
				if before.Result != ref || !before.ReturnSources().Equal(sources) {
					t.Fatal("substitution mutated the original callback")
				}
			}
		})
	}
}
