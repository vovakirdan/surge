package mir

import (
	"reflect"
	"testing"

	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/types"
)

// callLayoutCorpus is one finalized module whose globals pin every shape a call
// contract has to classify.
type callLayoutCorpus struct {
	types   *types.Interner
	layouts *layout.Registry
	table   *CallLayoutTable

	i64         types.TypeID
	boolT       types.TypeID
	str         types.TypeID
	point       types.TypeID
	packed      types.TypeID
	overaligned types.TypeID
	empty       types.TypeID
	alignedZST  types.TypeID
	pointArray  types.TypeID
	pair        types.TypeID
	choice      types.TypeID
	pointRef    types.TypeID
	callback    types.TypeID
	pointAlias  types.TypeID
	ownedPoint  types.TypeID
	orphan      types.TypeID
}

func newCallLayoutCorpus(t *testing.T) *callLayoutCorpus {
	t.Helper()
	typesIn := newLayoutTestInterner()
	builtins := typesIn.Builtins()
	c := &callLayoutCorpus{types: typesIn, i64: builtins.Int64, boolT: builtins.Bool, str: builtins.String}

	c.point = typesIn.RegisterStruct(typesIn.Strings.Intern("Point"), source.Span{})
	typesIn.SetStructFields(c.point, []types.StructField{{Type: c.i64}, {Type: c.i64}})

	c.packed = typesIn.RegisterStruct(typesIn.Strings.Intern("Packed"), source.Span{})
	typesIn.SetStructFields(c.packed, []types.StructField{{Type: builtins.Uint8}, {Type: c.i64}})
	typesIn.SetTypeLayoutAttrs(c.packed, types.LayoutAttrs{Packed: true})

	overAlign := uint64(16)
	c.overaligned = typesIn.RegisterStruct(typesIn.Strings.Intern("OverAligned"), source.Span{})
	typesIn.SetStructFields(c.overaligned, []types.StructField{{Type: c.i64}})
	typesIn.SetTypeLayoutAttrs(c.overaligned, types.LayoutAttrs{AlignOverride: &overAlign})

	c.empty = typesIn.RegisterStruct(typesIn.Strings.Intern("Empty"), source.Span{})
	typesIn.SetStructFields(c.empty, nil)

	zstAlign := uint64(16)
	c.alignedZST = typesIn.RegisterStruct(typesIn.Strings.Intern("AlignedEmpty"), source.Span{})
	typesIn.SetStructFields(c.alignedZST, nil)
	typesIn.SetTypeLayoutAttrs(c.alignedZST, types.LayoutAttrs{AlignOverride: &zstAlign})

	c.pointArray = typesIn.Intern(types.MakeArray(c.point, 3))
	c.pair = typesIn.RegisterTuple([]types.TypeID{c.i64, c.boolT})

	c.choice = typesIn.RegisterUnion(typesIn.Strings.Intern("Choice"), source.Span{})
	typesIn.SetUnionMembers(c.choice, []types.UnionMember{
		{Kind: types.UnionMemberNothing},
		{Kind: types.UnionMemberTag, TagName: typesIn.Strings.Intern("Some"), TagArgs: []types.TypeID{c.point}},
	})

	c.pointRef = typesIn.Intern(types.MakeReference(c.point, false))
	c.callback = typesIn.RegisterFn([]types.TypeID{c.i64}, c.i64)
	c.pointAlias = typesIn.RegisterAlias(typesIn.Strings.Intern("Coord"), source.Span{})
	typesIn.SetAliasTarget(c.pointAlias, c.point)
	c.ownedPoint = typesIn.Intern(types.MakeOwn(c.point))

	// Registered but never reachable from the module, so no finalized layout
	// exists for it.
	c.orphan = typesIn.RegisterStruct(typesIn.Strings.Intern("Orphan"), source.Span{})
	typesIn.SetStructFields(c.orphan, []types.StructField{{Type: c.i64}})

	roots := []types.TypeID{
		c.i64, c.boolT, c.str, c.point, c.packed, c.overaligned, c.empty,
		c.alignedZST, c.pointArray, c.pair, c.choice, c.pointRef, c.callback,
		c.pointAlias, c.ownedPoint,
	}
	globals := make([]Global, 0, len(roots))
	for _, id := range roots {
		globals = append(globals, Global{Type: id})
	}
	mod := &Module{Funcs: make(map[FuncID]*Func), Globals: globals, Meta: &ModuleMeta{}}
	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU(), nil); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	c.layouts = mod.Meta.Layouts
	c.table = mod.Meta.CallLayouts
	if c.table == nil {
		t.Fatal("FinalizeModuleMeta published no call layout table")
	}
	return c
}

