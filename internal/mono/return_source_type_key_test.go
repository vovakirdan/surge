package mono

import (
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestReturnSourceMonoTypeKeys(t *testing.T) {
	for _, name := range []string{"legacy", "explicit", "nested", "empty"} {
		t.Run(name, func(t *testing.T) {
			in := types.NewInterner()
			ref := in.Intern(types.MakeReference(in.Builtins().String, false))
			sources := types.ExplicitReturnSources(0)
			if name == "legacy" {
				sources = types.ReturnSources{}
			}
			id := in.RegisterFnWithReturnSources([]types.TypeID{ref}, ref, sources)
			want := "fn@return_source{0}(&string) -> &string"
			switch name {
			case "legacy":
				want = "fn(&string) -> &string"
			case "nested":
				id = in.RegisterFn([]types.TypeID{id, in.Builtins().Int}, id)
				want = "fn(fn@return_source{0}(&string) -> &string, int) -> fn@return_source{0}(&string) -> &string"
			case "empty":
				id = in.RegisterFnWithReturnSources(nil, in.Builtins().String, types.ExplicitReturnSources())
				want = "fn@return_source{}() -> string"
			}
			got := formatType(in, source.NewInterner(), id, 0)
			if got != want {
				t.Fatalf("mono key = %q, want %q", got, want)
			}
			_, _, parsedSources, ok := symbols.ParseFunctionTypeKey(symbols.TypeKey(got))
			info, _ := in.FnInfo(id)
			if !ok || !parsedSources.Equal(info.ReturnSources()) {
				t.Fatal("mono key lost its readable machine contract")
			}
		})
	}
}
