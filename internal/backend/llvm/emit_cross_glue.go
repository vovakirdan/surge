package llvm

import (
	"fmt"

	"surge/internal/types"
	"surge/internal/valueops"
)

// Generated crossing glue: the compiler's half of the typed-carrier cross ABI.
//
// A crossing is two calls, not one. `plan_cross` is a READ-ONLY preflight that
// reports what the transfer will newly own, so the sender can reserve before it
// gives up its source; the apply then re-derives the same answer and refuses any
// difference as PLAN_MISMATCH before the source commits. Both halves are frozen
// in `runtime/abi/typed_carrier_v2.json` and this file emits them.
//
// One descriptor carries ONE plan_cross that serves whichever modes its flags
// admit, and one apply body per admitted mode:
//
//   - MOVE transfers a single ownership obligation: the destination takes the
//     bits and the source is logically empty afterwards, which is byte-for-byte
//     what the local move_init does. It walks nothing and charges no sidecars.
//   - CLONE deep-copies. It is the local clone walk with one arm changed: a
//     counted scalar is DUPLICATED rather than retained, because the bignum
//     refcount is non-atomic and a block reachable from two shards is a race,
//     not a leak. A string is cloned and a handle retained exactly as the local
//     clone does; a nested composite recurses into its own cross-clone walk.
//
// Neither plan charges sidecars. The owner's ruling of 2026-08-29 found that
// pointer transport takes no per-message byte cost and the budget that exists is
// SLOTS, so the deep-copy allocations a clone makes are TARGET-OWNER memory from
// the moment they exist, not transport credit reached through the plan
// allocator. total_bytes is the payload alone and the allocator allowances start
// and end at zero. The frozen sidecar fields stay and are filled truthfully.

// Two namespaces, and the line between them is what a descriptor can name.
//
// The plan and BOTH applies are keyed on the EXACT registry type id, like
// move_init and unlike drop glue. A descriptor constant is named after the exact
// id, the plan a body writes is made of that entry's facts, and the apply checks
// the plan's `ops` against that very constant -- so a body keyed on the RESOLVED
// id would serve a descriptor whose constant is named after a different number
// and compare against a symbol nothing defines. That is not hypothetical: it is
// what the behaviour corpus found the first time a `byte[]` crossed.
//
// The clone WALK is keyed on the resolved id, like clone glue, because it reads
// nothing but layout and is shared by every entry that resolves to that layout;
// it takes no plan and checks nothing. A nested composite recurses into the walk,
// never into an apply, for the same reason.
func crossPlanName(id types.TypeID) string      { return fmt.Sprintf("plan_cross.type%d", id) }
func crossMoveName(id types.TypeID) string      { return fmt.Sprintf("cross_move.type%d", id) }
func crossCloneName(id types.TypeID) string     { return fmt.Sprintf("cross_clone.type%d", id) }
func crossCloneWalkName(id types.TypeID) string { return fmt.Sprintf("cross_clone_walk.type%d", id) }

// Field indices into `%struct.rt_cross_plan`, in the frozen record's order.
const (
	crossPlanFieldOps = iota
	crossPlanFieldMode
	crossPlanFieldEnvelopeBytes
	crossPlanFieldPayloadOffset
	crossPlanFieldPayloadBytes
	crossPlanFieldPayloadAlign
	crossPlanFieldSidecarBytes
	crossPlanFieldTotalBytes
	crossPlanFieldSidecarCount
)

// Field indices into `%struct.rt_cross_allocator`.
const (
	crossAllocFieldContext = iota
	crossAllocFieldAllocate
	crossAllocFieldRemainingBytes
	crossAllocFieldRemainingAllocations
)

// rt_carrier_status and rt_cross_mode values this file uses. The full
// enumerations are in the frozen manifest.
const (
	carrierStatusOK           = 0
	carrierStatusPlanMismatch = 5
	crossModeMove             = 0
	crossModeClone            = 1
)

// crossPlanEnvelopeBytes is what a TYPE charges for the transport envelope:
// nothing. The envelope is transport's own record and only transport knows its
// header size; a per-type body that guessed would invent a number the
// reservation then trusts.
const crossPlanEnvelopeBytes = 0

