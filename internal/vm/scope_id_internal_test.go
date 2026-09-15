package vm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func newScopeIDMachine() *VM {
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()
	return New(nil, nil, nil, interner, nil)
}

func TestVMScopeIDAcceptsUint64Bits(t *testing.T) {
	machine := newScopeIDMachine()
	for _, row := range []struct {
		name string
		bits uint64
	}{
		{"zero", 0},
		{"one", 1},
		{"max-signed", 1<<63 - 1},
		{"high-bit", 1 << 63},
		{"max-uint", ^uint64(0)},
	} {
		t.Run(row.name, func(t *testing.T) {
			before := machine.heapCounters
			value := MakeInt(asInt64(row.bits), machine.Types.Builtins().Uint64)
			got, vmErr := machine.scopeIDFromValue(value)
			if vmErr != nil || uint64(got) != row.bits {
				t.Fatalf("uint64 scope bits %#x decoded as %#x: %v", row.bits, uint64(got), vmErr)
			}
			if value.IsHeap() || machine.heapCounters != before {
				t.Fatal("a numeric scope ID allocated or counted a heap value")
			}
		})
	}
}

func TestVMScopeIDAcceptsAliasesAndReferences(t *testing.T) {
	machine := newScopeIDMachine()
	interner := machine.Types
	u64 := interner.Builtins().Uint64
	alias := interner.RegisterAlias(interner.Strings.Intern("ScopeID"), source.Span{})
	interner.SetAliasTarget(alias, u64)
	aliasChain := interner.RegisterAlias(interner.Strings.Intern("ScopeAlias"), source.Span{})
	interner.SetAliasTarget(aliasChain, alias)
	refType := interner.Intern(types.MakeReference(u64, false))
	mutType := interner.Intern(types.MakeReference(u64, true))
	aliasRefType := interner.Intern(types.MakeReference(alias, false))
	const bits = uint64(1)<<63 + 7
	frame := &Frame{Locals: []LocalSlot{
		{TypeID: u64, V: MakeInt(asInt64(bits), u64), IsInit: true},
		{TypeID: alias, V: MakeInt(asInt64(bits), alias), IsInit: true},
	}}
	loc := Location{Kind: LKLocal, FrameRef: frame, Local: 0}
	aliasLoc := Location{Kind: LKLocal, FrameRef: frame, Local: 1}
	for _, row := range []struct {
		name  string
		value Value
	}{
		{"alias", MakeInt(asInt64(bits), alias)},
		{"alias-chain", MakeInt(asInt64(bits), aliasChain)},
		{"ref", MakeRef(loc, refType)},
		{"ref-mut", MakeRefMut(loc, mutType)},
		{"ref-alias", MakeRef(aliasLoc, aliasRefType)},
	} {
		t.Run(row.name, func(t *testing.T) {
			before := machine.heapCounters
			got, vmErr := machine.scopeIDFromValue(row.value)
			if vmErr != nil || uint64(got) != bits {
				t.Fatalf("aliased/referenced uint64 scope decoded as %#x: %v", uint64(got), vmErr)
			}
			if machine.heapCounters != before || frame.Locals[0].V.Int != asInt64(bits) ||
				frame.Locals[1].V.Int != asInt64(bits) {
				t.Fatal("reading a numeric scope ID changed its input or heap ownership")
			}
		})
	}
}

func TestVMScopeIDRejectsOtherRepresentations(t *testing.T) {
	machine, taskType, _ := newResourceFixture(t)
	b := machine.Types.Builtins()
	bigInt := machine.makeBigInt(b.Int, bignum.IntFromInt64(7))
	bigUint := machine.makeBigUint(b.Uint, bignum.UintFromUint64(7))
	resource, vmErr := machine.resourceValue(7, taskType, "Task")
	if vmErr != nil {
		t.Fatalf("build resource control: %v", vmErr)
	}
	defer machine.dropValue(bigInt)
	defer machine.dropValue(bigUint)
	defer machine.dropValue(resource)
	ptrType := machine.Types.Intern(types.MakePointer(b.Uint64))
	refType := machine.Types.Intern(types.MakeReference(b.Uint64, false))
	ownType := machine.Types.Intern(types.MakeOwn(b.Uint64))
	wrongBigType := bigUint
	wrongBigType.TypeID = b.Uint64
	for _, row := range []struct {
		name  string
		value Value
	}{
		{"signed-int", MakeInt(7, b.Int)},
		{"signed64", MakeInt(7, b.Int64)},
		{"signed-negative", MakeInt(-1, b.Int64)},
		{"unbounded-uint", MakeInt(7, b.Uint)},
		{"uint8", MakeInt(7, b.Uint8)},
		{"uint16", MakeInt(7, b.Uint16)},
		{"uint32", MakeInt(7, b.Uint32)},
		{"bool", MakeBool(true, b.Bool)},
		{"bool-with-word-type", MakeBool(true, b.Uint64)},
		{"nothing", MakeNothing()},
		{"big-int", bigInt},
		{"big-uint", bigUint},
		{"big-uint-with-word-type", wrongBigType},
		{"resource", resource},
		{"pointer", MakePtr(Location{}, ptrType)},
		{"pointer-word", MakeInt(7, ptrType)},
		{"reference-word", MakeInt(7, refType)},
		{"own-word", MakeInt(7, ownType)},
		{"float-word", MakeInt(7, b.Float64)},
		{"missing-type", MakeInt(7, types.NoTypeID)},
		{"unknown-type", MakeInt(7, types.TypeID(1<<30))},
	} {
		t.Run(row.name, func(t *testing.T) {
			before := machine.heapCounters
			got, err := machine.scopeIDFromValue(row.value)
			if err == nil || err.Code != PanicTypeMismatch || err.Message != "scope id must be uint64" || got != 0 {
				t.Fatalf("scope type refusal returned id=%d, error=%v; want PanicTypeMismatch: scope id must be uint64", got, err)
			}
			if machine.heapCounters != before {
				t.Fatal("refusing a scope ID changed borrowed heap ownership")
			}
		})
	}
}