func (c *callLayoutCorpus) surge(t *testing.T, params []types.TypeID, result types.TypeID) SurgeABI {
	t.Helper()
	l, err := ComputeCallLayoutForSignature(c.types, c.layouts, params, result, ABIDomainSurge)
	if err != nil {
		t.Fatalf("ComputeCallLayoutForSignature: %v", err)
	}
	abi, err := l.Surge()
	if err != nil {
		t.Fatalf("Surge: %v", err)
	}
	return abi
}

func TestCallLayoutClassifiesParameters(t *testing.T) {
	c := newCallLayoutCorpus(t)
	for _, tc := range []struct {
		name string
		id   types.TypeID
		want ParamLayout
	}{
		{name: "scalar", id: c.i64, want: ParamLayout{Class: ParamDirect, Size: 8, Align: 8}},
		{name: "bool", id: c.boolT, want: ParamLayout{Class: ParamDirect, Size: 1, Align: 1}},
		{name: "handle", id: c.str, want: ParamLayout{Class: ParamDirect, Size: 8, Align: 8}},
		{name: "reference", id: c.pointRef, want: ParamLayout{Class: ParamDirect, Size: 8, Align: 8}},
		{name: "function value", id: c.callback, want: ParamLayout{Class: ParamDirect, Size: 8, Align: 8}},
		{name: "struct", id: c.point, want: ParamLayout{Class: ParamByval, Size: 16, Align: 8}},
		{name: "packed struct", id: c.packed, want: ParamLayout{Class: ParamByval, Size: 9, Align: 1}},
		{name: "over-aligned struct", id: c.overaligned, want: ParamLayout{Class: ParamByval, Size: 16, Align: 16}},
		{name: "fixed array", id: c.pointArray, want: ParamLayout{Class: ParamByval, Size: 48, Align: 8}},
		{name: "tuple", id: c.pair, want: ParamLayout{Class: ParamByval, Size: 16, Align: 8}},
		{name: "union", id: c.choice, want: ParamLayout{Class: ParamByval, Size: 24, Align: 8}},
		{name: "zero-sized struct", id: c.empty, want: ParamLayout{Class: ParamElidedZST, Size: 0, Align: 1}},
		{name: "over-aligned zero-sized struct", id: c.alignedZST, want: ParamLayout{Class: ParamElidedZST, Size: 0, Align: 16}},
		{name: "alias of a struct", id: c.pointAlias, want: ParamLayout{Class: ParamByval, Size: 16, Align: 8}},
		{name: "owned struct", id: c.ownedPoint, want: ParamLayout{Class: ParamByval, Size: 16, Align: 8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			abi := c.surge(t, []types.TypeID{tc.id}, types.NoTypeID)
			want := tc.want
			want.Type = tc.id
			if len(abi.Params) != 1 || abi.Params[0] != want {
				t.Fatalf("params = %+v, want [%+v]", abi.Params, want)
			}
			if abi.Ret.Class != RetVoid {
				t.Fatalf("result class = %s, want %s", abi.Ret.Class, RetVoid)
			}
		})
	}
}