// requireCrossCloneGlue records that the resolved form of `id` needs a
// cross-clone WALK body and returns its name. The fixpoint (emitCrossGlue)
// drains what the recursion adds.
func (e *Emitter) requireCrossCloneGlue(id types.TypeID) string {
	id = resolveValueType(e.types, id)
	if e.crossCloneGlueNeeded == nil {
		e.crossCloneGlueNeeded = make(map[types.TypeID]struct{})
	}
	e.crossCloneGlueNeeded[id] = struct{}{}
	return crossCloneWalkName(id)
}

// emitCrossBodies writes the crossing bodies one descriptor entry needs: the
// mode-branching plan, the move apply if it is shard-movable, and the clone
// apply if it is cross-clonable. It runs in the descriptor pass's first loop, so
// the bodies exist before the constant that names them.
//
// The clone apply is emitted here, keyed on the entry's exact id; the walk it
// delegates to is DEMANDED, because the walk recurses and the fixpoint below is
// what follows that recursion.
func (e *Emitter) emitCrossBodies(entry *valueops.Entry, flags valueops.Flags) {
	movable := flags&valueops.FlagShardMovable != 0
	clonable := flags&valueops.FlagCrossClonable != 0
	if !movable && !clonable {
		return
	}
	e.emitCrossPlanBody(entry, movable, clonable)
	if movable {
		e.emitCrossMoveBody(entry)
	}
	if clonable {
		e.emitCrossCloneApply(entry)
	}
}

// emitCrossGlue drains the cross-clone walk demand, emitting each body and the
// nested bodies it recurses into. Same fixpoint the clone glue uses, because a
// composite arm asks for the walk of a nested composite.
func (e *Emitter) emitCrossGlue() error {
	done := make(map[types.TypeID]struct{})
	for {
		pending := takePendingGlue(e.crossCloneGlueNeeded, done)
		if len(pending) == 0 {
			return nil
		}
		for _, id := range pending {
			if err := e.emitCrossCloneWalkBody(id); err != nil {
				return err
			}
		}
	}
}

// emitCrossPlanBody emits the one read-only preflight, branching on mode and
// answering only for the modes this descriptor's flags admit.
//
// A mode the descriptor cannot serve does NOT return a status. The storage
// model is explicit that a call outside a descriptor's cross capability is a
// protocol violation by the caller, and that a status would assert the call was
// legal and merely declined. So an unadmitted mode traps, like the unavailable
// stub a non-cross descriptor binds.
func (e *Emitter) emitCrossPlanBody(entry *valueops.Entry, movable, clonable bool) {
	size := entry.Facts.Size
	align := entry.Facts.Align
	if align == 0 {
		align = 1
	}

	fmt.Fprintf(&e.buf, "define internal zeroext i32 @%s(ptr %%src, i8 zeroext %%mode, ptr %%out) {\nentry:\n",
		crossPlanName(entry.Type))
	if movable {
		fmt.Fprintf(&e.buf, "  %%ismove = icmp eq i8 %%mode, %d\n", crossModeMove)
		fmt.Fprintf(&e.buf, "  br i1 %%ismove, label %%serve, label %%tryclone\n")
		fmt.Fprintf(&e.buf, "tryclone:\n")
	}
	if clonable {
		fmt.Fprintf(&e.buf, "  %%isclone = icmp eq i8 %%mode, %d\n", crossModeClone)
		fmt.Fprintf(&e.buf, "  br i1 %%isclone, label %%serve, label %%badmode\n")
	} else {
		fmt.Fprintf(&e.buf, "  br label %%badmode\n")
	}

	fmt.Fprintf(&e.buf, "serve:\n")
	// Both admitted modes describe the same physical payload and charge no
	// sidecars; only the mode field differs, and it is the caller's own `mode`
	// rather than a constant, because one body serves both.
	e.storeCrossPlanPtr(crossPlanFieldOps, "@"+valueOpsSymbol(entry.Type))
	e.storeCrossPlanModeFromArg()
	e.storeCrossPlanWord(crossPlanFieldEnvelopeBytes, crossPlanEnvelopeBytes)
	e.storeCrossPlanWord(crossPlanFieldPayloadOffset, 0)
	e.storeCrossPlanWord(crossPlanFieldPayloadBytes, size)
	e.storeCrossPlanWord(crossPlanFieldPayloadAlign, align)
	e.storeCrossPlanWord(crossPlanFieldSidecarBytes, 0)
	e.storeCrossPlanWord(crossPlanFieldTotalBytes, size)
	e.storeCrossPlanWord(crossPlanFieldSidecarCount, 0)
	fmt.Fprintf(&e.buf, "  ret i32 %d\n", carrierStatusOK)

	fmt.Fprintf(&e.buf, "badmode:\n")
	fmt.Fprintf(&e.buf, "  call void @llvm.trap()\n")
	fmt.Fprintf(&e.buf, "  unreachable\n}\n\n")
}

