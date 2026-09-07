package mir

import (
	"errors"
	"fmt"
	"strings"

	"surge/internal/sema"
	"surge/internal/types"
)

// A relinquishing sink is a position where this function gives a value up to
// another thread: a field of a crossing's capture state, a far-select SEND
// payload, the `ret` of a crossing or blocking body. The runtime consumes what
// it is handed there on every path, and the receiving thread releases it
// under a count that is not atomic — so a value that may share a counted
// block with a holder left behind here must arrive PRIVATE, and the
// instruction that made it private is InstrUnshare.
//
// The same instruction carries a SECOND obligation, which is not about privacy
// at all: a value that carries a DYNAMIC ARRAY must be shown to the runtime,
// because only the view registry knows whether that array is a view into a
// buffer this shard keeps reading, or a base a live view still reads. An
// `int[]` shares no count and so was never named by the first reason, and a
// view of one crossed and was written through from the other thread. Both
// reasons are asked by one predicate, needsRelinquishWalkIn, and either one
// makes a sink subject to the two rules below.
//
// Two rules, checked at two times, because the async split moves them apart.
//
// The ACT — "the un-share is the last thing that touched the operand's local
// before the sink" — is checked by validateRelinquishedOperandsArePrivate,
// which the lowering runs on every function before SimplifyCFG and before the
// async state machine. It is a same-block rule on purpose: every lowering site
// emits the un-share immediately before its sink, and a block boundary between
// them would be a lowering change worth a red. It cannot run later: the split
// (async_normalize.go) leaves the crossing ALONE in a resume block that two
// Goto edges enter — the prelude that ends with the un-share, and the entry
// dispatch's case block (async_codegen.go buildAsyncPollEntry), which unpacks
// the packed locals and never names the private temp, because the temp is not
// live past the crossing and so is never packed. "Every predecessor ends with
// the un-share" is therefore false on every async far-select, and a rule that
// skipped predecessors that never touch the local would pass a path on which
// the local is defined further up and shared.
//
// The SHAPE — "a may-share operand is a constant or a MOVE out of a bare
// local" — has no topology and is checked by validateRelinquishedOperandShapes
// from the structural validator after the split. It is what makes the two
// operand kinds a boundary must never see a validator error by name: a COPY
// there is the far-select aliasing bug (a bare alias of a live binding whose
// reference the runtime then consumes), and a RETAIN is the retain trap (the
// emitters take a sink operand's ADDRESS and never materialize the retain).

// mayShareCountedBlockIn asks sema's owned-move question with only an interner
// in hand, which is all the lowering and the validator have. The predicate
// reads nothing but the interner, so a result wrapping it is the same walk
// (R5: one walk, not a copy of it in this package); a free function over the
// interner is sema's to offer.
func mayShareCountedBlockIn(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	return (&sema.Result{TypeInterner: typesIn}).MayShareCountedBlock(id)
}

// needsRelinquishWalkIn is the same wrapping for the question every sink but
// one actually asks: may this value share a counted block, OR does it carry a
// dynamic array whose header only the runtime can classify. It is what the
// lowering gates on and what both validators below check, so the instruction
// and the rules about it cannot disagree on which values are subject.
func needsRelinquishWalkIn(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	return (&sema.Result{TypeInterner: typesIn}).NeedsRelinquishWalk(id)
}

// dynamicArrayIn asks whether this value IS a dynamic array — a header and a
// buffer the body owes a drop for — rather than whether one is reachable from
// it. The anchored send's give-away is the one place that wants the narrow
// question: it hands a whole capture to the ring, and a value that merely
// CONTAINS an array (a struct field, a map's table) is not a capture this gate
// opened.
func dynamicArrayIn(typesIn *types.Interner, id types.TypeID) bool {
	if typesIn == nil || id == types.NoTypeID {
		return false
	}
	_, isArray := typesIn.DynamicArrayElem(id)
	return isArray
}

// bareLocalOf reports the local a place names directly: no projection, no
// global.
func bareLocalOf(p Place) (LocalID, bool) {
	if p.Kind != PlaceLocal || len(p.Proj) != 0 || p.Local == NoLocalID {
		return NoLocalID, false
	}
	return p.Local, true
}

