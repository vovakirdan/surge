package llvm

import (
	"fmt"

	"surge/internal/layout"
	"surge/internal/types"
)

// Generated clone glue for value composites: the native half of the
// type-directed copy.
//
// It is written to parallel the drop glue next door, and the parallel is
// deliberate — the two walk the same members of the same layouts and disagreeing
// about which members those are is how a copy and a free stop matching. Where
// drop glue releases a member, clone glue gives the destination its own claim
// on it.
//
// The body is: MEMCPY the source's bytes over the destination's, then fix up
// only the members whose bits are not the whole value. That order matters. The
// memcpy carries every plain-bits field, the padding, and — for a union — the
// discriminant, so the fixups never have to know the layout of anything they do
// not themselves own, and a member added to a struct is copied correctly the
// day it is added rather than the day someone remembers to extend a switch.
//
// The destination is the CALLER'S storage, passed in. That is the shape of
// every operation on an inline value: the glue does not decide where the copy
// lives, because the copy lives wherever the assignment named. A body that
// allocated its own would be inventing an owner the language never asked for.
//
// Only two member shapes need a fixup, because only a COPY composite ever
// reaches here (`OperandCopyValue` is emitted for nothing else) and every
// member of a Copy composite is itself Copy:
//
//   - a reference-counted scalar: its copied word points into a counted block,
//     so the destination takes its own reference;
//   - a nested value composite: its own members may need the same treatment, so
//     it is cloned recursively into the destination's bytes for it.
//
// A string, dynamic array or map can never appear: none of them is Copy, so a
// composite holding one is not Copy either and is moved rather than copied.

func cloneGlueName(id types.TypeID) string { return fmt.Sprintf("clone.type%d", id) }

// requireCloneGlue records that `id` needs clone glue and returns its name.
// The emission fixpoint (emitCloneGlue) then walks what the recursion adds.
func (e *Emitter) requireCloneGlue(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.cloneGlueNeeded == nil {
		e.cloneGlueNeeded = make(map[types.TypeID]struct{})
	}
	e.cloneGlueNeeded[id] = struct{}{}
	return cloneGlueName(id)
}

// emitCloneGlue emits every requested clone body, re-checking the set after
// each pass because a body may request the glue of a nested composite. Same
// fixpoint the drop glue uses, for the same reason.
func (e *Emitter) emitCloneGlue() error {
	done := make(map[types.TypeID]struct{})
	for {
		progressed := false
		for _, id := range takePendingGlue(e.cloneGlueNeeded, done) {
			if err := e.emitCloneGlueBody(id); err != nil {
				return err
			}
			progressed = true
		}
		if !progressed {
			return nil
		}
	}
}

// emitCloneGlueBody emits `@clone.typeN(ptr %dst, ptr %src)`: make the value at
// %dst an INDEPENDENT copy of the one at %src. Both are storage the caller
// owns; null-safe on the source, so a source nobody wrote leaves the
// destination alone rather than reading bytes that are not there.
func (e *Emitter) emitCloneGlueBody(id types.TypeID) error {
	id = resolveValueType(e.types, id)
	layoutInfo, err := e.layoutOf(id)
	if err != nil {
		return err
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}

	fmt.Fprintf(&e.buf, "define void @%s(ptr %%dst, ptr %%src) {\n", cloneGlueName(id))
	fmt.Fprintf(&e.buf, "entry:\n")
	fmt.Fprintf(&e.buf, "  %%isnull = icmp eq ptr %%src, null\n")
	fmt.Fprintf(&e.buf, "  br i1 %%isnull, label %%ret, label %%body\n")
	fmt.Fprintf(&e.buf, "body:\n")
	e.emitGlueStorageCopy("%dst", "%src", layoutInfo.Size, align)
	e.emitCloneCopyCounter(layoutInfo.Size)

	g := &glueTmp{}
	if elem, length, ok := arrayFixedInfo(e.types, id); ok {
		e.emitFixedArrayElemClones(g, elem, align, int(length))
	} else if tt, ok := e.types.Lookup(id); ok {
		switch tt.Kind {
		case types.KindStruct:
			fields := e.types.StructFields(id)
			fieldOffsets, err := requireAggregateFieldOffsets(e.types, &layoutInfo, len(fields), "struct", id)
			if err != nil {
				return err
			}
			for i, f := range fields {
				e.emitFieldCloneAt(g, f.Type, align, fieldOffsets[i])
			}
		case types.KindTuple:
			if info, ok := e.types.TupleInfo(id); ok && info != nil {
				fieldOffsets, err := requireAggregateFieldOffsets(e.types, &layoutInfo, len(info.Elems), "tuple", id)
				if err != nil {
					return err
				}
				for i, el := range info.Elems {
					e.emitFieldCloneAt(g, el, align, fieldOffsets[i])
				}
			}
		case types.KindUnion:
			if err := e.emitUnionPayloadClones(g, id, &layoutInfo, align); err != nil {
				return err
			}
		}
	}

	fmt.Fprintf(&e.buf, "  br label %%ret\n")
	fmt.Fprintf(&e.buf, "ret:\n")
	fmt.Fprintf(&e.buf, "  ret void\n}\n")
	return nil
}

// emitGlueStorageCopy is the byte move inside a generated body. It states the
// alignment on both ends, like every other access through storage.
func (e *Emitter) emitGlueStorageCopy(dst, src string, size, align uint64) {
	if size == 0 {
		return
	}
	fmt.Fprintf(&e.buf,
		"  call void @llvm.memcpy.p0.p0.i64(ptr align %d %s, ptr align %d %s, i64 %d, i1 false)\n",
		align, dst, align, src, size)
}

