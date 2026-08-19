package vm

import (
	"fmt"

	"surge/internal/types"
)

// Scratch is where a composite lives between the operation that builds it and
// the slot it is stored into.
//
// An interpreter needs this for the same reason a compiled function needs an
// alloca. A value that has been produced but not yet stored has to be
// SOMEWHERE, and once composites are exact-layout bytes the only somewhere is
// an arena extent. The producing activation owns it, which is what keeps the
// ownership question answerable: the extent dies with the instruction that
// created it unless that instruction moved it into a slot.
//
// The reclamation discipline is deliberately not a per-value free:
//
//   - space is reclaimed by rewinding to a HIGH-WATER MARK taken at the
//     instruction boundary, so no path can free one extent twice;
//   - a value stored into a slot MOVES, and the operation that moves it marks
//     its extent dead in the same step, so the rewind cannot drop it again;
//   - a rewind drops every extent still live, which is what bounds a panic
//     partway through an expression: what was built is released exactly once,
//     and what was never built is not touched;
//   - the rewind bumps the arena generation, so a reference into scratch that
//     outlives its instruction is refused rather than resolved against whatever
//     occupies those bytes next.
type scratch struct {
	arena   *Arena
	used    uint64
	entries []scratchEntry
}

// scratchEntry remembers one extent so a rewind knows what it still owes. A
// dead entry is one whose value was moved out; its bytes are still reserved
// until the rewind, because rewinding is by mark and not by extent.
//
// An entry with a nonzero stride is an element RUN rather than a single value:
// it holds `count` values of `typeID` laid end to end, and a rewind owes a drop
// for each of them. `count` tracks the run's LENGTH and never its capacity —
// the slots past the length hold nothing, and dropping them would drop what a
// pop already moved out.
type scratchEntry struct {
	offset uint64
	size   uint64
	typeID types.TypeID
	live   bool
	count  uint64
	stride uint64
}

// scratchMark names a point in scratch to rewind back to.
type scratchMark struct {
	used    uint64
	entries int
}

func newScratch() *scratch {
	return &scratch{arena: &Arena{gen: 1}}
}

// mark records the current high-water point. Every instruction takes one before
// it evaluates anything and rewinds to it afterwards, which is what makes the
// lifetime of a temporary exactly the instruction that produced it.
func (s *scratch) mark() scratchMark {
	if s == nil {
		return scratchMark{}
	}
	return scratchMark{used: s.used, entries: len(s.entries)}
}

// reserve hands out one extent for a value of a known type.
//
// The type is required rather than a size, because an extent without a type is
// bytes nobody can walk: a rewind has to drop what the extent holds, and only
// the type says what that is. This is also why a producer receives the
// DESTINATION type — a composite cannot be built anywhere without one.
func (vm *VM) reserveScratch(s *scratch, typeID types.TypeID) (StorageRef, error) {
	if s == nil || s.arena == nil {
		return StorageRef{}, fmt.Errorf("storage: this activation has no scratch storage")
	}
	if vm == nil || vm.Layouts == nil {
		return StorageRef{}, fmt.Errorf("storage: no finalized layout registry")
	}
	facts, err := vm.Layouts.Require(typeID)
	if err != nil {
		return StorageRef{}, fmt.Errorf("storage: type#%d has no usable layout: %w", typeID, err)
	}
	align := facts.Align
	if align == 0 {
		align = 1
	}
	offset, ok := alignUpChecked(s.used, align)
	if !ok {
		return StorageRef{}, fmt.Errorf("storage: type#%d cannot be aligned to %d in scratch", typeID, align)
	}
	end, ok := addChecked(offset, facts.Size)
	if !ok {
		return StorageRef{}, fmt.Errorf("storage: type#%d overflows scratch storage", typeID)
	}
	s.grow(end)
	s.used = end
	s.entries = append(s.entries, scratchEntry{offset: offset, size: facts.Size, typeID: typeID, live: true})
	return StorageRef{
		Arena:  s.arena,
		Offset: offset,
		TypeID: typeID,
		Gen:    s.arena.Generation(),
		Align:  align,
	}, nil
}