// residentPlace reports whether a place is a RESIDENT field of an async
// frame: the storage a bare local was rewritten into by the split because a
// live child borrows it (async_resident_places.go). The lowering un-shared
// the bare local and moved out of it; after the split both name this one
// field, so the post-split shape rule reads it as the same bare local.
func residentPlace(p Place) bool {
	return p.Kind == PlaceLocal && len(p.Proj) == 1 && p.Proj[0].Kind == PlaceProjField &&
		strings.HasPrefix(p.Proj[0].FieldName, asyncResidentFieldPrefix)
}

type relinquishSink struct {
	block BlockID
	// instr indexes the sink within its block; len(Instrs) names the
	// terminator, which is where the scan for the act starts either way.
	instr int
	op    *Operand
	what  string
	// privateByProvenance marks a sink whose act is NOT an un-share in this
	// function but the origin of the local: an anchored body's send gives away
	// a capture the CALLER made private when it entered the state, and the
	// body's replayed prefix may not touch the value at all — not even to read
	// its count (anchoredSendGivenAway). The act checked for such a sink is
	// that the local's only touch before the sink is the unpack from the
	// state, and that no un-share was emitted on it.
	privateByProvenance bool
}

// relinquishSinks lists every position in f whose operand is consumed on
// another thread.
func relinquishSinks(f *Func) []relinquishSink {
	var out []relinquishSink
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			switch ins.Kind {
			case InstrCrossing:
				for fi := range ins.Crossing.State.Fields {
					// Field 0 is the frame state word; capture i is field i+1.
					// The block's anchor is leased, not given up: the caller
					// keeps the handle and the body borrows it, so it is not a
					// sink (relinquishCapture leaves it alone for the same
					// reason).
					if ci := fi - 1; ci >= 0 && ci < len(ins.Crossing.Captures) &&
						ins.Crossing.Captures[ci].Mode == sema.CrossingCaptureAnchorLease {
						continue
					}
					field := &ins.Crossing.State.Fields[fi]
					out = append(out, relinquishSink{block: BlockID(bi), instr: ii, op: &field.Value, what: "state field " + field.Name})
				}
				for oi := range ins.Crossing.RemoteOps {
					op := &ins.Crossing.RemoteOps[oi]
					if op.Method != "send" {
						continue
					}
					out = append(out, relinquishSink{block: BlockID(bi), instr: ii, op: &op.Value, what: fmt.Sprintf("remote op %d send payload", oi)})
				}
			case InstrBlocking:
				for fi := range ins.Blocking.State.Fields {
					field := &ins.Blocking.State.Fields[fi]
					out = append(out, relinquishSink{block: BlockID(bi), instr: ii, op: &field.Value, what: "blocking state field " + field.Name})
				}
			case InstrCall:
				// The anchored body's send: the ring takes the payload's bits
				// and the receiving shard may hold them before this body ends
				// (anchoredSendGivenAway).
				if ins.Call.Callee.Kind == CalleeValue && ins.Call.Callee.Name == "rt_anchored_channel_send" &&
					len(ins.Call.Args) == 1 {
					out = append(out, relinquishSink{
						block: BlockID(bi), instr: ii, op: &ins.Call.Args[0],
						what: "anchored send payload", privateByProvenance: true,
					})
				}
			}
		}
		if !f.ResultCrossesThreads {
			continue
		}
		switch bb.Term.Kind {
		case TermReturn:
			if bb.Term.Return.HasValue {
				out = append(out, relinquishSink{block: BlockID(bi), instr: len(bb.Instrs), op: &bb.Term.Return.Value, what: "return value"})
			}
		case TermAsyncReturn:
			if bb.Term.AsyncReturn.HasValue {
				out = append(out, relinquishSink{block: BlockID(bi), instr: len(bb.Instrs), op: &bb.Term.AsyncReturn.Value, what: "async return value"})
			}
		}
	}
	return out
}