// emitFieldCloneAt fixes up one member of the freshly copied destination at
// byte offset `off`. A member the byte copy already finished emits nothing.
func (e *Emitter) emitFieldCloneAt(g *glueTmp, fieldType types.TypeID, baseAlign, off uint64) {
	resolved := resolveValueType(e.types, fieldType)

	switch {
	case e.types.IsRefCountedScalar(resolved):
		// Immutable and counted: the copy shares the block and takes its own
		// reference. A deep copy would be waste, and it would also make two
		// values that must compare equal live at two addresses.
		fp := g.next()
		fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%dst, i64 %d\n", fp, off)
		fv := g.next()
		fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s, align %d\n", fv, fp, memberAccessAlign(baseAlign, off))
		e.emitGlueRetain(g, fv)

	case e.isCloneableComposite(resolved):
		// The byte copy carried this member's bits verbatim, which is right for
		// everything it owns outright and wrong for anything it shares. Cloning
		// it onto itself lets its own glue decide, member by member.
		dstField := g.next()
		fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%dst, i64 %d\n", dstField, off)
		srcField := g.next()
		fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%src, i64 %d\n", srcField, off)
		fmt.Fprintf(&e.buf, "  call void @%s(ptr %s, ptr %s)\n",
			e.requireCloneGlue(resolved), dstField, srcField)
	}
}

// emitFixedArrayElemClones fixes up each element of a copied fixed array.
func (e *Emitter) emitFixedArrayElemClones(g *glueTmp, elem types.TypeID, baseAlign uint64, length int) {
	if length <= 0 {
		return
	}
	resolved := resolveValueType(e.types, elem)
	if !e.types.IsRefCountedScalar(resolved) && !e.isCloneableComposite(resolved) {
		return
	}
	stride, err := e.arrayElemStride(elem)
	if err != nil {
		return
	}
	for i := range length {
		e.emitFieldCloneAt(g, elem, baseAlign, uint64(i)*stride)
	}
}

// emitUnionPayloadClones reads the discriminant — already carried over by the
// memcpy — and fixes up only the ACTIVE arm's payload. Walking every arm would
// read payload bytes that were never written for the arms that are not live.
func (e *Emitter) emitUnionPayloadClones(g *glueTmp, id types.TypeID, facts *layout.PhysicalFacts, baseAlign uint64) error {
	cases, err := e.tagCases(id)
	if err != nil {
		return err
	}
	type cloneCase struct {
		idx           int
		payloadOffset uint64
		offsets       []uint64
		payloadTys    []types.TypeID
	}
	var needsFixup []cloneCase
	for ci, c := range cases {
		if len(c.PayloadTypes) == 0 {
			continue
		}
		needsWork := false
		for _, pt := range c.PayloadTypes {
			resolved := resolveValueType(e.types, pt)
			if e.types.IsRefCountedScalar(resolved) || e.isCloneableComposite(resolved) {
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
		needsFixup = append(needsFixup, cloneCase{idx: ci, payloadOffset: caseLayout.PayloadOffset, offsets: offs, payloadTys: c.PayloadTypes})
	}
	if len(needsFixup) == 0 {
		return nil
	}

	tag := g.next()
	fmt.Fprintf(&e.buf, "  %s = load i32, ptr %%dst, align %d\n", tag, memberAccessAlign(baseAlign, 0))
	joinLabel := fmt.Sprintf("cl%d.join", g.n)
	fmt.Fprintf(&e.buf, "  switch i32 %s, label %%%s [", tag, joinLabel)
	for _, dc := range needsFixup {
		fmt.Fprintf(&e.buf, " i32 %d, label %%cl%d.c%d", dc.idx, g.n, dc.idx)
	}
	fmt.Fprintf(&e.buf, " ]\n")
	base := g.n
	for _, dc := range needsFixup {
		fmt.Fprintf(&e.buf, "cl%d.c%d:\n", base, dc.idx)
		for i, pt := range dc.payloadTys {
			e.emitFieldCloneAt(g, pt, baseAlign, dc.payloadOffset+dc.offsets[i])
		}
		fmt.Fprintf(&e.buf, "  br label %%%s\n", joinLabel)
	}
	fmt.Fprintf(&e.buf, "%s:\n", joinLabel)
	return nil
}

// emitGlueRetain bumps a reference count from inside generated glue. The
// funcEmitter's inline retain cannot be reused here: it allocates SSA names
// from the enclosing function's counter, and glue bodies have their own.
func (e *Emitter) emitGlueRetain(g *glueTmp, val string) {
	isNull := g.next()
	slot := g.next()
	count := g.next()
	bumped := g.next()
	fmt.Fprintf(&e.buf, "  %s = icmp eq ptr %s, null\n", isNull, val)
	fmt.Fprintf(&e.buf, "  %s = select i1 %s, ptr @%s, ptr %s\n", slot, isNull, retainScratchGlobal, val)
	fmt.Fprintf(&e.buf, "  %s = load i32, ptr %s\n", count, slot)
	fmt.Fprintf(&e.buf, "  %s = add i32 %s, 1\n", bumped, count)
	fmt.Fprintf(&e.buf, "  store i32 %s, ptr %s\n", bumped, slot)
}

// isCloneableComposite reports whether a member is a value whose own glue
// decides how it is duplicated. It asks the shared value-composite predicate,
// so the VM, the native backend and sema cannot drift on what counts as a
// value.
func (e *Emitter) isCloneableComposite(id types.TypeID) bool {
	if e == nil || e.types == nil || id == types.NoTypeID {
		return false
	}
	return e.hasInlineStorage(id)
}
