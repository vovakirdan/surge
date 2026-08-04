package mir

import (
	"testing"

	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func newLayoutTestInterner() *types.Interner {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	return typesIn
}

func moduleWithLayoutGlobal(id types.TypeID) *Module {
	return &Module{
		Funcs:   make(map[FuncID]*Func),
		Globals: []Global{{Type: id}},
		Meta:    &ModuleMeta{},
	}
}

func TestFinalizeModuleMetaPreservesExactChildAliasLookup(t *testing.T) {
	typesIn := newLayoutTestInterner()
	alias := typesIn.RegisterAlias(typesIn.Strings.Intern("Word"), source.Span{})
	typesIn.SetAliasTarget(alias, typesIn.Builtins().Uint64)
	holder := typesIn.RegisterStruct(typesIn.Strings.Intern("Holder"), source.Span{})
	typesIn.SetStructFields(holder, []types.StructField{{Type: alias}})
	mod := moduleWithLayoutGlobal(holder)

	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU()); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	facts, err := mod.Meta.Layouts.Require(alias)
	if err != nil {
		t.Fatalf("exact alias lookup: %v", err)
	}
	if facts.Size != 8 || facts.Align != 8 {
		t.Fatalf("alias facts = %+v, want size/align 8/8", facts)
	}
}

func TestFinalizeModuleMetaStopsAtOpaqueGenericBoundaries(t *testing.T) {
	typesIn := newLayoutTestInterner()
	generic := typesIn.RegisterTypeParam(typesIn.Strings.Intern("T"), 1, 0, false, types.NoTypeID)
	ref := typesIn.Intern(types.MakeReference(generic, false))
	fn := typesIn.RegisterFn([]types.TypeID{generic}, generic)
	phantom := typesIn.RegisterStructInstance(typesIn.Strings.Intern("Phantom"), source.Span{}, []types.TypeID{generic})
	typesIn.SetStructFields(phantom, []types.StructField{{Type: typesIn.Builtins().Bool}})
	mod := &Module{
		Funcs: make(map[FuncID]*Func),
		Globals: []Global{
			{Type: ref},
			{Type: fn},
			{Type: phantom},
		},
		Meta: &ModuleMeta{},
	}

	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU()); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	for _, id := range []types.TypeID{ref, fn, phantom} {
		if _, err := mod.Meta.Layouts.Require(id); err != nil {
			t.Fatalf("physical root type#%d: %v", id, err)
		}
	}
	if _, ok := mod.Meta.Layouts.Lookup(generic); ok {
		t.Fatal("generic hidden behind opaque/phantom metadata became a physical root")
	}
}

func TestFinalizeModuleMetaKeepsRuntimeHandlePayloadRoots(t *testing.T) {
	typesIn := newLayoutTestInterner()
	makePayload := func(name string) types.TypeID {
		id := typesIn.RegisterStruct(typesIn.Strings.Intern(name), source.Span{})
		typesIn.SetStructFields(id, []types.StructField{{Type: typesIn.Builtins().Bool}})
		return id
	}

	taskPayload := makePayload("TaskPayload")
	channelPayload := makePayload("ChannelPayload")
	rangePayload := makePayload("RangePayload")
	arrayPayload := makePayload("ArrayPayload")
	mapKey := makePayload("MapKey")
	mapValue := makePayload("MapValue")
	registerHandle := func(name string, payload types.TypeID) types.TypeID {
		nameID := typesIn.Strings.Intern(name)
		decl := source.Span{File: 1, Start: 10, End: 20}
		base := typesIn.RegisterStruct(nameID, decl)
		typesIn.MarkRuntimeHandleType(base)
		return typesIn.RegisterStructInstance(nameID, decl, []types.TypeID{payload})
	}
	task := registerHandle("Task", taskPayload)
	channel := registerHandle("Channel", channelPayload)
	rangeHandle := registerHandle("Range", rangePayload)
	arrayName := typesIn.Strings.Intern("Array")
	typesIn.EnsureArrayNominal(arrayName, typesIn.Strings.Intern("T"), source.Span{}, 1)
	array := typesIn.RegisterStructInstance(arrayName, source.Span{}, []types.TypeID{arrayPayload})
	mapName := typesIn.Strings.Intern("Map")
	typesIn.EnsureMapNominal(mapName, typesIn.Strings.Intern("K"), typesIn.Strings.Intern("V"), source.Span{}, 2)
	mapHandle := typesIn.RegisterStructInstance(mapName, source.Span{}, []types.TypeID{mapKey, mapValue})
	placement := typesIn.RegisterStruct(typesIn.Strings.Intern("Placement"), source.Span{})
	typesIn.MarkRuntimePlacementType(placement)

	refHidden := makePayload("RefHidden")
	ptrHidden := makePayload("PtrHidden")
	farHidden := makePayload("FarHidden")
	fnParamHidden := makePayload("FnParamHidden")
	fnResultHidden := makePayload("FnResultHidden")
	ref := typesIn.Intern(types.MakeReference(refHidden, false))
	ptr := typesIn.Intern(types.MakePointer(ptrHidden))
	far := typesIn.Intern(types.MakeFar(farHidden))
	fn := typesIn.RegisterFn([]types.TypeID{fnParamHidden}, fnResultHidden)

	mod := &Module{Funcs: make(map[FuncID]*Func), Meta: &ModuleMeta{}}
	for _, id := range []types.TypeID{task, channel, rangeHandle, array, mapHandle, placement, ref, ptr, far, fn} {
		mod.Globals = append(mod.Globals, Global{Type: id})
	}
	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU()); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	for _, id := range []types.TypeID{task, channel, rangeHandle, array, mapHandle, placement} {
		facts, err := mod.Meta.Layouts.Require(id)
		if err != nil || facts.Size != 8 || facts.Align != 8 {
			t.Fatalf("handle %s facts = %+v, %v", types.Label(typesIn, id), facts, err)
		}
	}
	for _, id := range []types.TypeID{taskPayload, channelPayload, rangePayload, arrayPayload, mapKey, mapValue} {
		if _, err := mod.Meta.Layouts.Require(id); err != nil {
			t.Fatalf("owned handle payload %s missing: %v", types.Label(typesIn, id), err)
		}
	}
	for _, id := range []types.TypeID{refHidden, ptrHidden, farHidden, fnParamHidden, fnResultHidden} {
		if _, ok := mod.Meta.Layouts.Lookup(id); ok {
			t.Fatalf("opaque pointee/signature child %s became a layout root", types.Label(typesIn, id))
		}
	}
}