func TestCallLayoutClassifiesResults(t *testing.T) {
	c := newCallLayoutCorpus(t)
	for _, tc := range []struct {
		name string
		id   types.TypeID
		want RetLayout
	}{
		{name: "no result", id: types.NoTypeID, want: RetLayout{Class: RetVoid}},
		{name: "scalar", id: c.i64, want: RetLayout{Class: RetDirect, Size: 8, Align: 8}},
		{name: "handle", id: c.str, want: RetLayout{Class: RetDirect, Size: 8, Align: 8}},
		{name: "reference", id: c.pointRef, want: RetLayout{Class: RetDirect, Size: 8, Align: 8}},
		{name: "struct", id: c.point, want: RetLayout{Class: RetSret, Size: 16, Align: 8}},
		{name: "packed struct", id: c.packed, want: RetLayout{Class: RetSret, Size: 9, Align: 1}},
		{name: "over-aligned struct", id: c.overaligned, want: RetLayout{Class: RetSret, Size: 16, Align: 16}},
		{name: "fixed array", id: c.pointArray, want: RetLayout{Class: RetSret, Size: 48, Align: 8}},
		{name: "union", id: c.choice, want: RetLayout{Class: RetSret, Size: 24, Align: 8}},
		{name: "zero-sized struct", id: c.empty, want: RetLayout{Class: RetVoid, Size: 0, Align: 1}},
		{name: "over-aligned zero-sized struct", id: c.alignedZST, want: RetLayout{Class: RetVoid, Size: 0, Align: 16}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			abi := c.surge(t, nil, tc.id)
			want := tc.want
			want.Type = tc.id
			if abi.Ret != want {
				t.Fatalf("result = %+v, want %+v", abi.Ret, want)
			}
			if len(abi.Params) != 0 {
				t.Fatalf("params = %+v, want none", abi.Params)
			}
		})
	}
}

// TestCallLayoutAgreesAcrossDirectAndIndirectCallers pins the reason this
// contract is keyed by type and not by function identity. A definition site
// carries a parameter list and a result; an indirect caller has only the
// callee's type. If those two could reach different answers, one of them would
// pass a composite by address while the other passed it directly, or would omit
// a hidden result destination the callee writes through — IR that verifies and
// then corrupts memory.
func TestCallLayoutAgreesAcrossDirectAndIndirectCallers(t *testing.T) {
	c := newCallLayoutCorpus(t)
	params := []types.TypeID{c.point, c.i64, c.empty}
	result := c.point
	fnType := c.types.RegisterFn(params, result)

	fromDefinition, err := c.table.OfSignature(params, result, ABIDomainSurge)
	if err != nil {
		t.Fatalf("OfSignature: %v", err)
	}
	fromCalleeType, err := c.table.Of(fnType, ABIDomainSurge)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	definitionABI, err := fromDefinition.Surge()
	if err != nil {
		t.Fatalf("Surge from definition: %v", err)
	}
	calleeABI, err := fromCalleeType.Surge()
	if err != nil {
		t.Fatalf("Surge from callee type: %v", err)
	}
	if !reflect.DeepEqual(definitionABI, calleeABI) {
		t.Fatalf("definition ABI %+v != callee-type ABI %+v", definitionABI, calleeABI)
	}
	if calleeABI.Ret.Class != RetSret || calleeABI.Params[0].Class != ParamByval {
		t.Fatalf("expected an sret result and a byval first argument, got %+v", calleeABI)
	}

	// The same signature spelled through an alias is the same call.
	aliasFn := c.types.RegisterAlias(c.types.Strings.Intern("Transform"), source.Span{})
	c.types.SetAliasTarget(aliasFn, fnType)
	fromAlias, err := c.table.Of(aliasFn, ABIDomainSurge)
	if err != nil {
		t.Fatalf("Of alias: %v", err)
	}
	aliasABI, err := fromAlias.Surge()
	if err != nil {
		t.Fatalf("Surge from alias: %v", err)
	}
	if !reflect.DeepEqual(aliasABI, calleeABI) {
		t.Fatalf("alias ABI %+v != callee-type ABI %+v", aliasABI, calleeABI)
	}
}