// grow makes room for one more extent.
//
// Growing REPLACES the byte slice, so any extent resolved before the growth
// names storage nothing will write to again. Every operation here resolves
// immediately before it reads or writes and never holds a slice across a
// reservation, which is the invariant that makes this safe; the referent table
// is appended to rather than replaced and is unaffected either way.
func (s *scratch) grow(need uint64) {
	if uint64(len(s.arena.bytes)) >= need {
		return
	}
	size := uint64(len(s.arena.bytes))
	if size == 0 {
		size = 64
	}
	for size < need {
		size *= 2
	}
	grown := make([]byte, size)
	copy(grown, s.arena.bytes)
	s.arena.bytes = grown
}

// release marks one extent dead because its value moved somewhere else.
//
// It takes the reference the mover already holds rather than an index, so the
// caller cannot name a different extent than the one it moved, and it refuses a
// reference that is not into this scratch at all — a move that thought it was
// clearing scratch and was not would leave the rewind dropping a value that now
// has a second owner.
func (s *scratch) release(ref StorageRef) error {
	if s == nil || ref.Arena != s.arena {
		return nil
	}
	for i := len(s.entries) - 1; i >= 0; i-- {
		entry := s.entries[i]
		if entry.offset == ref.Offset && entry.live {
			s.entries[i].live = false
			return nil
		}
		// A reference INSIDE a live extent names a MEMBER of that temporary,
		// not a temporary of its own, and there is nothing to retire: the thing
		// a rewind will drop is still the owner, and the move that got here has
		// already emptied the member out of it. Extents cannot overlap — a
		// reservation bumps the high-water mark past the previous one — so an
		// offset within one belongs to exactly that one.
		if entry.live && ref.Offset > entry.offset && ref.Offset < entry.offset+entry.size {
			return nil
		}
	}
	return fmt.Errorf("storage: no live scratch extent at offset %d to hand over", ref.Offset)
}

// rewind drops whatever is still live above the mark and reclaims the space,
// reporting the first drop that could not complete.
//
// The rewind always finishes. A drop that fails is reported and not retried,
// because the extent is going away either way and stopping here would leak
// every temporary above the failure — but the failure is returned rather than
// swallowed, since a temporary that could not be released is a leak somebody
// has to see.
//
// The generation bump is the load-bearing half. Without it the next reservation
// would hand the same bytes to a different value while an old reference still
// named them, and that reference would read the new occupant instead of being
// refused, which is the whole reason a reference carries a generation.
func (vm *VM) rewindScratch(s *scratch, mark scratchMark) error {
	if s == nil || mark.entries > len(s.entries) {
		return nil
	}
	var first error
	for i := len(s.entries) - 1; i >= mark.entries; i-- {
		entry := s.entries[i]
		if !entry.live {
			continue
		}
		ref := StorageRef{Arena: s.arena, Offset: entry.offset, TypeID: entry.typeID, Gen: s.arena.Generation()}
		if facts, err := vm.Layouts.Require(entry.typeID); err == nil {
			ref.Align = facts.Align
			if ref.Align == 0 {
				ref.Align = 1
			}
		}
		if entry.stride != 0 {
			// An element run owes one drop per LIVE element, walked from the
			// far end so a drop that reads a neighbour still sees it.
			for e := entry.count; e > 0; e-- {
				elemRef, refErr := vm.runElemRefAt(ref, entry.typeID, e-1)
				if refErr != nil {
					if first == nil {
						first = fmt.Errorf("storage: element %d of a run of type#%d could not be addressed: %w",
							e-1, entry.typeID, refErr)
					}
					continue
				}
				if err := vm.storageDrop(elemRef); err != nil && first == nil {
					first = fmt.Errorf("storage: element %d of a run of type#%d could not be released: %w",
						e-1, entry.typeID, err)
				}
			}
			continue
		}
		if err := vm.storageDrop(ref); err != nil && first == nil {
			first = fmt.Errorf("storage: a temporary of type#%d could not be released: %w", entry.typeID, err)
		}
	}
	s.entries = s.entries[:mark.entries]
	s.used = mark.used
	s.arena.gen++
	return first
}
