package sema

import (
	"strings"
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestInstantiationIdentityBindsBuiltinNominalsWithoutRootSymbolTypes(t *testing.T) {
	strs := source.NewInterner()
	in := types.NewInterner()
	in.Strings = strs
	table := symbols.NewTable(symbols.Hints{}, strs)

	arrayOwner := newBuiltinNominalTestSymbol(table, "Array")
	fixedOwner := newBuiltinNominalTestSymbol(table, "ArrayFixed")
	mapOwner := newBuiltinNominalTestSymbol(table, "Map")
	arrayType, arrayParam := in.EnsureArrayNominal(strs.Intern("Array"), strs.Intern("T"), source.Span{}, uint32(arrayOwner))
	fixedType, fixedParams := in.EnsureArrayFixedNominal(strs.Intern("ArrayFixed"), strs.Intern("T"), strs.Intern("N"), source.Span{}, uint32(fixedOwner), in.Builtins().Int)
	mapType, mapParams := in.EnsureMapNominal(strs.Intern("Map"), strs.Intern("K"), strs.Intern("V"), source.Span{}, uint32(mapOwner))

	for _, owner := range []symbols.SymbolID{arrayOwner, fixedOwner, mapOwner} {
		if got := table.Symbols.Get(owner).Type; got != types.NoTypeID {
			t.Fatalf("test requires an untyped root prelude symbol, got type#%d", got)
		}
	}
	identity, err := NewInstantiationKeyContext(in, &symbols.Result{Table: table}, nil)
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}

	for _, test := range []struct {
		name   string
		typeID types.TypeID
		params []types.TypeID
	}{
		{name: "Array", typeID: arrayType, params: []types.TypeID{arrayParam}},
		{name: "ArrayFixed", typeID: fixedType, params: fixedParams[:]},
		{name: "Map", typeID: mapType, params: mapParams[:]},
	} {
		t.Run(test.name, func(t *testing.T) {
			key, keyErr := identity.Types.TypeKey(test.typeID)
			if keyErr != nil {
				t.Fatalf("builtin type key: %v", keyErr)
			}
			if !strings.Contains(key, "<builtin>") {
				t.Fatalf("builtin type key lacks stable namespace: %q", key)
			}
			for _, param := range test.params {
				if _, paramErr := identity.Types.TypeKey(param); paramErr != nil {
					t.Fatalf("builtin parameter key: %v", paramErr)
				}
			}
		})
	}
}

func TestInstantiationIdentityUsesExactBuiltinParamsDespiteForeignRawOwner(t *testing.T) {
	strs := source.NewInterner()
	in := types.NewInterner()
	in.Strings = strs
	table := symbols.NewTable(symbols.Hints{}, strs)
	wrongOwner := table.Symbols.New(&symbols.Symbol{Name: strs.Intern("unrelated"), Kind: symbols.SymbolFunction, ModulePath: "core"})
	_ = newBuiltinNominalTestSymbol(table, "ArrayFixed")
	_, params := in.EnsureArrayFixedNominal(strs.Intern("ArrayFixed"), strs.Intern("T"), strs.Intern("N"), source.Span{}, uint32(wrongOwner), in.Builtins().Int)

	identity, err := NewInstantiationKeyContext(in, &symbols.Result{Table: table}, nil)
	if err != nil {
		t.Fatalf("exact builtin parameter identity rejected a harmless raw-owner collision: %v", err)
	}
	for _, param := range params {
		key, keyErr := identity.Types.TypeKey(param)
		if keyErr != nil || !strings.Contains(key, "<builtin>") {
			t.Fatalf("builtin parameter key = %q, err=%v", key, keyErr)
		}
	}
}

func TestInstantiationIdentityUsesTagSymbolForSyntheticUnion(t *testing.T) {
	strs := source.NewInterner()
	in := types.NewInterner()
	in.Strings = strs
	table := symbols.NewTable(symbols.Hints{}, strs)
	name := strs.Intern("Some")
	paramName := strs.Intern("T")
	tagID := table.Symbols.New(&symbols.Symbol{
		Name:       name,
		Kind:       symbols.SymbolTag,
		ModulePath: "core",
		TypeParams: []source.StringID{paramName},
	})
	param := in.RegisterTypeParam(paramName, uint32(tagID), 0, false, types.NoTypeID)
	union := in.RegisterUnionInstance(name, source.Span{}, []types.TypeID{param})

	identity, err := NewInstantiationKeyContext(in, &symbols.Result{Table: table}, nil)
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	key, err := identity.Types.TypeKey(union)
	if err != nil {
		t.Fatalf("synthetic tag union key: %v", err)
	}
	if !strings.Contains(key, "core") || !strings.Contains(key, "Some") {
		t.Fatalf("synthetic tag union lacks module identity: %q", key)
	}
}

func newBuiltinNominalTestSymbol(table *symbols.Table, name string) symbols.SymbolID {
	return table.Symbols.New(&symbols.Symbol{
		Name:  table.Strings.Intern(name),
		Kind:  symbols.SymbolType,
		Flags: symbols.SymbolFlagBuiltin,
	})
}
