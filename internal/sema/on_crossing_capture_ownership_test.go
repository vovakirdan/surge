package sema

import "testing"

// Accepting a capture and reclaiming it are one decision made in two places,
// and the places do not run at the same time: classifyOnCapture judges the
// capture AFTER the body has been walked, while registerCrossingBodyOwnership
// has to tell the body it owns the value BEFORE the walk, so a `ret` inside can
// collect it. That is why the capture set is recomputed rather than threaded --
// and why the two can quietly stop meaning the same set.
//
// They did. The registration asked `isOwnType`, which every dynamic array
// answers no to (an array literal cannot even be bound as `own int[]`), so from
// the moment the gate began accepting arrays the caller's binding died at the
// crossing and nothing on the other side dropped what it left behind. A body
// that only READ its captured array leaked the header and the buffer; a body
// that handed the array to an owning callee was clean, which is what hid it.
//
// This table is the invariant itself: for every shape, the verdict the gate
// reaches and the predicate the registration reads agree. It carries both
// answers, because agreement on one proves nothing.
func TestDynamicArrayCaptureRegistrationMatchesTheGatesVerdict(t *testing.T) {
	tc, _, syms := newContractChecker(t, `
type Plain = { id: int }
@shard_movable type Movable = { id: int }

type Shapes = {
    ints: int[],
    strs: string[],
    floats: float[],
    nested: int[][],
    movables: Movable[],
    plains: Plain[],
    scalar: int,
    plain: Plain,
}
`)
	shapesSym := lookupSymbolByName(syms, tc.builder.StringsInterner.Intern("Shapes"))
	if !shapesSym.IsValid() {
		t.Fatal("Shapes was not resolved")
	}
	fields := tc.types.StructFields(syms.Table.Symbols.Get(shapesSym).Type)
	if len(fields) != 8 {
		t.Fatalf("Shapes has %d fields, want 8", len(fields))
	}
	moves, stays := 0, 0
	for _, field := range fields {
		name := tc.lookupName(field.Name)
		mode, verdict, accepted := tc.classifyOnCapture(field.Type, blockingCapture{})
		gateMovesArray := accepted &&
			mode == CrossingCaptureMoveOwned &&
			verdict == CrossingCaptureOwnedMovableElements
		registers := tc.crossingCaptureMovesAsDynamicArray(field.Type)
		if registers != gateMovesArray {
			t.Errorf("%s (%s): the gate moves it as an array=%t, the body's ownership registration says %t "+
				"-- one of them abandons the value or drops what it does not own",
				name, tc.typeLabel(field.Type), gateMovesArray, registers)
		}
		if gateMovesArray {
			moves++
		} else {
			stays++
		}
	}
	if moves != 5 || stays != 3 {
		t.Fatalf("table answered %d moved-as-array / %d not, want 5 / 3; one verdict everywhere proves nothing",
			moves, stays)
	}
}
