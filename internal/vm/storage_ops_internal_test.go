package vm

import (
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// storageFixture is the smallest shape that exercises every cell kind an
// ordinary composite can hold: a nested value composite, a handle-backed
// member, an unsized integer, and a tagged union with one carrying arm and one
// empty one.
type storageFixture struct {
	vm     *VM
	arena  *Arena
	plan   StoragePlan
	leaf   types.TypeID
	node   types.TypeID
	choice types.TypeID
	i64    types.TypeID
	text   types.TypeID
	anyInt types.TypeID
	wrap   symbols.SymbolID
	bare   symbols.SymbolID
}

const (
	fixtureWrapSym = symbols.SymbolID(41)
	fixtureBareSym = symbols.SymbolID(42)
)

func newStorageFixture(t *testing.T) *storageFixture {
	t.Helper()
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()

	i64 := interner.Intern(types.Type{Kind: types.KindInt, Width: 64})
	unsized := interner.Intern(types.Type{Kind: types.KindInt, Width: types.WidthAny})
	text := interner.Intern(types.Type{Kind: types.KindString})

	leaf := interner.RegisterStruct(interner.Strings.Intern("Leaf"), source.Span{})
	interner.SetStructFields(leaf, []types.StructField{
		{Name: interner.Strings.Intern("a"), Type: i64},
		{Name: interner.Strings.Intern("b"), Type: i64},
	})
	node := interner.RegisterStruct(interner.Strings.Intern("Node"), source.Span{})
	interner.SetStructFields(node, []types.StructField{
		{Name: interner.Strings.Intern("leaf"), Type: leaf},
		{Name: interner.Strings.Intern("label"), Type: text},
		{Name: interner.Strings.Intern("count"), Type: unsized},
	})
	choice := interner.RegisterUnion(interner.Strings.Intern("Choice"), source.Span{})
	interner.SetUnionMembers(choice, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: interner.Strings.Intern("Wrapped"), TagArgs: []types.TypeID{leaf}},
		{Kind: types.UnionMemberTag, TagName: interner.Strings.Intern("Bare")},
	})

	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{i64, unsized, text, leaf, node, choice})
	if err != nil {
		t.Fatalf("freezing the fixture layouts must succeed: %v", err)
	}

	module := &mir.Module{Meta: &mir.ModuleMeta{
		Layouts: registry,
		TagLayouts: map[types.TypeID][]mir.TagCaseMeta{
			choice: {
				{TagName: "Wrapped", TagSym: fixtureWrapSym},
				{TagName: "Bare", TagSym: fixtureBareSym},
			},
		},
		TagNames: map[symbols.SymbolID]string{fixtureWrapSym: "Wrapped", fixtureBareSym: "Bare"},
	}}

	machine := New(module, nil, nil, interner, nil)
	slots := []types.TypeID{node, node, choice, choice}
	plan := buildStoragePlan(registry, slots, interner.IsValueComposite)
	if plan.Size == 0 {
		t.Fatal("a plan holding two structs and two unions must own bytes")
	}
	return &storageFixture{
		vm: machine, arena: newArena(&plan, 1), plan: plan,
		leaf: leaf, node: node, choice: choice, wrap: fixtureWrapSym, bare: fixtureBareSym,
		i64: i64, text: text, anyInt: unsized,
	}
}

func (f *storageFixture) ref(t *testing.T, slot int, typeID types.TypeID) StorageRef {
	t.Helper()
	ref, err := f.vm.storageRefAt(f.arena, f.plan.OffsetOf(slot), typeID)
	if err != nil {
		t.Fatalf("naming slot %d must succeed: %v", slot, err)
	}
	return ref
}

