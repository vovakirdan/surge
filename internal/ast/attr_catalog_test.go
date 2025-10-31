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
