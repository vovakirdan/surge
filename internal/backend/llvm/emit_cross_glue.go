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
// THE MOVE HALF IS HERE; THE CLONE HALF IS NOT, and the asymmetry is the point
// rather than an unfinished edge. A move across a shard boundary transfers one
// ownership obligation: the destination takes the bits and the source is
// logically empty afterwards, which is byte-for-byte what the local `move_init`
// already does. It allocates nothing, so it needs no member walk and no
// sidecars. A cross CLONE is the opposite -- it must deep-copy every counted
// scalar, because the bignum refcount is NON-ATOMIC and a block reachable from
// two shards is a race rather than a leak -- and that one walks members, charges
// sidecars, and needs a runtime that can price a leaf before copying it.
//
// So the move half lands first and on its own. Its slot-claim protocol already
// exists (`RT_SLOT_CLAIM_CROSS_MOVE`, `rt_slot_cross_move_failed_locked`);
// the clone half's does not, and that is built with it.

// The three cross bodies are keyed on the EXACT registry type id, like
// move_init and unlike drop glue. Resolving here would let two registry entries
// carrying different physical facts collide on one body, and the plan a body
// writes is made of those facts.
func crossPlanName(id types.TypeID) string { return fmt.Sprintf("plan_cross.type%d", id) }
func crossMoveName(id types.TypeID) string { return fmt.Sprintf("cross_move.type%d", id) }

// Field indices into `%struct.rt_cross_plan`, in the frozen record's order.
// Named rather than spelled inline because a body that stores into the wrong
// index produces a plan the apply will refuse, and the refusal names neither
// field.
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

// rt_carrier_status values this file returns. The full enumeration is in the
// frozen manifest; these are the two a move can answer.
const (
	carrierStatusOK           = 0
	carrierStatusPlanMismatch = 5
	crossModeMove             = 0
)

// crossPlanEnvelopeBytes is what a TYPE charges for the transport envelope:
// nothing. The envelope is transport's own record and transport is the only
// thing that knows how big its header is; a per-type body that guessed would be
// inventing a number the reservation then trusts. So the plan a type writes
// describes the type -- payload offset, bytes and alignment, and the sidecars
// the apply will allocate -- and `total_bytes` is that payload alone. If
// transport later needs its header charged, it adds it to a plan it received
// rather than asking each type to know it.
//
// Nothing gates on these bytes today in any case: the owner's ruling of
// 2026-08-29 found that pointer transport carries no per-message byte cost and
// the budget that exists is SLOTS. The fields stay because the record is frozen
// and they must be filled truthfully, not because a reservation reads them.
const crossPlanEnvelopeBytes = 0

// emitCrossMoveBodies emits `plan_cross.typeN` and `cross_move.typeN` for one
// registry entry. The caller decides which entries get them; this only writes
// the two bodies.
func (e *Emitter) emitCrossMoveBodies(entry *valueops.Entry) {
	e.emitCrossPlanBody(entry)
	e.emitCrossMoveBody(entry)
}

// emitCrossPlanBody emits the read-only preflight.
//
// A move charges no sidecars: it allocates nothing, so `sidecar_bytes` and
// `sidecar_count` are zero and the apply's allowances start and end at zero.
// The payload is the value's own exact layout.
//
// A mode this descriptor cannot serve does NOT return a status. The storage
// model is explicit that a call outside a descriptor's cross capability is a
// protocol violation by the caller rather than a recoverable refusal, and that
// giving it a status would assert the call was legal and merely declined. So it
// takes the same treatment as the unavailable stub: it does not return.
func (e *Emitter) emitCrossPlanBody(entry *valueops.Entry) {
	size := entry.Facts.Size
	align := entry.Facts.Align
	if align == 0 {
		align = 1
	}

	fmt.Fprintf(&e.buf, "define internal zeroext i32 @%s(ptr %%src, i8 zeroext %%mode, ptr %%out) {\nentry:\n",
		crossPlanName(entry.Type))
	fmt.Fprintf(&e.buf, "  %%ismove = icmp eq i8 %%mode, %d\n", crossModeMove)
	fmt.Fprintf(&e.buf, "  br i1 %%ismove, label %%plan, label %%badmode\n")
	fmt.Fprintf(&e.buf, "plan:\n")

	e.storeCrossPlanPtr(crossPlanFieldOps, "@"+valueOpsSymbol(entry.Type))
	e.storeCrossPlanI8(crossPlanFieldMode, crossModeMove)
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

func (e *Emitter) storeCrossPlanI8(field int, value uint64) {
	name := fmt.Sprintf("%%p%d", field)
	e.crossPlanFieldPtr(field, name)
	fmt.Fprintf(&e.buf, "  store i8 %d, ptr %s\n", value, name)
}

func (e *Emitter) storeCrossPlanPtr(field int, value string) {
	name := fmt.Sprintf("%%p%d", field)
	e.crossPlanFieldPtr(field, name)
	fmt.Fprintf(&e.buf, "  store ptr %s, ptr %s\n", value, name)
}

// emitCrossMoveBody emits the apply.
//
// It re-derives what the plan claims and refuses a disagreement BEFORE touching
// the source, which is the frozen contract: ops, mode, layout and the exact
// byte totals must match, and success requires both allocator allowances to be
// zero. For a move all of that is a comparison against constants this same
// compiler wrote into the plan, so a mismatch means the plan came from another
// descriptor or another mode -- exactly the confusion the check exists to
// catch.
//
// The transfer itself is the byte move `move_init` performs. The source is left
// as it is: emptying it is the caller's commit step, and the storage model puts
// that after destination commit rather than inside this body.
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
	e.checkCrossPlanPtrField(g, crossPlanFieldOps, "@"+valueOpsSymbol(entry.Type))
	e.checkCrossPlanI8Field(g, crossPlanFieldMode, crossModeMove)
	e.checkCrossPlanWordField(g, crossPlanFieldPayloadBytes, size)
	e.checkCrossPlanWordField(g, crossPlanFieldPayloadAlign, align)
	e.checkCrossPlanWordField(g, crossPlanFieldSidecarBytes, 0)
	e.checkCrossPlanWordField(g, crossPlanFieldSidecarCount, 0)
	e.checkCrossAllocatorAllowance(g, crossAllocFieldRemainingBytes)
	e.checkCrossAllocatorAllowance(g, crossAllocFieldRemainingAllocations)

	e.emitGlueStorageCopy("%dst", "%src", size, align)
	fmt.Fprintf(&e.buf, "  ret i32 %d\n", carrierStatusOK)
	fmt.Fprintf(&e.buf, "mismatch:\n")
	fmt.Fprintf(&e.buf, "  ret i32 %d\n}\n\n", carrierStatusPlanMismatch)
}

// checkCrossPlanWordField refuses unless the plan's field holds `want`.
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

// checkCrossAllocatorAllowance refuses unless the allowance is already zero. A
// move consumes nothing, so anything else means the plan was built for a
// different operation.
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