func TestVMScopeIDReferenceChecksLoadedValue(t *testing.T) {
	machine := newScopeIDMachine()
	b := machine.Types.Builtins()
	refType := machine.Types.Intern(types.MakeReference(b.Uint64, false))
	ptrType := machine.Types.Intern(types.MakePointer(b.Uint64))
	frame := &Frame{Locals: []LocalSlot{{TypeID: b.Uint64, IsInit: true}}}
	loc := Location{Kind: LKLocal, FrameRef: frame, Local: 0}
	for _, row := range []struct {
		name  string
		value Value
	}{
		{"signed-location", MakeInt(7, b.Int64)},
		{"pointer-location", MakePtr(Location{}, ptrType)},
		{"nested-reference", MakeRef(loc, refType)},
	} {
		t.Run(row.name, func(t *testing.T) {
			// The wrapper and the slot claim uint64; the actual loaded value
			// is the authority. A second dereference is never part of this API.
			frame.Locals[0].V = row.value
			got, err := machine.scopeIDFromValue(MakeRef(loc, refType))
			if err == nil || err.Code != PanicTypeMismatch || err.Message != "scope id must be uint64" || got != 0 {
				t.Fatalf("wrong scope location returned id=%d, error=%v; want exact uint64 type refusal", got, err)
			}
		})
	}
	t.Run("invalid-location", func(t *testing.T) {
		invalid := Location{Kind: LKLocal}
		_, want := machine.loadLocationRaw(invalid)
		_, got := machine.scopeIDFromValue(MakeRef(invalid, refType))
		if want == nil || got == nil || got.Code != want.Code || got.Message != want.Message {
			t.Fatalf("scope reference lost the location error: got %v, want %v", got, want)
		}
	})
}

func TestVMScopeEnterReturnsUncountedUint64(t *testing.T) {
	machine := newScopeIDMachine()
	b := machine.Types.Builtins()
	exec := machine.ensureExecutor()
	owner := exec.Spawn(1, nil)
	exec.SetCurrent(owner)
	frame := NewFrame(&mir.Func{Locals: []mir.Local{{Name: "scope", Type: b.Uint64, Flags: mir.LocalFlagCopy}}})
	call := &mir.CallInstr{
		HasDst: true,
		Dst:    mir.Place{Local: 0},
		Args: []mir.Operand{{
			Kind: mir.OperandConst, Type: b.Bool,
			Const: mir.Const{Kind: mir.ConstBool, Type: b.Bool},
		}},
	}
	before := machine.heapCounters
	var writes []LocalWrite
	if err := machine.handleScopeEnter(frame, call, &writes); err != nil {
		t.Fatalf("enter numeric scope: %v", err)
	}
	value := frame.Locals[0].V
	id, err := machine.scopeIDFromValue(value)
	if err != nil || id == 0 {
		t.Fatalf("created scope ID is not a nonzero uint64: %+v, %v", value, err)
	}
	defer exec.ExitScope(id)
	if value.Kind != VKInt || value.TypeID != b.Uint64 || value.IsHeap() || len(writes) != 1 ||
		writes[0].Value.Kind != VKInt || writes[0].Value.TypeID != b.Uint64 || writes[0].Value.Int != value.Int {
		t.Fatalf("scope-enter produced the wrong value/write: %+v, %+v", value, writes)
	}
	machine.dropValue(value)
	if machine.heapCounters != before {
		t.Fatal("creating or dropping the numeric ID touched VM heap ownership")
	}
}
