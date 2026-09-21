package sema

import (
	"slices"
	"testing"

	"surge/internal/types"
)

// A Map is a backing kind: its formal is a roster slot, returnOriginBackingContainer answers
// it, and a weak store through one Map formal unions into the other Map formals only.
const returnOriginMapBackingRosterSource = `fn roster(m: &mut Map<string, &string>, n: &Map<int, uint64>, owned: Map<string, string>, a: &uint64[], s: &string) -> nothing {
    let local: string = "local";
    return nothing;
}
`

// 1 parent + 3 leaves = 4 RUN.
func TestReturnOriginMapBackingFacts(t *testing.T) {
	v := func(slot uint32) returnOrigin { return backingFactRoot(slot, returnOriginInputValue) }
	e := func(slot uint32) returnOrigin { return backingFactRoot(slot, returnOriginInputElements) }
	a, fn := backingFactFunction(t, returnOriginMapBackingRosterSource, "roster")
	in := fn.unit.Sema.TypeInterner
	if in.MapNominalType() == types.NoTypeID || len(a.report.Diagnostics) != 0 || len(fn.info.Params) != 5 {
		t.Fatalf("PRECONDITION: Map is not declared or roster is diagnosed: %+v", a.report.Diagnostics)
	}
	t.Run("map_container_identity", func(t *testing.T) {
		if c, ok := returnOriginBackingContainer(in, fn.info.Params[0]); !ok || c.family != in.MapNominalType() || !c.reference || c.element != fn.info.Params[4] {
			t.Errorf("&mut Map<string, &string> = %+v, %v", c, ok)
		}
		if mutable, ok := returnOriginBackingDescriptor(in, fn.info.Params[0]); !ok || !mutable {
			t.Errorf("&mut Map descriptor = %v, %v", mutable, ok)
		}
		if mutable, ok := returnOriginBackingDescriptor(in, fn.info.Params[1]); !ok || mutable {
			t.Errorf("&Map descriptor = %v, %v", mutable, ok)
		}
		if _, ok := returnOriginBackingDescriptor(in, fn.info.Params[2]); ok {
			t.Error("an owned Map became a backing formal")
		}
		if _, canonical := returnOriginContainer(in, fn.info.Params[0]); canonical {
			t.Error("returnOriginContainer answered a Map")
		}
		if c, ok := returnOriginMapContainer(in, fn.info.Params[2]); !ok || c.reference {
			t.Errorf("owned Map<string, string> = %+v, %v", c, ok)
		}
		if c, ok := returnOriginBackingContainer(in, fn.info.Params[3]); !ok || c.family != in.ArrayNominalType() {
			t.Errorf("&uint64[] = %+v, %v", c, ok)
		}
	})
	t.Run("map_roster", func(t *testing.T) {
		if !slices.Equal(fn.backingSlots, []uint32{0, 1, 3}) || !slices.Equal(fn.mutableBackingSlots, []uint32{0}) {
			t.Errorf("roster backings=%v mutable=%v, want [0 1 3] [0]", fn.backingSlots, fn.mutableBackingSlots)
		}
	})
	t.Run("map_weak_store_family", func(t *testing.T) {
		b := &returnOriginBody{analyzer: a, function: fn}
		env := backingFactEnv(fn)
		values, _ := returnOriginBackingContainer(in, fn.info.Params[0])
		next := b.storeBackingContents(env, values, returnOriginBackingTargets{slots: []uint32{0}}, returnOriginValueOf(v(4)), nil, fn.item.Span)
		checkCell(t, "Map store B0", next.backing(0), e(0), v(4))
		checkCell(t, "Map store reaches Map B1", next.backing(1), e(1), v(4))
		checkCell(t, "Map store skips Array B3", next.backing(3), e(3))
		localID, local := backingFactLocal(t, fn, "local")
		scalars, _ := returnOriginBackingContainer(in, fn.info.Params[1])
		b.storeBackingContents(env, scalars, returnOriginBackingTargets{slots: []uint32{1}}, returnOriginValueOf(backingFactLocalRoot(localID, local)), nil, fn.item.Span)
		if !cellPendingAt(a, fn.item.Span, "storage loan would be discarded by a payload-free value") {
			t.Errorf("a local loan stored into Map<int, uint64> left no Pending: %+v", a.report.Pending)
		}
	})
}
