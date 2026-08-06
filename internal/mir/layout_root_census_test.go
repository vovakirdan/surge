package mir

import (
	"testing"

	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// censusBaselineLayoutHash is the registry hash the census fixture produced
// before roots carried roles. Roles are metadata about the walk, so neither the
// roots nor their layouts may move.
const censusBaselineLayoutHash = "8b9880ef70ce6db68d8ee9a6d203055a99567d5e7f203f38ae626240bd3ae201"

// censusFixture builds one module covering every shape the root walk
// distinguishes: maps (one nested inside another map's value), dynamic and
// fixed arrays, tag payloads, generic instantiations and a function body. Its
// first global is a plain value that a later map also uses as its key, so every
// walk over it exercises value-before-key discovery.
type censusFixture struct {
	types *types.Interner
	mod   *Module

	sharedKey  types.TypeID
	mapValue   types.TypeID
	innerKey   types.TypeID
	innerValue types.TypeID
	plainOnly  types.TypeID
	arrayElem  types.TypeID
	tagPayload types.TypeID
	innerMap   types.TypeID
	outerMap   types.TypeID
	nestedMap  types.TypeID
	array      types.TypeID
	fixedArray types.TypeID
	fixedLen   types.TypeID
	phantom    types.TypeID
	generic    types.TypeID
	tagged     types.TypeID
}

func newCensusFixture(t *testing.T) *censusFixture {
	t.Helper()
	typesIn := newLayoutTestInterner()
	b := typesIn.Builtins()
	strct := func(name string, field types.TypeID) types.TypeID {
		id := typesIn.RegisterStruct(typesIn.Strings.Intern(name), source.Span{})
		typesIn.SetStructFields(id, []types.StructField{{Type: field}})
		return id
	}

	f := &censusFixture{types: typesIn}
	f.sharedKey = strct("SharedKey", b.Uint64)
	f.mapValue = strct("MapValue", b.Bool)
	f.innerKey = strct("InnerKey", b.Int32)
	f.innerValue = strct("InnerValue", b.Float64)
	f.plainOnly = strct("PlainOnly", b.Uint16)
	f.arrayElem = strct("ArrayElem", b.Uint8)
	f.tagPayload = strct("TagPayload", b.Int64)
	f.tagged = strct("Tagged", b.Uint32)

	mapName := typesIn.Strings.Intern("Map")
	typesIn.EnsureMapNominal(mapName, typesIn.Strings.Intern("K"), typesIn.Strings.Intern("V"), source.Span{}, 2)
	f.innerMap = typesIn.RegisterStructInstance(mapName, source.Span{}, []types.TypeID{f.innerKey, f.innerValue})
	f.outerMap = typesIn.RegisterStructInstance(mapName, source.Span{}, []types.TypeID{f.sharedKey, f.mapValue})
	f.nestedMap = typesIn.RegisterStructInstance(mapName, source.Span{}, []types.TypeID{f.sharedKey, f.innerMap})

	arrayName := typesIn.Strings.Intern("Array")
	typesIn.EnsureArrayNominal(arrayName, typesIn.Strings.Intern("E"), source.Span{}, 3)
	f.array = typesIn.RegisterStructInstance(arrayName, source.Span{}, []types.TypeID{f.arrayElem})

	fixedName := typesIn.Strings.Intern("ArrayFixed")
	typesIn.EnsureArrayFixedNominal(
		fixedName,
		typesIn.Strings.Intern("T"),
		typesIn.Strings.Intern("N"),
		source.Span{},
		4,
		b.Uint32,
	)
	f.fixedLen = typesIn.Intern(types.MakeConstUint(4))
	f.fixedArray = typesIn.RegisterStructInstance(fixedName, source.Span{}, []types.TypeID{b.Int32, f.fixedLen})

	f.generic = typesIn.RegisterTypeParam(typesIn.Strings.Intern("T"), 5, 0, false, types.NoTypeID)
	f.phantom = typesIn.RegisterStructInstance(typesIn.Strings.Intern("Phantom"), source.Span{}, []types.TypeID{f.generic})
	typesIn.SetStructFields(f.phantom, []types.StructField{{Type: b.Bool}})

	f.mod = &Module{
		Funcs: map[FuncID]*Func{
			0: {
				ID:     0,
				Name:   "census",
				Result: f.mapValue,
				Locals: []Local{{Type: f.outerMap}, {Type: b.Bool}},
				Blocks: []Block{{
					ID: 0,
					Instrs: []Instr{{
						Kind: InstrAssign,
						Assign: AssignInstr{
							Dst: Place{Kind: PlaceLocal, Local: 1},
							Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandCopy, Type: b.Bool}},
						},
					}},
					Term: Terminator{Kind: TermReturn},
				}},
			},
		},
		Globals: []Global{
			{Type: f.sharedKey},
			{Type: f.outerMap},
			{Type: f.nestedMap},
			{Type: f.array},
			{Type: f.fixedArray},
			{Type: f.plainOnly},
			{Type: f.phantom},
		},
		Meta: &ModuleMeta{
			TagLayouts: map[types.TypeID][]TagCaseMeta{
				f.tagged: {{TagName: "Some", PayloadTypes: []types.TypeID{f.tagPayload}}},
			},
			FuncTypeArgs: map[symbols.SymbolID][]types.TypeID{
				symbols.SymbolID(1): {f.innerMap, f.fixedLen},
			},
		},
	}
	return f
}

