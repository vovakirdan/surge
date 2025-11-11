package types

import "testing"

func TestInternerBuiltins(t *testing.T) {
	in := NewInterner()
	b := in.Builtins()
	if b.Unit == NoTypeID || b.Bool == NoTypeID {
		t.Fatalf("builtins not initialized")
	}
	unit, _ := in.Lookup(b.Unit)
	if unit.Kind != KindUnit {
		t.Fatalf("expected unit kind, got %v", unit.Kind)
	}
}

func TestInternerDeduplicatesDescriptors(t *testing.T) {
	in := NewInterner()
	elem := in.Intern(Type{Kind: KindString})
	arr1 := in.Intern(MakeArray(elem, ArrayDynamicLength))
	arr2 := in.Intern(MakeArray(elem, ArrayDynamicLength))
	if arr1 != arr2 {
		t.Fatalf("array types should be deduplicated")
	}
}

func TestReferenceMutabilityAffectsIdentity(t *testing.T) {
	in := NewInterner()
	elem := in.Builtins().Int
	mut := in.Intern(MakeReference(elem, true))
	imm := in.Intern(MakeReference(elem, false))
	if mut == imm {
		t.Fatalf("mutable and immutable references must differ")
	}
}
