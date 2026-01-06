package types //nolint:revive

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

func TestIsCopy(t *testing.T) {
	in := NewInterner()
	b := in.Builtins()

	// Primitives are Copy
	copyTypes := []struct {
		name string
		id   TypeID
	}{
		{"bool", b.Bool},
		{"int", b.Int},
		{"int8", b.Int8},
		{"int16", b.Int16},
		{"int32", b.Int32},
		{"int64", b.Int64},
		{"uint", b.Uint},
		{"uint8", b.Uint8},
		{"uint16", b.Uint16},
		{"uint32", b.Uint32},
		{"uint64", b.Uint64},
		{"float", b.Float},
		{"float16", b.Float16},
		{"float32", b.Float32},
		{"float64", b.Float64},
		{"unit", b.Unit},
		{"nothing", b.Nothing},
	}
	for _, tc := range copyTypes {
		if !in.IsCopy(tc.id) {
			t.Errorf("%s should be Copy", tc.name)
		}
	}

	// String is NOT Copy
	if in.IsCopy(b.String) {
		t.Error("string should NOT be Copy")
	}

	// Raw pointer is Copy
	ptr := in.Intern(MakePointer(b.Int32))
	if !in.IsCopy(ptr) {
		t.Error("*int32 should be Copy")
	}

	// Shared reference (&T) is Copy
	sharedRef := in.Intern(MakeReference(b.Int32, false))
	if !in.IsCopy(sharedRef) {
		t.Error("&int32 should be Copy")
	}

	// Mutable reference (&mut T) is NOT Copy
	mutRef := in.Intern(MakeReference(b.Int32, true))
	if in.IsCopy(mutRef) {
		t.Error("&mut int32 should NOT be Copy")
	}

	// own T is Copy if T is Copy
	ownInt := in.Intern(MakeOwn(b.Int32))
	if !in.IsCopy(ownInt) {
		t.Error("own int32 should be Copy")
	}

	// own string is NOT Copy (string is not Copy)
	ownString := in.Intern(MakeOwn(b.String))
	if in.IsCopy(ownString) {
		t.Error("own string should NOT be Copy")
	}

	// NoTypeID is not Copy
	if in.IsCopy(NoTypeID) {
		t.Error("NoTypeID should not be Copy")
	}
}
