package ast

import (
	"testing"
)

func TestLookupAttr_Basic(t *testing.T) {
	spec, ok := LookupAttr("PURE")
	if !ok {
		t.Fatalf("expected to find @pure spec")
	}
	if !spec.Allows(AttrTargetFn) {
		t.Fatalf("@pure should allow function target")
	}
	if spec.Allows(AttrTargetType) {
		t.Fatalf("@pure should not allow type targets")
	}
}

func TestLookupAttr_SpecialFlags(t *testing.T) {
	override, ok := LookupAttr("override")
	if !ok {
		t.Fatalf("expected override spec")
	}
	if !override.HasFlag(AttrFlagExternOnly) {
		t.Fatalf("@override should be marked as extern-only")
	}

	intrinsic, ok := LookupAttr("intrinsic")
	if !ok {
		t.Fatalf("expected intrinsic spec")
	}
	if !intrinsic.HasFlag(AttrFlagFnDeclOnly) {
		t.Fatalf("@intrinsic should require function declarations")
	}
}

func TestShardAttrsAreTypeOnly(t *testing.T) {
	for _, name := range []string{"shard_movable", "shard_pinned"} {
		spec, ok := LookupAttr(name)
		if !ok {
			t.Fatalf("expected @%s spec", name)
		}
		if !spec.Allows(AttrTargetType) {
			t.Errorf("@%s should allow type declarations", name)
		}
		for _, bad := range []AttrTargetMask{AttrTargetFn, AttrTargetField, AttrTargetParam, AttrTargetBlock, AttrTargetLet} {
			if spec.Allows(bad) {
				t.Errorf("@%s should not allow target %d", name, bad)
			}
		}
	}
}

func TestIntrinsicAllowsConstTarget(t *testing.T) {
	// `@intrinsic pub const pool: Placement;` requires const/let target support
	// (Block 4 placement intrinsics).
	spec, ok := LookupAttr("intrinsic")
	if !ok {
		t.Fatal("expected intrinsic spec")
	}
	if !spec.Allows(AttrTargetLet) {
		t.Error("@intrinsic should allow const/let targets")
	}
	if !spec.Allows(AttrTargetFn) || !spec.Allows(AttrTargetType) {
		t.Error("@intrinsic should still allow fn and type targets")
	}
}

func TestAttrSpecsSortedUnique(t *testing.T) {
	specs := AttrSpecs()
	if len(specs) != len(attrRegistry) {
		t.Fatalf("expected %d specs, got %d", len(attrRegistry), len(specs))
	}
	for idx := 1; idx < len(specs); idx++ {
		if specs[idx-1].Name >= specs[idx].Name {
			t.Fatalf("specs not sorted: %q >= %q", specs[idx-1].Name, specs[idx].Name)
		}
	}
}