// assertMarkerOnly checks that a layout carries its domain and nothing else:
// asking it for a hidden destination or a by-value argument fails rather than
// answering with a zero value that would read as "neither".
func assertMarkerOnly(t *testing.T, l CallLayout, want ABIDomain, source string) {
	t.Helper()
	if l.Domain() != want {
		t.Fatalf("%s domain = %s, want %s", source, l.Domain(), want)
	}
	if _, err := l.Surge(); err == nil {
		t.Fatalf("%s classified a call another authority governs", source)
	}
}

func TestCallLayoutRefusesClassificationOffDomain(t *testing.T) {
	c := newCallLayoutCorpus(t)
	fnType := c.types.RegisterFn([]types.TypeID{c.point}, c.point)
	for _, domain := range []ABIDomain{ABIDomainRuntime, ABIDomainForeign} {
		t.Run(domain.String(), func(t *testing.T) {
			computed, err := ComputeCallLayout(c.types, c.layouts, fnType, domain)
			if err != nil {
				t.Fatalf("ComputeCallLayout: %v", err)
			}
			assertMarkerOnly(t, computed, domain, "computed layout")

			viaTable, err := c.table.Of(fnType, domain)
			if err != nil {
				t.Fatalf("Of: %v", err)
			}
			assertMarkerOnly(t, viaTable, domain, "table layout")

			viaSignature, err := c.table.OfSignature([]types.TypeID{c.point}, c.point, domain)
			if err != nil {
				t.Fatalf("OfSignature: %v", err)
			}
			assertMarkerOnly(t, viaSignature, domain, "definition-site layout")

			// A hand-written or foreign callee need not have a function type
			// here at all, and asking still yields only the marker.
			bare, err := ComputeCallLayout(nil, nil, types.NoTypeID, domain)
			if err != nil {
				t.Fatalf("ComputeCallLayout without a type: %v", err)
			}
			assertMarkerOnly(t, bare, domain, "layout without a callee type")
		})
	}
}

func TestCallLayoutRejectsAnUnnamedDomain(t *testing.T) {
	c := newCallLayoutCorpus(t)
	fnType := c.types.RegisterFn([]types.TypeID{c.i64}, c.i64)
	if _, err := ComputeCallLayout(c.types, c.layouts, fnType, ABIDomainInvalid); err == nil {
		t.Fatal("ComputeCallLayout accepted a call with no named authority")
	}
	if _, err := ComputeCallLayoutForSignature(c.types, c.layouts, nil, c.i64, ABIDomainInvalid); err == nil {
		t.Fatal("ComputeCallLayoutForSignature accepted a call with no named authority")
	}
	if _, err := c.table.Of(fnType, ABIDomainInvalid); err == nil {
		t.Fatal("Of accepted a call with no named authority")
	}
}

// TestCallLayoutZeroValueClassifiesNothing keeps an unset layout from reading as
// "no hidden destination, no by-value arguments".
func TestCallLayoutZeroValueClassifiesNothing(t *testing.T) {
	var l CallLayout
	if l.Domain() != ABIDomainInvalid {
		t.Fatalf("zero domain = %s, want %s", l.Domain(), ABIDomainInvalid)
	}
	if _, err := l.Surge(); err == nil {
		t.Fatal("the zero call layout answered a classification query")
	}
	var param ParamLayout
	if param.Class != ParamGoverned {
		t.Fatalf("zero parameter class = %s, want %s", param.Class, ParamGoverned)
	}
	var ret RetLayout
	if ret.Class != RetGoverned {
		t.Fatalf("zero result class = %s, want %s", ret.Class, RetGoverned)
	}
}

