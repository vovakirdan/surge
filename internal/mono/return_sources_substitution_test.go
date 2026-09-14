package mono

import (
	"slices"
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

func TestReturnSourcesSurviveMonoSubstitution(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	param := in.RegisterTypeParam(in.Strings.Intern("T"), 17, 0, false, types.NoTypeID)
	ref := in.Intern(types.MakeReference(param, false))
	concrete := in.Intern(types.MakeReference(in.Builtins().Int64, false))
	for _, sources := range []types.ReturnSources{{}, types.ExplicitReturnSources(), types.ExplicitReturnSources(1)} {
		inner := in.RegisterFnWithReturnSources([]types.TypeID{ref, ref}, ref, sources)
		original := in.RegisterFnWithReturnSources([]types.TypeID{inner}, inner, types.ExplicitReturnSources())
		subst := Subst{Types: in, ExactArgs: map[types.TypeID]types.TypeID{param: in.Builtins().Int64}}
		got := subst.Type(original)
		outer, ok := in.FnInfo(got)
		if !ok || got == original || len(outer.Params) != 1 || outer.Params[0] != outer.Result ||
			!outer.ReturnSources().Equal(types.ExplicitReturnSources()) {
			t.Fatal("outer callback did not preserve its explicit empty contract")
		}
		after, ok := in.FnInfo(outer.Result)
		if !ok || !slices.Equal(after.Params, []types.TypeID{concrete, concrete}) || after.Result != concrete ||
			!after.ReturnSources().Equal(sources) {
			t.Fatal("mono substitution lost callback sources or its concrete signature")
		}
		before, _ := in.FnInfo(inner)
		if before.Result != ref || !before.ReturnSources().Equal(sources) {
			t.Fatal("mono substitution mutated the original callback")
		}
	}
}
