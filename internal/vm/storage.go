package vm

import (
	"fmt"

	"surge/internal/layout"
	"surge/internal/types"
)

// noStorageOffset marks a slot that owns no arena bytes: a scalar, a
// handle-backed value, or a reference. Only value composites are laid out
// inline, so only they get an offset.
const noStorageOffset = ^uint64(0)

// Arena owns the inline bytes of every composite value rooted in one frame
// activation, in the globals table, or in one heap-owned composite.
//
// The arena is exact-layout storage, not an indirection cell. A composite local
// IS the bytes at its offset: projecting a field is arithmetic on that offset,
// not a second lookup through a per-value object, and two locals of the same
// type occupy two disjoint byte ranges rather than two pointers to one. The
// byte extents come from the layout registry and nowhere else, so the VM cannot
// invent a size, an alignment or a field offset that the native backend does
// not also see.
//
// Members whose VM value is not a bit pattern — a string, a dynamic array, a
// map, a reference — still occupy exactly their layout extent. What the VM
// writes into that extent is its own name for the referent (a heap handle, or
// an index into refs) rather than a machine address, because an interpreter has
// no address space to hand out. The extent, its offset and its alignment are
// the shared fact; the bits inside one member's extent are the VM's business
// and never spill into a neighbour's.
type Arena struct {
	bytes []byte
	// refs holds the referents of reference and pointer members. A member's
	// extent stores index+1, so a zeroed extent reads back as "no referent"
	// and partially initialized storage cannot be mistaken for refs[0].
	refs     []Location
	refIndex map[Location]uint64
	gen      uint32
	// pins counts the slots of the owning frame that some task state is
	// borrowing as backing storage. An arena with a live pin is never retired,
	// so a pinned frame that outlives the stack keeps valid storage.
	pins uint32
}

// StorageRef names one inline composite value: the arena that owns its bytes,
// the byte offset where its layout begins, its static type, the arena
// generation current when the reference was formed, and the alignment the
// layout registry proved for it.
//
// Offset is a uint64 because a field offset legitimately exceeds the range of
// an int32 — layout's own TestPhysicalLayoutOffsetAboveMaxInt32IsNotTruncated
// pins that — and truncating it would silently address the wrong bytes.
type StorageRef struct {
	Arena  *Arena
	Offset uint64
	TypeID types.TypeID
	Gen    uint32
	Align  uint64
}

// StoragePlan assigns one arena offset to each slot of a frame or of the
// globals table. It is a pure function of the slot types and the layout
// registry, so it is computed once per function and shared by every activation.
type StoragePlan struct {
	Offsets []uint64
	Size    uint64
	Align   uint64
}

// IsEmpty reports whether the plan needs no arena at all.
func (p *StoragePlan) IsEmpty() bool { return p == nil || p.Size == 0 && p.Align <= 1 }

// OffsetOf returns the arena offset of one slot, or noStorageOffset when the
// slot is not composite-typed.
func (p *StoragePlan) OffsetOf(slot int) uint64 {
	if p == nil || slot < 0 || slot >= len(p.Offsets) {
		return noStorageOffset
	}
	return p.Offsets[slot]
}

// buildStoragePlan lays every composite-typed slot out back to back, each at
// its own alignment. Slot order is declaration order, which keeps the plan
// deterministic across runs and across the two compiles the golden gate makes.
//
// A slot whose layout is not concrete — deferred, or a type the registry
// refuses — gets no storage rather than a guessed extent: the caller then
// treats it as non-composite and the refusal surfaces where the value is used,
// naming the type, instead of here where it would name only a slot index.
func buildStoragePlan(layouts *layout.Registry, typeIDs []types.TypeID, isComposite func(types.TypeID) bool) StoragePlan {
	plan := StoragePlan{Offsets: make([]uint64, len(typeIDs)), Align: 1}
	cursor := uint64(0)
	for i, typeID := range typeIDs {
		plan.Offsets[i] = noStorageOffset
		if layouts == nil || isComposite == nil || !isComposite(typeID) {
			continue
		}
		facts, err := layouts.Require(typeID)
		if err != nil {
			continue
		}
		align := facts.Align
		if align == 0 {
			align = 1
		}
		start, ok := alignUpChecked(cursor, align)
		if !ok {
			continue
		}
		end, ok := addChecked(start, facts.Size)
		if !ok {
			continue
		}
		plan.Offsets[i] = start
		cursor = end
		if align > plan.Align {
			plan.Align = align
		}
	}
	plan.Size = cursor
	return plan
}