func (e *Emitter) crossPlanFieldPtr(field int, name string) {
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds %%struct.rt_cross_plan, ptr %%out, i32 0, i32 %d\n",
		name, field)
}

func (e *Emitter) storeCrossPlanWord(field int, value uint64) {
	name := fmt.Sprintf("%%p%d", field)
	e.crossPlanFieldPtr(field, name)
	fmt.Fprintf(&e.buf, "  store i64 %d, ptr %s\n", value, name)
}

// storeCrossPlanModeFromArg writes the caller's own mode into the plan, so one
// body serves both admitted modes and the apply's mode check sees exactly what
// was asked for.
func (e *Emitter) storeCrossPlanModeFromArg() {
	name := fmt.Sprintf("%%p%d", crossPlanFieldMode)
	e.crossPlanFieldPtr(crossPlanFieldMode, name)
	fmt.Fprintf(&e.buf, "  store i8 %%mode, ptr %s\n", name)
}

func (e *Emitter) storeCrossPlanPtr(field int, value string) {
	name := fmt.Sprintf("%%p%d", field)
	e.crossPlanFieldPtr(field, name)
	fmt.Fprintf(&e.buf, "  store ptr %s, ptr %s\n", value, name)
}

// emitCrossMoveBody emits the move apply: check the plan, then transfer the
// bytes. The source is left as it is; emptying it is the caller's commit step.
func (e *Emitter) emitCrossMoveBody(entry *valueops.Entry) {
	size := entry.Facts.Size
	align := entry.Facts.Align
	if align == 0 {
		align = 1
	}

	fmt.Fprintf(&e.buf,
		"define internal zeroext i32 @%s(ptr %%dst, ptr %%src, ptr %%plan, ptr %%alloc) {\nentry:\n",
		crossMoveName(entry.Type))

	g := &glueTmp{}
	e.checkCommonCrossPlan(g, entry, crossModeMove, size, align)
	e.emitGlueStorageCopy("%dst", "%src", size, align)
	fmt.Fprintf(&e.buf, "  ret i32 %d\n", carrierStatusOK)
	fmt.Fprintf(&e.buf, "mismatch:\n")
	fmt.Fprintf(&e.buf, "  ret i32 %d\n}\n\n", carrierStatusPlanMismatch)
}

// emitCrossCloneApply emits the clone apply for one registry entry: check the
// plan against THIS entry's descriptor, then delegate the copy to the walk of
// its resolved layout. The apply is the only clone body that sees the plan, and
// the only one keyed on the exact id -- see the namespace note at the top.
//
// There is no rollback block. The only recoverable failure is PLAN_MISMATCH,
// which is answered before any copy; the deep copies themselves cannot
// recoverably fail, because allocator exhaustion is process-terminal and never a
// returned status. So the apply either mismatches before touching anything or
// succeeds.
func (e *Emitter) emitCrossCloneApply(entry *valueops.Entry) {
	size := entry.Facts.Size
	align := entry.Facts.Align
	if align == 0 {
		align = 1
	}

	fmt.Fprintf(&e.buf,
		"define internal zeroext i32 @%s(ptr %%dst, ptr %%src, ptr %%plan, ptr %%alloc) {\nentry:\n",
		crossCloneName(entry.Type))

	g := &glueTmp{}
	e.checkCommonCrossPlan(g, entry, crossModeClone, size, align)
	fmt.Fprintf(&e.buf, "  call void @%s(ptr %%dst, ptr %%src)\n", e.requireCrossCloneGlue(entry.Type))
	fmt.Fprintf(&e.buf, "  ret i32 %d\n", carrierStatusOK)
	fmt.Fprintf(&e.buf, "mismatch:\n")
	fmt.Fprintf(&e.buf, "  ret i32 %d\n}\n\n", carrierStatusPlanMismatch)
}

