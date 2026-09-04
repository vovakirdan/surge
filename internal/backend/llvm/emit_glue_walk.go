package llvm

import (
	"fmt"

	"surge/internal/layout"
	"surge/internal/types"
)

// The member walk that generated bodies share.
//
// Clone glue and the crossing glue visit THE SAME members of the same layouts,
// at the same offsets, and pick a union's arm the same way. That is not tidiness.
// The storage model requires a crossing to re-derive its walk twice -- once
// read-only to size the transfer, once to perform it -- and refuses any
// difference in operations, layout, byte totals or allocation count as
// PLAN_MISMATCH before the source commits. Two hand-written walks satisfy that
// exactly until one of them is edited, which is the failure this file exists to
// make impossible: there is one enumeration and the bodies differ only in what
// they DO at a member.
//
// And they do differ. For a reference-counted scalar a local copy retains a
// shared block, while a crossing must deep-copy it -- the count is non-atomic,
// so a block reachable from two shards is a race rather than a leak. The walk is
// here; the per-member decision belongs to the caller through glueWalker.

// glueWalker is the per-member half of a type-directed walk. The walk supplies
// WHICH members and at WHAT offsets; the walker supplies what happens there.
type glueWalker interface {
	// labelPrefix keeps one walk's basic-block labels from colliding with
	// another's inside the same generated body.
	labelPrefix() string

	// tagStorage names the pointer a union's discriminant is read through. A
	// walk that has already byte-copied the value reads the destination, where
	// the memcpy carried the discriminant; a read-only walk reads the source.
	tagStorage() string

	// needsFixup reports whether a member is worth visiting at all. It is the
	// one list the arms and the "is there any work here" scans both read, so a
	// newly handled shape cannot leave a scan behind.
	needsFixup(resolved types.TypeID) bool

	// leafAt acts on a value that owns something DIRECTLY at byte offset `off`,
	// and answers whether it recognised the type. It must be reachable at
	// offset zero of a body whose own type is a leaf: a `Task<string>`'s result
	// is a bare string, not a struct holding one.
	leafAt(g *glueTmp, resolved types.TypeID, baseAlign, off uint64) bool

	// compositeAt acts on a nested value composite at byte offset `off`, which
	// in every current walker means recursing into that type's own body.
	compositeAt(g *glueTmp, resolved types.TypeID, baseAlign, off uint64)
}

// glueWalksInto reports whether a member is a value whose OWN generated body
// decides what happens to it, rather than one this body finishes itself.
//
// It asks the shared value-composite predicate, so the VM, the native backend
// and sema cannot drift on what counts as a value.
func (e *Emitter) glueWalksInto(id types.TypeID) bool {
	if e == nil || e.types == nil || id == types.NoTypeID {
		return false
	}
	return e.hasInlineStorage(id)
}

// walkGlueValue drives a walker over one whole value of type `id`.
//
// A type that owns something directly is finished at offset zero and the walk
// stops there; everything else is visited member by member. The caller emits the
// body's prologue and epilogue, because those differ between a void body and one
// that returns a status.
func (e *Emitter) walkGlueValue(
	g *glueTmp,
	id types.TypeID,
	facts *layout.PhysicalFacts,
	align uint64,
	w glueWalker,
) error {
	if w.leafAt(g, id, align, 0) {
		return nil
	}
	if elem, length, ok := arrayFixedInfo(e.types, id); ok {
		e.walkGlueFixedArray(g, elem, align, int(length), w)
		return nil
	}
	tt, ok := e.types.Lookup(id)
	if !ok {
		return nil
	}
	switch tt.Kind {
	case types.KindStruct:
		fields := e.types.StructFields(id)
		fieldOffsets, err := requireAggregateFieldOffsets(e.types, facts, len(fields), "struct", id)
		if err != nil {
			return err
		}
		for i, f := range fields {
			e.walkGlueMemberAt(g, f.Type, align, fieldOffsets[i], w)
		}
	case types.KindTuple:
		if info, ok := e.types.TupleInfo(id); ok && info != nil {
			fieldOffsets, err := requireAggregateFieldOffsets(e.types, facts, len(info.Elems), "tuple", id)
			if err != nil {
				return err
			}
			for i, el := range info.Elems {
				e.walkGlueMemberAt(g, el, align, fieldOffsets[i], w)
			}
		}
	case types.KindUnion:
		return e.walkGlueUnionPayload(g, id, facts, align, w)
	}
	return nil
}

