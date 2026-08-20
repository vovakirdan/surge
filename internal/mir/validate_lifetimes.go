package mir

import (
	"errors"
	"fmt"
)

// What a function must have finished doing by the time it is handed on: every
// borrow ended, every owned value dropped, and every crossing supported by a
// lowering that actually exists.
//
// These three ask about the END of a life rather than the shape of a step,
// which is why they read together and apart from the structural checks.

func validateEndBorrow(f *Func, globals []Global) error {
	var errs []error

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			if ins.Kind != InstrEndBorrow {
				continue
			}

			place := ins.EndBorrow.Place
			if place.Kind == PlaceGlobal {
				if place.Global < 0 || int(place.Global) >= len(globals) {
					continue // Already reported by validateLocalIDs
				}
				continue
			}

			localID := place.Local
			if localID < 0 || int(localID) >= len(f.Locals) {
				continue // Already reported by validateLocalIDs
			}

			loc := f.Locals[localID]
			if loc.Flags&(LocalFlagRef|LocalFlagRefMut) == 0 {
				errs = append(errs, fmt.Errorf("bb%d instr %d: end_borrow on non-reference local L%d (%s)",
					i, j, localID, loc.Name))
			}
		}
	}

	return errors.Join(errs...)
}

// validateDrop checks that Drop only targets locals that own something to
// reclaim: never a borrow, and never a local carrying no drop obligation.
//
// The rule reads the two ownership axes as two flags. A local is undroppable
// when it is duplicable and owns nothing — LocalFlagCopy without
// LocalFlagOwnsHeap. Being Copy is NOT on its own disqualifying: a
// reference-counted scalar is duplicable AND owned, and dropping one is the
// release that gives its reference back. It used to take a hardcoded
// type test to say so; now it is just a local that carries both flags, and the
// next type to want that combination — a Copy value composite — needs no
// second exception here.
func validateDrop(f *Func, globals []Global) error {
	var errs []error

	for i := range f.Blocks {
		bb := &f.Blocks[i]
		for j := range bb.Instrs {
			ins := &bb.Instrs[j]
			if ins.Kind != InstrDrop {
				continue
			}

			place := ins.Drop.Place
			if place.Kind == PlaceGlobal {
				if place.Global < 0 || int(place.Global) >= len(globals) {
					continue // Already reported by validateLocalIDs
				}
				continue
			}

			localID := place.Local
			if localID < 0 || int(localID) >= len(f.Locals) {
				continue // Already reported by validateLocalIDs
			}

			loc := f.Locals[localID]
			// A PROJECTED drop acts on a field, not on the binding, so the
			// local's own flags say nothing about it: a Copy local can hold an
			// owning field, and a reference local is projected THROUGH rather
			// than dropped. Judging the projection needs the field's type,
			// which this pass does not have — so it checks what it can, which
			// is that the base is addressable at all, and leaves the rest to
			// the emitters that do resolve the projection.
			if len(place.Proj) != 0 {
				continue
			}
			if loc.Flags&LocalFlagCopy != 0 && loc.Flags&LocalFlagOwnsHeap == 0 {
				errs = append(errs, fmt.Errorf("bb%d instr %d: drop on copy local L%d (%s) that owns nothing",
					i, j, localID, loc.Name))
			}
			if loc.Flags&(LocalFlagRef|LocalFlagRefMut) != 0 {
				errs = append(errs, fmt.Errorf("bb%d instr %d: drop on reference local L%d (%s) (use end_borrow)",
					i, j, localID, loc.Name))
			}
		}
	}

	return errors.Join(errs...)
}

func validateCrossingSupport(f *Func, opts ValidateOptions) error {
	if f == nil {
		return nil
	}
	var errs []error
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			if ins.Kind != InstrCrossing {
				continue
			}
			if opts.crossingEnabled(ins.Crossing.Kind) {
				continue
			}
			errs = append(errs, fmt.Errorf("bb%d instr %d: crossing %s is not enabled", bi, ii, mirCrossingKindName(ins.Crossing.Kind)))
		}
	}
	return errors.Join(errs...)
}