// emitCrossCloneWalkBody emits the clone walk for one resolved layout: memcpy
// the value, then fix up every counted-scalar leaf with a DEEP copy rather than
// a retain. It takes no plan and checks nothing, so a nested composite can call
// it with the field pointers alone, and the same body serves every registry
// entry that resolves to this layout.
//
// The layout must be finalized: a missing one is a builder bug, and it is
// returned as an error rather than papered over with an empty copy.
func (e *Emitter) emitCrossCloneWalkBody(id types.TypeID) error {
	layoutInfo, err := e.layoutOf(id)
	if err != nil {
		return err
	}
	align := layoutInfo.Align
	if align == 0 {
		align = 1
	}

	fmt.Fprintf(&e.buf, "define void @%s(ptr %%dst, ptr %%src) {\nentry:\n", crossCloneWalkName(id))
	e.emitGlueStorageCopy("%dst", "%src", layoutInfo.Size, align)
	// A value that owns no heap is finished by the byte copy: it has no counted
	// scalar to duplicate, no string to clone, no handle to retain, and so no
	// member to walk. Skipping the walk is not only cheaper -- it avoids asking
	// for aggregate field offsets a plain struct's layout need not carry, which
	// is the difference between a clone that memcpys and one that must fix up.
	if e.typeOwnsHeap(id) {
		g := &glueTmp{}
		if err := e.walkGlueValue(g, id, &layoutInfo, align, crossCloneWalk{e: e}); err != nil {
			return err
		}
	}
	fmt.Fprintf(&e.buf, "  ret void\n}\n\n")
	return nil
}

// checkCommonCrossPlan emits the plan checks both applies share: ops, mode,
// layout and the exact byte totals must agree, and both allocator allowances
// must be zero. Every check sits before the caller's copy, because a body that
// copied first and refused after would have written a destination its caller was
// told stayed empty.
func (e *Emitter) checkCommonCrossPlan(g *glueTmp, entry *valueops.Entry, mode, size, align uint64) {
	e.checkCrossPlanPtrField(g, crossPlanFieldOps, "@"+valueOpsSymbol(entry.Type))
	e.checkCrossPlanI8Field(g, crossPlanFieldMode, mode)
	e.checkCrossPlanWordField(g, crossPlanFieldPayloadBytes, size)
	e.checkCrossPlanWordField(g, crossPlanFieldPayloadAlign, align)
	e.checkCrossPlanWordField(g, crossPlanFieldSidecarBytes, 0)
	e.checkCrossPlanWordField(g, crossPlanFieldSidecarCount, 0)
	e.checkCrossAllocatorAllowance(g, crossAllocFieldRemainingBytes)
	e.checkCrossAllocatorAllowance(g, crossAllocFieldRemainingAllocations)
}

func (e *Emitter) checkCrossPlanWordField(g *glueTmp, field int, want uint64) {
	ptr := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds %%struct.rt_cross_plan, ptr %%plan, i32 0, i32 %d\n", ptr, field)
	got := g.next()
	fmt.Fprintf(&e.buf, "  %s = load i64, ptr %s\n", got, ptr)
	ok := g.next()
	fmt.Fprintf(&e.buf, "  %s = icmp eq i64 %s, %d\n", ok, got, want)
	next := fmt.Sprintf("agree%d", g.n)
	fmt.Fprintf(&e.buf, "  br i1 %s, label %%%s, label %%mismatch\n%s:\n", ok, next, next)
}

func (e *Emitter) checkCrossPlanI8Field(g *glueTmp, field int, want uint64) {
	ptr := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds %%struct.rt_cross_plan, ptr %%plan, i32 0, i32 %d\n", ptr, field)
	got := g.next()
	fmt.Fprintf(&e.buf, "  %s = load i8, ptr %s\n", got, ptr)
	ok := g.next()
	fmt.Fprintf(&e.buf, "  %s = icmp eq i8 %s, %d\n", ok, got, want)
	next := fmt.Sprintf("agree%d", g.n)
	fmt.Fprintf(&e.buf, "  br i1 %s, label %%%s, label %%mismatch\n%s:\n", ok, next, next)
}