func TestCallLayoutFailsClosedOnUnresolvedLayout(t *testing.T) {
	c := newCallLayoutCorpus(t)
	fnType := c.types.RegisterFn([]types.TypeID{c.orphan}, types.NoTypeID)
	if _, err := ComputeCallLayout(c.types, c.layouts, fnType, ABIDomainSurge); err == nil {
		t.Fatal("an unresolved composite argument was classified instead of refused")
	}
	if _, err := c.table.Of(fnType, ABIDomainSurge); err == nil {
		t.Fatal("the table classified an unresolved composite argument")
	}
	resultFn := c.types.RegisterFn(nil, c.orphan)
	if _, err := ComputeCallLayout(c.types, c.layouts, resultFn, ABIDomainSurge); err == nil {
		t.Fatal("an unresolved composite result was classified instead of refused")
	}
	if _, err := ComputeCallLayoutForSignature(c.types, c.layouts, []types.TypeID{types.NoTypeID}, types.NoTypeID, ABIDomainSurge); err == nil {
		t.Fatal("a parameter with no type was classified")
	}
	if _, err := ComputeCallLayoutForSignature(c.types, nil, nil, c.point, ABIDomainSurge); err == nil {
		t.Fatal("a composite result was classified with no finalized layouts")
	}
	if _, err := ComputeCallLayout(c.types, c.layouts, c.point, ABIDomainSurge); err == nil {
		t.Fatal("a non-function type was accepted as a callee")
	}
	var missing *CallLayoutTable
	if _, err := missing.Of(fnType, ABIDomainSurge); err == nil {
		t.Fatal("a missing table answered a query")
	}
	if _, err := missing.OfSignature(nil, c.i64, ABIDomainSurge); err == nil {
		t.Fatal("a missing table answered a signature query")
	}
}

func TestCallLayoutIsDeterministic(t *testing.T) {
	c := newCallLayoutCorpus(t)
	params := []types.TypeID{c.packed, c.overaligned, c.alignedZST, c.str}
	fnType := c.types.RegisterFn(params, c.choice)

	first := c.surge(t, params, c.choice)
	second := c.surge(t, params, c.choice)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("recomputed ABI %+v != %+v", second, first)
	}
	cached, err := c.table.Of(fnType, ABIDomainSurge)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	recached, err := c.table.Of(fnType, ABIDomainSurge)
	if err != nil {
		t.Fatalf("Of again: %v", err)
	}
	cachedABI, err := cached.Surge()
	if err != nil {
		t.Fatalf("Surge: %v", err)
	}
	recachedABI, err := recached.Surge()
	if err != nil {
		t.Fatalf("Surge again: %v", err)
	}
	if !reflect.DeepEqual(cachedABI, recachedABI) || !reflect.DeepEqual(cachedABI, first) {
		t.Fatalf("remembered ABI %+v != %+v / %+v", recachedABI, cachedABI, first)
	}

	// A caller holding an answer cannot edit the one the next caller receives.
	cachedABI.Params[0] = ParamLayout{}
	afterEdit, err := c.table.Of(fnType, ABIDomainSurge)
	if err != nil {
		t.Fatalf("Of after edit: %v", err)
	}
	afterEditABI, err := afterEdit.Surge()
	if err != nil {
		t.Fatalf("Surge after edit: %v", err)
	}
	if !reflect.DeepEqual(afterEditABI, first) {
		t.Fatalf("remembered ABI was edited through a caller's copy: %+v", afterEditABI)
	}
}

func TestFinalizeModuleMetaPublishesCallLayouts(t *testing.T) {
	typesIn := newLayoutTestInterner()
	point := typesIn.RegisterStruct(typesIn.Strings.Intern("Point"), source.Span{})
	typesIn.SetStructFields(point, []types.StructField{{Type: typesIn.Builtins().Int64}})
	mod := moduleWithLayoutGlobal(point)
	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU(), nil); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	fnType := typesIn.RegisterFn([]types.TypeID{point}, point)
	l, err := mod.Meta.CallLayouts.Of(fnType, ABIDomainSurge)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	abi, err := l.Surge()
	if err != nil {
		t.Fatalf("Surge: %v", err)
	}
	if abi.Ret.Class != RetSret || len(abi.Params) != 1 || abi.Params[0].Class != ParamByval {
		t.Fatalf("published ABI = %+v, want an sret result and one byval argument", abi)
	}
}