// newArena creates zeroed storage for one activation of a plan.
func newArena(plan *StoragePlan, gen uint32) *Arena {
	if plan == nil || plan.Size == 0 {
		return &Arena{gen: gen}
	}
	return &Arena{bytes: make([]byte, plan.Size), gen: gen}
}

// Generation returns the arena's current generation.
func (a *Arena) Generation() uint32 {
	if a == nil {
		return 0
	}
	return a.gen
}

// retire invalidates every reference into the arena and releases its bytes.
//
// Retirement is refused while a pin is outstanding. That is what makes the
// generation check a real staleness signal rather than a race with the async
// path: a task state that pins a local keeps the frame — and therefore this
// arena — reachable, and the pin is released before the frame is retired.
func (a *Arena) retire() bool {
	if a == nil || a.pins != 0 {
		return false
	}
	a.gen++
	a.bytes = nil
	a.refs = nil
	a.refIndex = nil
	return true
}

func (a *Arena) pin()   { a.pins++ }
func (a *Arena) unpin() { a.pins-- }

// extent returns the bytes of one member, refusing anything the arena does not
// own. A refusal names the offset and length that were asked for, because the
// caller of a bad extent is always a projection whose arithmetic went wrong and
// the numbers are what identify it.
func (a *Arena) extent(offset, size uint64) ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("storage: no arena for extent at offset %d", offset)
	}
	end, ok := addChecked(offset, size)
	if !ok || end > uint64(len(a.bytes)) {
		return nil, fmt.Errorf("storage: extent [%d,+%d) is outside an arena of %d bytes", offset, size, len(a.bytes))
	}
	return a.bytes[offset:end], nil
}

// resolve returns the bytes a StorageRef names, checking the generation first.
func (r StorageRef) resolve(size uint64) ([]byte, error) {
	if r.Arena == nil {
		return nil, fmt.Errorf("storage: reference to type#%d has no arena", r.TypeID)
	}
	if r.Gen != r.Arena.gen {
		return nil, fmt.Errorf(
			"storage: stale reference to type#%d at offset %d (generation %d, arena is at %d)",
			r.TypeID, r.Offset, r.Gen, r.Arena.gen)
	}
	if r.Align != 0 && r.Offset%r.Align != 0 {
		return nil, fmt.Errorf(
			"storage: reference to type#%d at offset %d is not %d-byte aligned",
			r.TypeID, r.Offset, r.Align)
	}
	return r.Arena.extent(r.Offset, size)
}

// field returns the reference to one member at a relative offset, keeping the
// owner identity and the generation of the value it projects from.
func (r StorageRef) field(relative uint64, typeID types.TypeID, align uint64) (StorageRef, error) {
	offset, ok := addChecked(r.Offset, relative)
	if !ok {
		return StorageRef{}, fmt.Errorf("storage: field offset %d+%d overflows", r.Offset, relative)
	}
	return StorageRef{Arena: r.Arena, Offset: offset, TypeID: typeID, Gen: r.Gen, Align: align}, nil
}

// addRef records one referent and returns the slot encoding for it.
//
// A referent already in the table is reused rather than appended again. Without
// that, copying a value with a reference member in a loop would grow the table
// once per iteration even though the storage it describes never changes.
func (a *Arena) addRef(loc Location) (uint64, error) {
	if a == nil {
		return 0, fmt.Errorf("storage: no arena for a reference member")
	}
	if encoded, ok := a.refIndex[loc]; ok {
		return encoded, nil
	}
	a.refs = append(a.refs, loc)
	encoded := uint64(len(a.refs))
	if a.refIndex == nil {
		a.refIndex = make(map[Location]uint64, 4)
	}
	a.refIndex[loc] = encoded
	return encoded, nil
}

// refAt returns the referent a slot encoding names.
func (a *Arena) refAt(encoded uint64) (Location, bool) {
	if a == nil || encoded == 0 || encoded > uint64(len(a.refs)) {
		return Location{}, false
	}
	return a.refs[encoded-1], true
}

func alignUpChecked(value, align uint64) (uint64, bool) {
	if align <= 1 {
		return value, true
	}
	if align&(align-1) != 0 {
		return 0, false
	}
	rounded, ok := addChecked(value, align-1)
	if !ok {
		return 0, false
	}
	return rounded &^ (align - 1), true
}

func addChecked(a, b uint64) (uint64, bool) {
	sum := a + b
	if sum < a {
		return 0, false
	}
	return sum, true
}