func (e *Emitter) checkCrossPlanPtrField(g *glueTmp, field int, want string) {
	ptr := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds %%struct.rt_cross_plan, ptr %%plan, i32 0, i32 %d\n", ptr, field)
	got := g.next()
	fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s\n", got, ptr)
	ok := g.next()
	fmt.Fprintf(&e.buf, "  %s = icmp eq ptr %s, %s\n", ok, got, want)
	next := fmt.Sprintf("agree%d", g.n)
	fmt.Fprintf(&e.buf, "  br i1 %s, label %%%s, label %%mismatch\n%s:\n", ok, next, next)
}

func (e *Emitter) checkCrossAllocatorAllowance(g *glueTmp, field int) {
	ptr := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds %%struct.rt_cross_allocator, ptr %%alloc, i32 0, i32 %d\n", ptr, field)
	got := g.next()
	fmt.Fprintf(&e.buf, "  %s = load i64, ptr %s\n", got, ptr)
	ok := g.next()
	fmt.Fprintf(&e.buf, "  %s = icmp eq i64 %s, 0\n", ok, got)
	next := fmt.Sprintf("agree%d", g.n)
	fmt.Fprintf(&e.buf, "  br i1 %s, label %%%s, label %%mismatch\n%s:\n", ok, next, next)
}

// crossCloneWalk is the clone half of the shared member walk, differing from
// cloneWalk in exactly one arm: a counted scalar is DEEP-COPIED, not retained.
type crossCloneWalk struct{ e *Emitter }

func (crossCloneWalk) labelPrefix() string { return "xc" }

// The discriminant is read from the DESTINATION, already carried there by the
// memcpy, exactly as the local clone walk does.
func (crossCloneWalk) tagStorage() string { return "%dst" }

func (w crossCloneWalk) needsFixup(resolved types.TypeID) bool {
	return w.e.memberNeedsCloneFixup(resolved)
}

// leafAt fixes up a value that owns something directly. It is emitLeafCloneAt
// with the counted-scalar arm replaced: local clone RETAINS a shared block,
// crossing must DUPLICATE it, because the count is non-atomic and sharing it
// across shards is a race. The string and handle arms are identical to the
// local clone -- a string is single-owner bytes that get their own copy either
// way, and a channel handle's count is atomic and runtime-owned.
func (w crossCloneWalk) leafAt(g *glueTmp, resolved types.TypeID, baseAlign, off uint64) bool {
	e := w.e
	switch {
	case e.types.IsRefCountedScalar(resolved):
		// The one counted scalar today is WidthAny float, so the duplicate is
		// rt_bigfloat_clone: NULL-safe (NULL is the zero float and clones to
		// NULL), and its allocation is target-owner memory rather than a
		// transport sidecar. When int/uint join, this arm dispatches by which
		// counted scalar the leaf is; there is only one now.
		fp := g.next()
		fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%dst, i64 %d\n", fp, off)
		fv := g.next()
		fmt.Fprintf(&e.buf, "  %s = load ptr, ptr %s, align %d\n", fv, fp, memberAccessAlign(baseAlign, off))
		dup := g.next()
		fmt.Fprintf(&e.buf, "  %s = call ptr @rt_bigfloat_clone(ptr %s)\n", dup, fv)
		fmt.Fprintf(&e.buf, "  store ptr %s, ptr %s, align %d\n", dup, fp, memberAccessAlign(baseAlign, off))
		return true
	default:
		// Everything else the crossing duplicates exactly as the local clone
		// does, so it goes through the shared leaf and cannot drift from it.
		return e.emitLeafCloneAt(g, resolved, baseAlign, off)
	}
}

// compositeAt recurses into the nested composite's own CROSS clone walk, not
// its local one: a nested member's counted scalars must be duplicated too. It
// is the walk and never the apply, because the plan belongs to the whole value
// and a nested member has no descriptor of its own to check it against.
func (w crossCloneWalk) compositeAt(g *glueTmp, resolved types.TypeID, baseAlign, off uint64) {
	e := w.e
	dstField := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%dst, i64 %d\n", dstField, off)
	srcField := g.next()
	fmt.Fprintf(&e.buf, "  %s = getelementptr inbounds i8, ptr %%src, i64 %d\n", srcField, off)
	fmt.Fprintf(&e.buf, "  call void @%s(ptr %s, ptr %s)\n", e.requireCrossCloneGlue(resolved), dstField, srcField)
}
