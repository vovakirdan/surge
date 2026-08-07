package vm

import (
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// retagUnionValue has three answers, and the test that matters is the one that
// separates the two it used to conflate: a conversion that DOES NOT APPLY and a
// conversion that applies and FAILS.
//
// The second used to be reported as the first, which handed the caller back its
// unconverted value and let the store fail afterwards for an unrelated reason.
// That is invisible in any test that only checks the end state, because the end
// state is "it failed" either way — so these assert on WHICH answer came back.

const (
	retagWrapSym = symbols.SymbolID(71)
	retagBareSym = symbols.SymbolID(72)
)

// retagFixture is two unions with an arm of the SAME NAME and different arity.
// That is the smallest shape where the arms match by name — so the conversion
// applies — and cannot be carried out.
type retagFixture struct {
	vm     *VM
	frame  *Frame
	narrow types.TypeID
	wide   types.TypeID
	leaf   types.TypeID
}

func newRetagFixture(t *testing.T) *retagFixture {
	t.Helper()
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()
	i64 := interner.Intern(types.Type{Kind: types.KindInt, Width: 64})

	leaf := interner.RegisterStruct(interner.Strings.Intern("Leaf"), source.Span{})
	interner.SetStructFields(leaf, []types.StructField{
		{Name: interner.Strings.Intern("a"), Type: i64},
	})

	wrapped := interner.Strings.Intern("Wrapped")
	bare := interner.Strings.Intern("Bare")
	narrow := interner.RegisterUnion(interner.Strings.Intern("Narrow"), source.Span{})
	interner.SetUnionMembers(narrow, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: wrapped, TagArgs: []types.TypeID{leaf}},
		{Kind: types.UnionMemberTag, TagName: bare},
	})
	wide := interner.RegisterUnion(interner.Strings.Intern("Wide"), source.Span{})
	interner.SetUnionMembers(wide, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: wrapped, TagArgs: []types.TypeID{leaf, leaf}},
		{Kind: types.UnionMemberTag, TagName: bare},
	})

	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{i64, leaf, narrow, wide})
	if err != nil {
		t.Fatalf("freezing the fixture layouts must succeed: %v", err)
	}
	tagCases := []mir.TagCaseMeta{
		{TagName: "Wrapped", TagSym: retagWrapSym},
		{TagName: "Bare", TagSym: retagBareSym},
	}
	module := &mir.Module{Meta: &mir.ModuleMeta{
		Layouts:    registry,
		TagLayouts: map[types.TypeID][]mir.TagCaseMeta{narrow: tagCases, wide: tagCases},
		TagNames:   map[symbols.SymbolID]string{retagWrapSym: "Wrapped", retagBareSym: "Bare"},
	}}

	machine := New(module, nil, nil, interner, nil)
	fn := &mir.Func{
		Locals: []mir.Local{{Name: "value", Type: narrow}},
		Blocks: []mir.Block{{}},
		Entry:  0,
	}
	frame := machine.activate(fn)
	machine.Stack = []*Frame{frame}
	return &retagFixture{vm: machine, frame: frame, narrow: narrow, wide: wide, leaf: leaf}
}

// wrappedNarrow builds `Wrapped(Leaf)` as a Narrow living in storage.
func (f *retagFixture) wrappedNarrow(t *testing.T) Value {
	t.Helper()
	payload, vmErr := f.vm.buildComposite(f.frame, f.leaf)
	if vmErr != nil {
		t.Fatalf("building the payload must succeed: %v", vmErr)
	}
	value, vmErr := f.vm.buildTag(f.frame, f.narrow, retagWrapSym, []Value{MakeComposite(payload)})
	if vmErr != nil {
		t.Fatalf("building the union must succeed: %v", vmErr)
	}
	return value
}

// A conversion that APPLIES and cannot be carried out must RAISE. The arms match
// by name, so this is not "no conversion applies" — it is a conversion that
// failed, and the two used to be the same answer.
func TestRetagUnionValueRaisesWhenTheConversionFails(t *testing.T) {
	f := newRetagFixture(t)
	value := f.wrappedNarrow(t)

	converted, applied, vmErr := f.vm.retagUnionValue(value, f.wide)
	if vmErr == nil {
		t.Fatal("a conversion that cannot be carried out must not be reported as one that does not apply")
	}
	if applied {
		t.Fatal("a failed conversion must not also claim to have applied")
	}
	if !strings.Contains(vmErr.Message, "carries") {
		t.Fatalf("the refusal must say what could not be carried across: %q", vmErr.Message)
	}
	// The value is handed back untouched, so the caller has something to release.
	if converted != value {
		t.Fatal("a failed conversion must leave the value alone")
	}

	f.vm.dropValue(value)
}

// A conversion that DOES NOT APPLY stays quiet, because the store this feeds
// refuses the value with both types named. Only the failing case raises.
func TestRetagUnionValueStaysQuietWhenNoConversionApplies(t *testing.T) {
	f := newRetagFixture(t)

	for _, tc := range []struct {
		name     string
		value    Value
		expected types.TypeID
	}{
		{name: "not a union at all", value: MakeInt(7, types.NoTypeID), expected: f.wide},
		{name: "no destination type", value: f.wrappedNarrow(t), expected: types.NoTypeID},
		{name: "already that union", value: f.wrappedNarrow(t), expected: f.narrow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			converted, applied, vmErr := f.vm.retagUnionValue(tc.value, tc.expected)
			if vmErr != nil {
				t.Fatalf("a conversion that does not apply must not raise: %v", vmErr)
			}
			if applied {
				t.Fatal("no conversion applies, so none may be reported")
			}
			if converted != tc.value {
				t.Fatal("the value must be handed back untouched")
			}
		})
	}
}
