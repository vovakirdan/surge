package mir

import (
	"testing"

	"surge/internal/hir"
	"surge/internal/mono"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestEntrypointCountedCopyFlags(t *testing.T) {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	builtins := in.Builtins()
	alias := in.RegisterAlias(in.Strings.Intern("Count"), source.Span{})
	in.SetAliasTarget(alias, builtins.Int)
	b := &surgeStartBuilder{typesIn: in}
	for _, row := range []struct {
		name string
		ty   types.TypeID
		want LocalFlags
	}{
		{"int", builtins.Int, LocalFlagCopy | LocalFlagOwnsHeap},
		{"uint", builtins.Uint, LocalFlagCopy | LocalFlagOwnsHeap},
		{"float", builtins.Float, LocalFlagCopy | LocalFlagOwnsHeap},
		{"alias", alias, LocalFlagCopy | LocalFlagOwnsHeap},
		{"int64", builtins.Int64, LocalFlagCopy},
		{"uint64", builtins.Uint64, LocalFlagCopy},
		{"bool", builtins.Bool, LocalFlagCopy},
		{"reference", in.Intern(types.MakeReference(builtins.Int, false)), LocalFlagCopy},
		{"pointer", in.Intern(types.Type{Kind: types.KindPointer, Elem: builtins.Int}), LocalFlagCopy},
		{"missing", types.NoTypeID, 0},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := b.localFlags(row.ty); got != row.want {
				t.Fatalf("entrypoint flags=%v, want %v", got, row.want)
			}
		})
	}
}

func TestEntrypointGeneratedNumericLocalsOwnHeap(t *testing.T) {
	in := types.NewInterner()
	key := mono.MonoKey{Sym: symbols.SymbolID(1)}
	mf := &mono.MonoFunc{Key: key, Func: &hir.Func{
		Name: "main", SymbolID: key.Sym, Result: in.Builtins().Int, Flags: hir.FuncEntrypoint,
	}}
	mm := &mono.MonoModule{Source: &hir.Module{TypeInterner: in}, Funcs: map[mono.MonoKey]*mono.MonoFunc{key: mf}}
	t.Run("code", func(t *testing.T) {
		f, err := BuildSurgeStart(mm, nil, in, 1, nil, nil, nil, nil)
		if err != nil || f == nil {
			t.Fatalf("build entrypoint: function=%v err=%v", f, err)
		}
		requireEntrypointNumericLocal(t, f, "code", in.Builtins().Int)
	})
	t.Run("argv-and-failure", func(t *testing.T) {
		// Missing Erring metadata selects the real startup failure path after
		// argv_len has been emitted, so both generated helpers are observed.
		args := &mono.MonoFunc{Func: &hir.Func{Params: []hir.Param{{Name: "n", Type: in.Builtins().Int}}}}
		b := &surgeStartBuilder{typesIn: in, entryMF: args, mm: mm, f: &Func{Name: "argv_failure"}}
		b.cur = b.newBlock()
		b.prepareArgsArgv()
		requireEntrypointNumericLocal(t, b.f, "argv_len", in.Builtins().Uint)
		requireEntrypointNumericLocal(t, b.f, "exit_code", in.Builtins().Int)
	})
}

func requireEntrypointNumericLocal(t *testing.T, f *Func, name string, ty types.TypeID) {
	t.Helper()
	count := 0
	for _, local := range f.Locals {
		if local.Name == name {
			count++
			if local.Type != ty || local.Flags != LocalFlagCopy|LocalFlagOwnsHeap {
				t.Fatalf("%s: type=%v flags=%v", name, local.Type, local.Flags)
			}
		}
	}
	if count != 1 {
		t.Fatalf("%s local census=%d, want1", name, count)
	}
}