// baselineOrder is the exact root order this module produced before roots
// carried roles. It is a captured expectation: a walk change that moves an
// entry is a regression, not a reason to re-record the list.
func (f *censusFixture) baselineOrder() []types.TypeID {
	b := f.types.Builtins()
	return []types.TypeID{
		f.sharedKey, b.Uint64,
		f.outerMap, f.mapValue, b.Bool,
		f.nestedMap, f.innerMap, f.innerKey, b.Int32, f.innerValue, b.Float64,
		f.array, f.arrayElem, b.Uint8,
		f.fixedArray,
		f.plainOnly, b.Uint16,
		f.phantom,
		f.tagged, b.Uint32, f.tagPayload, b.Int64,
	}
}

func (f *censusFixture) collect(t *testing.T) *RootCensus {
	t.Helper()
	census, err := collectOperationRoots(f.mod, f.types, layout.New(layout.X86_64LinuxGNU(), f.types))
	if err != nil {
		t.Fatalf("collectOperationRoots: %v", err)
	}
	return census
}

func labelAll(typesIn *types.Interner, ids []types.TypeID) []string {
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		labels = append(labels, types.Label(typesIn, id))
	}
	return labels
}

func assertSameRoots(t *testing.T, typesIn *types.Interner, what string, got, want []types.TypeID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, labelAll(typesIn, got), labelAll(typesIn, want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %s, want %s (full: %v)",
				what, i, types.Label(typesIn, got[i]), types.Label(typesIn, want[i]), labelAll(typesIn, got))
		}
	}
}

// TestOperationRootCensusValuesMatchThePreRoleWalk is the compatibility spine:
// adding roles must not add, drop, or reorder a single physical root, and the
// finalized registry must hash to what it hashed before.
func TestOperationRootCensusValuesMatchThePreRoleWalk(t *testing.T) {
	f := newCensusFixture(t)
	census := f.collect(t)
	assertSameRoots(t, f.types, "census.Values()", census.Values(), f.baselineOrder())

	mutated := census.Values()
	mutated[0] = types.NoTypeID
	assertSameRoots(t, f.types, "census.Values() after caller mutation", census.Values(), f.baselineOrder())

	if err := FinalizeModuleMeta(f.mod, f.types, layout.X86_64LinuxGNU(), nil); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	hash, err := f.mod.Meta.Layouts.Hash()
	if err != nil {
		t.Fatalf("registry hash: %v", err)
	}
	if hash != censusBaselineLayoutHash {
		t.Fatalf("registry hash = %s, want the pre-role hash %s", hash, censusBaselineLayoutHash)
	}
}

// TestOperationRootCensusSeparatesMapKeysFromValues covers the map contract:
// K is both a key and a value, V is only a value, and nesting reaches the inner
// map's key too.
func TestOperationRootCensusSeparatesMapKeysFromValues(t *testing.T) {
	f := newCensusFixture(t)
	census := f.collect(t)

	assertSameRoots(t, f.types, "census.Keys()", census.Keys(), []types.TypeID{f.sharedKey, f.innerKey})

	for _, id := range census.Keys() {
		if census.Role(id)&RootValue == 0 {
			t.Errorf("key %s is missing RootValue", types.Label(f.types, id))
		}
	}
	for _, id := range census.Values() {
		if census.Role(id)&RootValue == 0 {
			t.Errorf("root %s is missing RootValue", types.Label(f.types, id))
		}
	}

	neverKeys := []types.TypeID{
		f.mapValue, f.innerValue, f.plainOnly, f.arrayElem, f.tagPayload,
		f.outerMap, f.nestedMap, f.innerMap, f.array, f.fixedArray,
	}
	for _, id := range neverKeys {
		if census.Role(id)&RootKey != 0 {
			t.Errorf("%s is not a map key but carries RootKey", types.Label(f.types, id))
		}
	}
}