// writeNode fills a Node so every cell kind is exercised at once.
func (f *storageFixture) writeNode(t *testing.T, ref StorageRef, a, b, count int64, label string) Handle {
	t.Helper()
	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	leafRef, err := ref.memberRef(members[0])
	if err != nil {
		t.Fatalf("projecting the nested value must succeed: %v", err)
	}
	leafMembers, err := f.vm.compositeMembers(f.leaf)
	if err != nil {
		t.Fatalf("Leaf must have describable members: %v", err)
	}
	f.writeCell(t, leafRef, leafMembers[0], MakeInt(a, types.NoTypeID))
	f.writeCell(t, leafRef, leafMembers[1], MakeInt(b, types.NoTypeID))

	handle := f.vm.Heap.AllocString(types.NoTypeID, label)
	f.writeCell(t, ref, members[1], MakeHandleString(handle, types.NoTypeID))
	f.writeCell(t, ref, members[2], MakeInt(count, types.NoTypeID))
	return handle
}

func (f *storageFixture) writeCell(t *testing.T, owner StorageRef, member storageMember, v Value) {
	t.Helper()
	target, err := owner.memberRef(member)
	if err != nil {
		t.Fatalf("projecting a member must succeed: %v", err)
	}
	if err := f.vm.storageWriteCell(target, member, v); err != nil {
		t.Fatalf("writing a member must succeed: %v", err)
	}
}

func (f *storageFixture) readCell(t *testing.T, owner StorageRef, member storageMember) Value {
	t.Helper()
	target, err := owner.memberRef(member)
	if err != nil {
		t.Fatalf("projecting a member must succeed: %v", err)
	}
	v, err := f.vm.storageReadCell(target, member)
	if err != nil {
		t.Fatalf("reading a member must succeed: %v", err)
	}
	return v
}

func TestStorageMembersComeFromTheLayoutRegistry(t *testing.T) {
	f := newStorageFixture(t)

	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("Node has %d members, want 3", len(members))
	}
	wantKinds := []cellKind{cellComposite, cellHandle, cellTaggedWord}
	for i, member := range members {
		if member.Kind != wantKinds[i] {
			t.Fatalf("member %d classified as %s, want %s", i, member.Kind, wantKinds[i])
		}
		offset, err := f.vm.Layouts.FieldOffset(f.node, i)
		if err != nil {
			t.Fatalf("the registry must know field %d: %v", i, err)
		}
		if member.Offset != offset {
			t.Fatalf("member %d sits at %d, the registry says %d", i, member.Offset, offset)
		}
	}
}

func TestStorageRoundTripsEveryMemberOfAComposite(t *testing.T) {
	f := newStorageFixture(t)
	ref := f.ref(t, 0, f.node)
	handle := f.writeNode(t, ref, 3, 4, 7, "left")

	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	leafMembers, err := f.vm.compositeMembers(f.leaf)
	if err != nil {
		t.Fatalf("Leaf must have describable members: %v", err)
	}
	leafRef, err := ref.memberRef(members[0])
	if err != nil {
		t.Fatalf("projecting the nested value must succeed: %v", err)
	}

	if got := f.readCell(t, leafRef, leafMembers[0]); got.Int != 3 {
		t.Fatalf("the nested first field reads back as %d, want 3", got.Int)
	}
	if got := f.readCell(t, leafRef, leafMembers[1]); got.Int != 4 {
		t.Fatalf("the nested second field reads back as %d, want 4", got.Int)
	}
	label := f.readCell(t, ref, members[1])
	if label.Kind != VKHandleString || label.H != handle {
		t.Fatalf("the handle member reads back as %s/%d, want string/%d", label.Kind, label.H, handle)
	}
	if got := f.readCell(t, ref, members[2]); got.Kind != VKInt || got.Int != 7 {
		t.Fatalf("the unsized integer reads back as %s/%d, want int/7", got.Kind, got.Int)
	}
}