// walkGlueMemberAt visits one member at byte offset `off`: a leaf if the walker
// recognises it, otherwise a nested composite whose own body decides. A member
// the walker recognises as neither emits nothing, because for it the bits ARE
// the value and the enclosing body already finished them.
func (e *Emitter) walkGlueMemberAt(
	g *glueTmp,
	memberType types.TypeID,
	baseAlign, off uint64,
	w glueWalker,
) {
	resolved := resolveValueType(e.types, memberType)
	if w.leafAt(g, resolved, baseAlign, off) {
		return
	}
	if e.glueWalksInto(resolved) {
		w.compositeAt(g, resolved, baseAlign, off)
	}
}

// walkGlueFixedArray visits each element of a fixed array. The element type
// decides once for every element, so an array of plain bits emits nothing at
// all rather than a loop of no-ops.
func (e *Emitter) walkGlueFixedArray(
	g *glueTmp,
	elem types.TypeID,
	baseAlign uint64,
	length int,
	w glueWalker,
) {
	if length <= 0 {
		return
	}
	resolved := resolveValueType(e.types, elem)
	if !w.needsFixup(resolved) {
		return
	}
	stride, err := e.arrayElemStride(elem)
	if err != nil {
		return
	}
	for i := range length {
		e.walkGlueMemberAt(g, elem, baseAlign, uint64(i)*stride, w)
	}
}

// walkGlueUnionPayload reads the discriminant and visits only the ACTIVE arm's
// payload. Walking every arm would read payload bytes that were never written
// for the arms that are not live.
func (e *Emitter) walkGlueUnionPayload(
	g *glueTmp,
	id types.TypeID,
	facts *layout.PhysicalFacts,
	baseAlign uint64,
	w glueWalker,
) error {
	cases, _, err := e.unionCases(id)
	if err != nil {
		return err
	}
	type walkCase struct {
		idx           int
		payloadOffset uint64
		offsets       []uint64
		payloadTys    []types.TypeID
	}
	var needsFixup []walkCase
	for _, c := range cases {
		if len(c.PayloadTypes) == 0 {
			continue
		}
		ci := c.PhysicalCaseIndex
		needsWork := false
		for _, pt := range c.PayloadTypes {
			if w.needsFixup(resolveValueType(e.types, pt)) {
				needsWork = true
				break
			}
		}
		if !needsWork {
			continue
		}
		caseLayout, ok := facts.UnionCase(ci)
		if !ok {
			return fmt.Errorf("missing finalized union case %d for type#%d", ci, id)
		}
		offs := caseLayout.FieldOffsets()
		if len(offs) != len(c.PayloadTypes) {
			return fmt.Errorf("finalized union case %d for type#%d has %d payload offsets, want %d", ci, id, len(offs), len(c.PayloadTypes))
		}
		needsFixup = append(needsFixup, walkCase{idx: ci, payloadOffset: caseLayout.PayloadOffset, offsets: offs, payloadTys: c.PayloadTypes})
	}
	if len(needsFixup) == 0 {
		return nil
	}

	prefix := w.labelPrefix()
	tag := g.next()
	fmt.Fprintf(&e.buf, "  %s = load i32, ptr %s, align %d\n", tag, w.tagStorage(), memberAccessAlign(baseAlign, 0))
	joinLabel := fmt.Sprintf("%s%d.join", prefix, g.n)
	fmt.Fprintf(&e.buf, "  switch i32 %s, label %%%s [", tag, joinLabel)
	for _, dc := range needsFixup {
		fmt.Fprintf(&e.buf, " i32 %d, label %%%s%d.c%d", dc.idx, prefix, g.n, dc.idx)
	}
	fmt.Fprintf(&e.buf, " ]\n")
	// The block id is fixed ONCE, before any per-member emission moves the SSA
	// counter, or the case labels drift from the switch targets above.
	base := g.n
	for _, dc := range needsFixup {
		fmt.Fprintf(&e.buf, "%s%d.c%d:\n", prefix, base, dc.idx)
		for i, pt := range dc.payloadTys {
			e.walkGlueMemberAt(g, pt, baseAlign, dc.payloadOffset+dc.offsets[i], w)
		}
		fmt.Fprintf(&e.buf, "  br label %%%s\n", joinLabel)
	}
	fmt.Fprintf(&e.buf, "%s:\n", joinLabel)
	return nil
}