func TestFinalizeModuleMetaFixedArrayConstIsIdentityNotPhysicalRoot(t *testing.T) {
	typesIn := newLayoutTestInterner()
	name := typesIn.Strings.Intern("ArrayFixed")
	typesIn.EnsureArrayFixedNominal(
		name,
		typesIn.Strings.Intern("T"),
		typesIn.Strings.Intern("N"),
		source.Span{},
		1,
		typesIn.Builtins().Uint32,
	)
	length := typesIn.Intern(types.MakeConstUint(4))
	fixed := typesIn.RegisterStructInstance(name, source.Span{}, []types.TypeID{typesIn.Builtins().Int32, length})
	mod := moduleWithLayoutGlobal(fixed)

	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU()); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	facts, err := mod.Meta.Layouts.Require(fixed)
	if err != nil {
		t.Fatalf("fixed array lookup: %v", err)
	}
	if facts.Size != 16 || facts.Align != 4 {
		t.Fatalf("fixed array facts = %+v, want size/align 16/4", facts)
	}
	if _, ok := mod.Meta.Layouts.Lookup(length); ok {
		t.Fatal("fixed-array const identity argument became a physical root")
	}
}

func TestValidateStructureIsExplicitWhileValidateRequiresFinalLayouts(t *testing.T) {
	typesIn := newLayoutTestInterner()
	mod := moduleWithLayoutGlobal(typesIn.Builtins().Bool)

	if err := ValidateStructure(mod, typesIn); err != nil {
		t.Fatalf("ValidateStructure: %v", err)
	}
	if err := Validate(mod, typesIn); err == nil {
		t.Fatal("Validate accepted production MIR without finalized layouts")
	}
	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU()); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	if err := Validate(mod, typesIn); err != nil {
		t.Fatalf("Validate after finalization: %v", err)
	}
}

func TestFinalizeModuleMetaSkipsConstFunctionTypeArgument(t *testing.T) {
	typesIn := newLayoutTestInterner()
	constant := typesIn.Intern(types.MakeConstUint(7))
	mod := moduleWithLayoutGlobal(typesIn.Builtins().Bool)
	mod.Meta.FuncTypeArgs = map[symbols.SymbolID][]types.TypeID{
		symbols.SymbolID(1): {constant},
	}

	if err := FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU()); err != nil {
		t.Fatalf("FinalizeModuleMeta: %v", err)
	}
	if _, ok := mod.Meta.Layouts.Lookup(constant); ok {
		t.Fatal("const function argument became a physical root")
	}
}

func TestLayoutRootWalkerCoversEveryNodeKind(t *testing.T) {
	if instrKindCount != InstrEnvelopeRelease+1 ||
		rvalueKindCount != RValueHeirTest+1 ||
		operandKindCount != OperandCopyValue+1 ||
		termKindCount != TermUnreachable+1 ||
		calleeKindCount != CalleeValue+1 ||
		selectArmKindCount != SelectArmDefault+1 {
		t.Fatal("MIR kind count sentinel is not immediately after its last known kind")
	}

	typesIn := newLayoutTestInterner()
	collector := &layoutRootCollector{
		types:         typesIn,
		builder:       layout.New(layout.X86_64LinuxGNU(), typesIn),
		exactSeen:     make(map[types.TypeID]struct{}),
		canonicalSeen: make(map[types.TypeID]struct{}),
	}
	for kind := InstrKind(0); kind < instrKindCount; kind++ {
		if err := collector.walkInstr(&Instr{Kind: kind}); err != nil {
			t.Errorf("InstrKind %d: %v", kind, err)
		}
	}
	for kind := RValueKind(0); kind < rvalueKindCount; kind++ {
		if err := collector.walkRValue(&RValue{Kind: kind}); err != nil {
			t.Errorf("RValueKind %d: %v", kind, err)
		}
	}
	for kind := OperandKind(0); kind < operandKindCount; kind++ {
		if err := collector.walkOperand(&Operand{Kind: kind}); err != nil {
			t.Errorf("OperandKind %d: %v", kind, err)
		}
	}
	for kind := TermKind(0); kind < termKindCount; kind++ {
		if err := collector.walkTerm(&Terminator{Kind: kind}); err != nil {
			t.Errorf("TermKind %d: %v", kind, err)
		}
	}
	for kind := CalleeKind(0); kind < calleeKindCount; kind++ {
		if err := collector.walkInstr(&Instr{Kind: InstrCall, Call: CallInstr{Callee: Callee{Kind: kind}}}); err != nil {
			t.Errorf("CalleeKind %d: %v", kind, err)
		}
	}
	for kind := SelectArmKind(0); kind < selectArmKindCount; kind++ {
		if err := collector.walkInstr(&Instr{Kind: InstrSelect, Select: SelectInstr{Arms: []SelectArm{{Kind: kind}}}}); err != nil {
			t.Errorf("SelectArmKind %d: %v", kind, err)
		}
	}
}