func TestStorageCopyIsIndependentAndCountsItsHandles(t *testing.T) {
	f := newStorageFixture(t)
	src := f.ref(t, 0, f.node)
	dst := f.ref(t, 1, f.node)
	handle := f.writeNode(t, src, 3, 4, 7, "left")

	if got := f.vm.Heap.Get(handle).RefCount; got != 1 {
		t.Fatalf("a fresh string starts at %d references, want 1", got)
	}
	if err := f.vm.storageCopy(dst, src); err != nil {
		t.Fatalf("copying a composite must succeed: %v", err)
	}
	if got := f.vm.Heap.Get(handle).RefCount; got != 2 {
		t.Fatalf("a copied handle member is held %d times, want 2", got)
	}

	leafMembers, err := f.vm.compositeMembers(f.leaf)
	if err != nil {
		t.Fatalf("Leaf must have describable members: %v", err)
	}
	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	dstLeaf, err := dst.memberRef(members[0])
	if err != nil {
		t.Fatalf("projecting the copy's nested value must succeed: %v", err)
	}
	srcLeaf, err := src.memberRef(members[0])
	if err != nil {
		t.Fatalf("projecting the original's nested value must succeed: %v", err)
	}
	if got := f.readCell(t, dstLeaf, leafMembers[0]); got.Int != 3 {
		t.Fatalf("the copy's nested field is %d, want 3", got.Int)
	}

	f.writeCell(t, dstLeaf, leafMembers[0], MakeInt(99, types.NoTypeID))
	if got := f.readCell(t, srcLeaf, leafMembers[0]); got.Int != 3 {
		t.Fatalf("writing the copy changed the original to %d", got.Int)
	}
	if got := f.readCell(t, dstLeaf, leafMembers[0]); got.Int != 99 {
		t.Fatalf("the copy did not take the write, it reads %d", got.Int)
	}
}