// sinkOperandType is the operand's own type, or the local's when the operand
// carries none — the rewrite that closes a crossing body's returns builds its
// operands from places alone.
func sinkOperandType(f *Func, op *Operand) types.TypeID {
	if op.Type != types.NoTypeID || op.Kind == OperandConst {
		return op.Type
	}
	if local, ok := bareLocalOf(op.Place); ok && int(local) < len(f.Locals) {
		return f.Locals[local].Type
	}
	return types.NoTypeID
}

// relinquishedLocal applies the shape rule to one sink operand: a constant is
// minted at the site and needs nothing; anything else must be a MOVE out of a
// bare local, which is then the local whose act the caller looks for. After
// the async split the bare local may have become a resident field of the
// frame; that is the same storage under another name, and the act was
// already checked before the split, so the post-split caller accepts it and
// gets no local back.
func relinquishedLocal(s *relinquishSink, ctx string) (LocalID, bool, error) {
	if s.op.Kind == OperandConst {
		return NoLocalID, false, nil
	}
	if s.op.Kind == OperandMove && residentPlace(s.op.Place) {
		return NoLocalID, false, nil
	}
	local, ok := bareLocalOf(s.op.Place)
	if s.op.Kind != OperandMove || !ok {
		return NoLocalID, false, fmt.Errorf("%s: %s reaches the boundary as %s %s; a value that may share a counted "+
			"block, or that carries a dynamic array the runtime must inspect, is handed over only as a MOVE "+
			"out of a bare local walked first",
			ctx, s.what, s.op.Kind, formatPlace(s.op.Place))
	}
	return local, true, nil
}

func relinquishSinkContext(s *relinquishSink, f *Func) string {
	if s.instr == len(f.Blocks[s.block].Instrs) {
		return fmt.Sprintf("bb%d terminator", s.block)
	}
	return fmt.Sprintf("bb%d instr %d", s.block, s.instr)
}

// sinkIsSubject reports whether the two rules apply to this sink's operand.
//
// Every sink but one asks the full question. The exception is the anchored
// body's send, and the honest account of it is that this sink is only PARTLY
// guarded for arrays — not that its array was checked somewhere else.
//
// Why the rules cannot be asked here: relinquishOperand does not serve this
// sink and must not, because the body's prefix replays from its first
// instruction on every wake, so a walk emitted before the send would run again
// over storage the ring already owns. There is no instruction that could
// satisfy the act rule at this sink, so asking the widened question would turn
// every array anchored send into a validator's error rather than a diagnostic.
//
// WHAT IS GUARDED, and where. Sema holds the payload of an anchored send to
// `own <captured binding>` (checkAnchoredSendGivesThePayloadAway) when the
// element may share a counted block, and now also when the element IS a dynamic
// array and the payload names a binding the crossing captured as one; the
// lowering that gives the capture's own reference away (anchoredSendGivenAway)
// recognizes the same two. So an array capture reaches the ring as the body's
// only reference, with the body's drop withheld, exactly as a counted capture
// does. The act is not re-checked here, because this predicate reads a type
// while the array half is also a fact about the BINDING; the shape is by
// construction, the lowering building the operand as a move out of the
// capture's local.
//
// WHAT IS STILL OPEN, stated rather than argued away: a payload that names no
// captured array — a literal built inside the body, a window sliced out of a
// `@shard_movable` capture's field, or a whole struct capture that merely
// CONTAINS an array — is held to no shape and takes no walk on either thread.
// All three crash, and all three crashed the same way before the capture gate
// admitted a bare array, so this is the sink's older debt (RV2-DEBT-349), not
// the widening's.
func sinkIsSubject(s *relinquishSink, typesIn *types.Interner, id types.TypeID) bool {
	if s.privateByProvenance {
		return mayShareCountedBlockIn(typesIn, id)
	}
	return needsRelinquishWalkIn(typesIn, id)
}