// TestOperationRootCensusAccumulatesBothRolesForOneType pins that a type used
// both as a map key and as an ordinary value carries both roles and is listed
// by both accessors.
func TestOperationRootCensusAccumulatesBothRolesForOneType(t *testing.T) {
	f := newCensusFixture(t)
	census := f.collect(t)

	if got, want := census.Role(f.sharedKey), RootValue|RootKey; got != want {
		t.Fatalf("SharedKey role = %d, want %d", got, want)
	}
	if census.Role(f.innerKey)&RootKey == 0 {
		t.Error("InnerKey, reached only through a nested map, is missing RootKey")
	}

	inValues := false
	for _, id := range census.Values() {
		if id == f.sharedKey {
			inValues = true
		}
	}
	inKeys := false
	for _, id := range census.Keys() {
		if id == f.sharedKey {
			inKeys = true
		}
	}
	if !inValues || !inKeys {
		t.Fatalf("SharedKey in Values()=%v, in Keys()=%v, want both", inValues, inKeys)
	}
}

// TestOperationRootCensusRoleSurvivesDiscoveryOrder is the order trap: the
// canonical-seen guard skips re-walking, so a role recorded below it would be
// lost whenever the key type happened to be walked as a plain value first.
func TestOperationRootCensusRoleSurvivesDiscoveryOrder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		valueQ1st bool
	}{
		{name: "key type walked as a plain value first", valueQ1st: true},
		{name: "map walked first", valueQ1st: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCensusFixture(t)
			globals := []Global{{Type: f.sharedKey}, {Type: f.outerMap}}
			if !tc.valueQ1st {
				globals = []Global{{Type: f.outerMap}, {Type: f.sharedKey}}
			}
			f.mod = &Module{Funcs: map[FuncID]*Func{}, Globals: globals, Meta: &ModuleMeta{}}

			census := f.collect(t)
			if got, want := census.Role(f.sharedKey), RootValue|RootKey; got != want {
				t.Fatalf("SharedKey role = %d, want %d", got, want)
			}
			assertSameRoots(t, f.types, "census.Keys()", census.Keys(), []types.TypeID{f.sharedKey})
		})
	}
}

// TestOperationRootCensusIsDeterministic walks a module whose tag and function
// type-argument maps hold several entries, so unsorted iteration would show up.
func TestOperationRootCensusIsDeterministic(t *testing.T) {
	build := func() *censusFixture {
		f := newCensusFixture(t)
		f.mod.Meta.TagLayouts = map[types.TypeID][]TagCaseMeta{
			f.tagged:    {{TagName: "Some", PayloadTypes: []types.TypeID{f.tagPayload}}},
			f.plainOnly: {{TagName: "Other", PayloadTypes: []types.TypeID{f.innerMap}}},
			f.mapValue:  {{TagName: "Third", PayloadTypes: []types.TypeID{f.nestedMap, f.array}}},
		}
		f.mod.Meta.FuncTypeArgs = map[symbols.SymbolID][]types.TypeID{
			symbols.SymbolID(9): {f.outerMap},
			symbols.SymbolID(3): {f.fixedArray, f.fixedLen},
			symbols.SymbolID(7): {f.innerMap},
		}
		return f
	}

	first := build()
	firstCensus := first.collect(t)
	for run := range 4 {
		next := build()
		nextCensus := next.collect(t)
		assertSameRoots(t, first.types, "census.Values()", nextCensus.Values(), firstCensus.Values())
		assertSameRoots(t, first.types, "census.Keys()", nextCensus.Keys(), firstCensus.Keys())
		for _, id := range firstCensus.Values() {
			if got, want := nextCensus.Role(id), firstCensus.Role(id); got != want {
				t.Fatalf("run %d: role of %s = %d, want %d", run, types.Label(first.types, id), got, want)
			}
		}
	}
}