func TestStorageDropReleasesOnceAndLeavesNothingToDropTwice(t *testing.T) {
	f := newStorageFixture(t)
	src := f.ref(t, 0, f.node)
	dst := f.ref(t, 1, f.node)
	handle := f.writeNode(t, src, 1, 2, 3, "held")
	if err := f.vm.storageCopy(dst, src); err != nil {
		t.Fatalf("copying a composite must succeed: %v", err)
	}

	if err := f.vm.storageDrop(dst); err != nil {
		t.Fatalf("dropping the copy must succeed: %v", err)
	}
	if got := f.vm.Heap.Get(handle).RefCount; got != 1 {
		t.Fatalf("after one drop the string is held %d times, want 1", got)
	}
	if err := f.vm.storageDrop(dst); err != nil {
		t.Fatalf("dropping already-dropped storage must be a no-op, not an error: %v", err)
	}
	if got := f.vm.Heap.Get(handle).RefCount; got != 1 {
		t.Fatalf("a second drop released a reference it did not own; count is now %d", got)
	}

	if err := f.vm.storageDrop(src); err != nil {
		t.Fatalf("dropping the original must succeed: %v", err)
	}
	if obj, ok := f.vm.Heap.lookup(handle); !ok || !obj.Freed {
		t.Fatal("the last drop must release the string")
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("storage left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

func TestStorageUnionKeepsNoByteOfTheArmItReplaced(t *testing.T) {
	f := newStorageFixture(t)
	ref := f.ref(t, 2, f.choice)
	shape, err := f.vm.unionMembers(f.choice)
	if err != nil {
		t.Fatalf("Choice must have describable arms: %v", err)
	}
	if len(shape.Cases) != 2 {
		t.Fatalf("Choice has %d arms, want 2", len(shape.Cases))
	}

	wrapped, err := f.vm.storageCaseIndexOf(f.choice, shape, f.wrap)
	if err != nil {
		t.Fatalf("the carrying arm must be selectable by its tag: %v", err)
	}
	bare, err := f.vm.storageCaseIndexOf(f.choice, shape, f.bare)
	if err != nil {
		t.Fatalf("the empty arm must be selectable by its tag: %v", err)
	}

	if selectErr := f.vm.storageSetActiveCase(ref, shape, wrapped); selectErr != nil {
		t.Fatalf("selecting the carrying arm must succeed: %v", selectErr)
	}
	payload := shape.Cases[wrapped].Payload
	if len(payload) != 1 {
		t.Fatalf("the carrying arm holds %d values, want 1", len(payload))
	}
	payloadRef, projectErr := ref.memberRef(payload[0])
	if projectErr != nil {
		t.Fatalf("projecting the arm's payload must succeed: %v", projectErr)
	}
	leafMembers, membersErr := f.vm.compositeMembers(f.leaf)
	if membersErr != nil {
		t.Fatalf("Leaf must have describable members: %v", membersErr)
	}
	f.writeCell(t, payloadRef, leafMembers[0], MakeInt(0x5A5A5A5A, types.NoTypeID))

	if got, readErr := f.vm.storageActiveCase(ref, shape); readErr != nil || got != wrapped {
		t.Fatalf("the discriminant reads %d (%v), want %d", got, readErr, wrapped)
	}

	if switchErr := f.vm.storageSetActiveCase(ref, shape, bare); switchErr != nil {
		t.Fatalf("switching arms must succeed: %v", switchErr)
	}
	if switchErr := f.vm.storageSetActiveCase(ref, shape, wrapped); switchErr != nil {
		t.Fatalf("switching back must succeed: %v", switchErr)
	}
	if got := f.readCell(t, payloadRef, leafMembers[0]); got.Int != 0 {
		t.Fatalf("switching arms and back left %#x behind in the payload", got.Int)
	}
}

// A union arm that carries no TAG still has to be describable, because the
// language writes those arms and the entrypoint's own parse envelope is one:
// `Erring` is `Success(T) | E`, whose second arm is a bare type. This used to
// be refused on the grounds that nothing could name it, which named the
// limitation rather than a rule — the physical layout gives all three arm kinds
// a case, and a type alternative is distinguished by the type it admits.
func TestStorageDescribesArmsThatCarryNoTag(t *testing.T) {
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()
	i64 := interner.Intern(types.Type{Kind: types.KindInt, Width: 64})
	anonymous := interner.RegisterUnion(interner.Strings.Intern("Either"), source.Span{})
	interner.SetUnionMembers(anonymous, []types.UnionMember{
		{Kind: types.UnionMemberType, Type: i64},
		{Kind: types.UnionMemberNothing},
	})

	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{i64, anonymous})
	if err != nil {
		t.Fatalf("freezing must succeed: %v", err)
	}
	machine := New(&mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}}, nil, nil, interner, nil)

	shape, err := machine.unionMembers(anonymous)
	if err != nil {
		t.Fatalf("both arms must be describable: %v", err)
	}
	if len(shape.Cases) != 2 {
		t.Fatalf("the union has %d described arms, want 2", len(shape.Cases))
	}
	// The type alternative is named by what it admits and carries exactly that
	// one value, so a copy or a drop of this arm reaches it.
	if got, want := shape.Cases[0].TagName, typeArmName(i64); got != want {
		t.Fatalf("the type arm is called %q, want %q", got, want)
	}
	if len(shape.Cases[0].Payload) != 1 || shape.Cases[0].Payload[0].TypeID != i64 {
		t.Fatalf("the type arm carries %#v, want one value of type#%d", shape.Cases[0].Payload, i64)
	}
	// A nothing arm carries nothing, and is called what the tag layout calls it.
	if got := shape.Cases[1].TagName; got != nothingArmName {
		t.Fatalf("the nothing arm is called %q, want %q", got, nothingArmName)
	}
	if len(shape.Cases[1].Payload) != 0 {
		t.Fatalf("the nothing arm carries %d values, want none", len(shape.Cases[1].Payload))
	}
}

func TestStorageRefusesATypeWithNoInlineEncoding(t *testing.T) {
	f := newStorageFixture(t)
	if kind := f.vm.storageCellKind(types.NoTypeID); kind != cellUnsupported {
		t.Fatalf("an unknown type classified as %s", kind)
	}
	if _, err := f.vm.compositeMembers(f.leaf); err != nil {
		t.Fatalf("a plain struct must be describable: %v", err)
	}
	if _, err := f.vm.memberAt(0, types.NoTypeID); err == nil {
		t.Fatal("a member with no layout must be refused")
	}
}