// validateRelinquishedOperandShapes is the post-split half: every subject sink
// operand is a constant or a MOVE out of a bare local.
func validateRelinquishedOperandShapes(f *Func, typesIn *types.Interner) error {
	if f == nil {
		return nil
	}
	var errs []error
	for _, s := range relinquishSinks(f) {
		if !sinkIsSubject(&s, typesIn, sinkOperandType(f, s.op)) {
			continue
		}
		if _, _, err := relinquishedLocal(&s, relinquishSinkContext(&s, f)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// validateRelinquishedOperandsArePrivate is the pre-split half: the shape
// rule, and then the act — scanning the sink's block backwards, the first
// instruction that touches the moved local is an un-share of exactly that
// local. A later write into it, a retain read of it into another temp, or no
// un-share at all is a value that reaches the boundary still shared.
func validateRelinquishedOperandsArePrivate(f *Func, typesIn *types.Interner) error {
	if f == nil {
		return nil
	}
	var errs []error
	for _, s := range relinquishSinks(f) {
		ty := sinkOperandType(f, s.op)
		if !sinkIsSubject(&s, typesIn, ty) {
			continue
		}
		ctx := relinquishSinkContext(&s, f)
		local, need, err := relinquishedLocal(&s, ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !need {
			continue
		}
		if s.privateByProvenance {
			if !unpackedFromStateBeforeSink(f, &f.Blocks[s.block], s.instr, local) {
				errs = append(errs, fmt.Errorf("%s: %s (L%d, %s) reaches the ring from a local that is not the "+
					"capture's own unpack from the state, or was touched between the unpack and the send; "+
					"the body's prefix replays and may not touch the value",
					ctx, s.what, local, types.Label(typesIn, ty)))
			}
			continue
		}
		if unsharedBeforeSink(&f.Blocks[s.block], s.instr, local) {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: %s (L%d, %s) reaches the boundary without an un-share in its block; "+
			"it may share a counted block, or it carries a dynamic array the runtime must inspect",
			ctx, s.what, local, types.Label(typesIn, ty)))
	}
	return errors.Join(errs...)
}

// stateLocalName is the local a crossing or blocking body's poll function
// unpacks its captures from (lowerSpawnOnPollFunc, lowerBlockingFunc).
const stateLocalName = "__state"

// unpackedFromStateBeforeSink is the act for a sink private by provenance:
// scanning the sink's block backwards, the first instruction that touches
// local is the unpack of a state field into it — an assignment from a field of
// the `__state` local — and nothing on the way is an un-share of it. The
// unpack is the entry block's first business, so the same block holds both.
func unpackedFromStateBeforeSink(f *Func, bb *Block, sink int, local LocalID) bool {
	for i := sink - 1; i >= 0; i-- {
		ins := &bb.Instrs[i]
		if ins.Kind == InstrUnshare {
			if got, ok := bareLocalOf(ins.Unshare.Place); ok && got == local {
				return false
			}
		}
		if !instrTouchesLocal(ins, local) {
			continue
		}
		if ins.Kind != InstrAssign || ins.Assign.Src.Kind != RValueField {
			return false
		}
		dst, ok := bareLocalOf(ins.Assign.Dst)
		if !ok || dst != local {
			return false
		}
		state, ok := bareLocalOf(ins.Assign.Src.Field.Object.Place)
		return ok && int(state) < len(f.Locals) && f.Locals[state].Name == stateLocalName
	}
	return false
}

// unsharedBeforeSink reports whether the last instruction before index sink
// that touches local is `unshare local`.
func unsharedBeforeSink(bb *Block, sink int, local LocalID) bool {
	for i := sink - 1; i >= 0; i-- {
		ins := &bb.Instrs[i]
		if ins.Kind == InstrUnshare {
			if got, ok := bareLocalOf(ins.Unshare.Place); ok && got == local {
				return true
			}
		}
		if instrTouchesLocal(ins, local) {
			return false
		}
	}
	return false
}

// instrTouchesLocal reports whether any place the instruction names is rooted
// at local or indexes by it. A walk the place oracle cannot perform counts as
// a touch: the rule fails closed rather than looking past what it cannot see.
func instrTouchesLocal(ins *Instr, local LocalID) bool {
	touched := false
	err := instrPlaces(ins, func(p *Place) {
		if p.Kind == PlaceLocal && p.Local == local {
			touched = true
		}
		for _, proj := range p.Proj {
			if proj.Kind == PlaceProjIndex && proj.IndexLocal == local {
				touched = true
			}
		}
	})
	return touched || err != nil
}
