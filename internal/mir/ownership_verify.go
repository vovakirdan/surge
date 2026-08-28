package mir

import (
	"surge/internal/sema"
	"surge/internal/types"
)

// OwnershipSinkKind names the consuming position a finding was raised at.
type OwnershipSinkKind uint8

const (
	// OwnershipSinkDrop is a whole, unguarded release of a place.
	OwnershipSinkDrop OwnershipSinkKind = iota
	// OwnershipSinkEnvelopeRelease frees a synthesized-after-sema heap box.
	OwnershipSinkEnvelopeRelease
	// OwnershipSinkReturn hands a value out of the function.
	OwnershipSinkReturn
	// OwnershipSinkAsyncState hands a suspended or cancelled state envelope
	// back to the runtime.
	OwnershipSinkAsyncState
	// OwnershipSinkCallArg is a call position the callee owns or stores.
	OwnershipSinkCallArg
	// OwnershipSinkUnresolvedContract is a call position the lowering could not
	// classify at all, which is a finding on its own terms.
	OwnershipSinkUnresolvedContract
	// OwnershipSinkAggregateField is a field or element of a literal container.
	OwnershipSinkAggregateField
	// OwnershipSinkProjectedAssign writes into a place inside a live container.
	OwnershipSinkProjectedAssign
	// OwnershipSinkCrossingCapture becomes a field of a synthesized state that
	// outlives this function.
	OwnershipSinkCrossingCapture
	// OwnershipSinkCrossingReceiver is a crossing receiver the crossing
	// consumes.
	OwnershipSinkCrossingReceiver
	// OwnershipSinkCrossingRemoteValue is a remote select send arm's payload.
	OwnershipSinkCrossingRemoteValue
	// OwnershipSinkChanSend queues a value in a channel.
	OwnershipSinkChanSend
	// OwnershipSinkSelectSend queues a value in a channel from a select arm.
	OwnershipSinkSelectSend
	// OwnershipSinkGlobalAssign stores a value in a bare global place.
	OwnershipSinkGlobalAssign
	// OwnershipSinkTaskConsume is the task handle consumed by await, or by the
	// ready edge of poll/timeout. Pending retains the handle for the retry.
	OwnershipSinkTaskConsume
)

func (k OwnershipSinkKind) String() string {
	switch k {
	case OwnershipSinkDrop:
		return "drop"
	case OwnershipSinkEnvelopeRelease:
		return "envelope_release"
	case OwnershipSinkReturn:
		return "return"
	case OwnershipSinkAsyncState:
		return "async_state"
	case OwnershipSinkCallArg:
		return "call_arg"
	case OwnershipSinkUnresolvedContract:
		return "unresolved_contract"
	case OwnershipSinkAggregateField:
		return "aggregate_field"
	case OwnershipSinkProjectedAssign:
		return "projected_assign"
	case OwnershipSinkCrossingCapture:
		return "crossing_capture"
	case OwnershipSinkCrossingReceiver:
		return "crossing_receiver"
	case OwnershipSinkCrossingRemoteValue:
		return "crossing_remote_value"
	case OwnershipSinkChanSend:
		return "chan_send"
	case OwnershipSinkSelectSend:
		return "select_send"
	case OwnershipSinkGlobalAssign:
		return "global_assign"
	case OwnershipSinkTaskConsume:
		return "task_consume"
	default:
		return "unknown"
	}
}

// VerifyOwnership walks a module's MIR and reports every consuming sink whose
// value it could not establish, recursively through any transfers, as freshly
// minted or owned at entry.
//
// It is REPORT-ONLY by contract: it reads, it returns findings, and it must
// leave the module byte-identical. Nothing here writes to any MIR node — every
// operand it needs to adjust (an untyped one) it adjusts on a copy.
func VerifyOwnership(m *Module, typesIn *types.Interner, semaRes *sema.Result) []OwnershipFinding {
	if m == nil {
		return nil
	}
	ids := m.SortedFuncIDs()
	out := make([]OwnershipFinding, 0, len(ids))
	for _, id := range ids {
		f := m.Funcs[id]
		if f == nil {
			continue
		}
		v := &ownershipFuncVerifier{
			f:       f,
			globals: m.Globals,
			typesIn: typesIn,
			semaRes: semaRes,
			defs:    newReachingDefs(f),
		}
		out = append(out, v.verify()...)
	}
	return out
}

func (v *ownershipFuncVerifier) verify() []OwnershipFinding {
	var out []OwnershipFinding
	for bi := range v.f.Blocks {
		block := &v.f.Blocks[bi]
		for ii := range block.Instrs {
			at := ownershipPoint{Block: BlockID(bi), Instr: ii}
			out = append(out, v.checkInstr(&block.Instrs[ii], at)...)
		}
		termAt := ownershipPoint{Block: BlockID(bi), Instr: len(block.Instrs)}
		out = append(out, v.checkTerm(&block.Term, termAt)...)
	}
	return out
}

func (v *ownershipFuncVerifier) checkInstr(ins *Instr, at ownershipPoint) []OwnershipFinding {
	switch ins.Kind {
	case InstrDrop:
		return v.checkReleasedPlace(ins.Drop.Place, ins.Drop.Shallow, at, OwnershipSinkDrop, "place")
	case InstrEnvelopeRelease:
		return v.checkReleasedPlace(ins.EnvelopeRelease.Place, false, at, OwnershipSinkEnvelopeRelease, "place")
	case InstrAwait:
		return v.checkTaskConsume(&ins.Await.Task, at)
	case InstrPoll:
		return v.checkTaskConsume(&ins.Poll.Task, at)
	case InstrTimeout:
		return v.checkTaskConsume(&ins.Timeout.Task, at)
	case InstrCall:
		return v.checkCallArgs(&ins.Call, at)
	case InstrAssign:
		return v.checkAssign(&ins.Assign, at)
	case InstrCrossing:
		return v.checkCrossing(&ins.Crossing, at)
	case InstrBlocking:
		return v.checkAggregateFields(ins.Blocking.State.Fields, nil, "state", at)
	case InstrChanSend:
		return v.checkOperandSink(&ins.ChanSend.Value, at, OwnershipSinkChanSend, "value")
	case InstrSelect:
		var out []OwnershipFinding
		for i := range ins.Select.Arms {
			arm := &ins.Select.Arms[i]
			if arm.Kind != SelectArmChanSend {
				continue
			}
			out = append(out, v.checkOperandSink(&arm.Value, at, OwnershipSinkSelectSend,
				indexedOwnershipPosition("arm", i)+".value")...)
		}
		return out
	}
	return nil
}

func (v *ownershipFuncVerifier) checkTerm(term *Terminator, at ownershipPoint) []OwnershipFinding {
	switch term.Kind {
	case TermReturn:
		if !term.Return.HasValue || !ownsHeapFor(v.typesIn, v.semaRes, v.f.Result) {
			return nil
		}
		return v.checkOperandSink(&term.Return.Value, at, OwnershipSinkReturn, "value")
	case TermAsyncReturn:
		if !term.AsyncReturn.HasValue || !ownsHeapFor(v.typesIn, v.semaRes, v.f.Result) {
			return nil
		}
		return v.checkOperandSink(&term.AsyncReturn.Value, at, OwnershipSinkReturn, "value")
	case TermAsyncYield:
		return v.checkOperandSink(&term.AsyncYield.State, at, OwnershipSinkAsyncState, "state")
	case TermAsyncReturnCancelled:
		return v.checkOperandSink(&term.AsyncReturnCancelled.State, at, OwnershipSinkAsyncState, "state")
	}
	return nil
}

// checkReleasedPlace handles the two rows that release a PLACE outright.
//
// Neither carries an operand to classify, so the "resolve the use, then recurse
// only on TRANSFERS" split does not apply: this goes straight to the
// definition-resolving procedure for the released local. Modelling the release
// as an implicit copy-read instead would read ALIASES unconditionally and fail
// every unguarded drop in the corpus before a single definition was queried.
func (v *ownershipFuncVerifier) checkReleasedPlace(
	p Place,
	shallow bool,
	at ownershipPoint,
	kind OwnershipSinkKind,
	position string,
) []OwnershipFinding {
	if shallow || p.Kind != PlaceLocal || len(p.Proj) != 0 || p.Local == NoLocalID {
		return nil
	}
	if !v.localOwnsHeap(p.Local) {
		return nil
	}
	if kind == OwnershipSinkDrop && v.guardedDropAccepted(at) {
		return nil
	}
	st := newOwnershipResolveState()
	if v.resolvePlaceDefs(p.Local, at, st) {
		return nil
	}
	return v.findings(p.Local, st, at, kind, position)
}

func (v *ownershipFuncVerifier) checkCallArgs(call *CallInstr, at ownershipPoint) []OwnershipFinding {
	var out []OwnershipFinding
	for i, contract := range call.ArgContracts {
		if i >= len(call.Args) {
			break
		}
		arg := &call.Args[i]
		position := indexedOwnershipPosition("arg", i)
		switch contract {
		case ArgContractUnresolved:
			// A callee shape the lowering could not classify is reported
			// directly, with no reaching-def query: the gap itself is the
			// finding, and passing it through silently is the failure mode the
			// marker exists to prevent.
			out = append(out, v.findings(operandLocal(arg), nil, at, OwnershipSinkUnresolvedContract, position)...)
		case ArgContractTransferOwned:
			out = append(out, v.checkOperandSink(arg, at, OwnershipSinkCallArg, position)...)
		case ArgContractStore:
			out = append(out, v.checkOperandSink(arg, at, OwnershipSinkCallArg, position)...)
		}
	}
	return out
}

func (v *ownershipFuncVerifier) checkAssign(assign *AssignInstr, at ownershipPoint) []OwnershipFinding {
	out := v.checkAggregateRValue(&assign.Src, at)
	sinkKind := OwnershipSinkProjectedAssign
	if len(assign.Dst.Proj) == 0 {
		if assign.Dst.Kind != PlaceGlobal {
			return out
		}
		sinkKind = OwnershipSinkGlobalAssign
	}
	// A write through a projection or into a bare global is a STORE: the
	// destination keeps the value beyond this assignment.
	ty, ok := placeTypeWithMapElems(v.typesIn, v.f, v.globals, assign.Dst)
	if !ok || !ownsHeapFor(v.typesIn, v.semaRes, ty) {
		return out
	}
	st := newOwnershipResolveState()
	if v.resolveRValueUse(&assign.Src, ty, at, st) {
		return out
	}
	return append(out, v.findings(rvalueLocal(&assign.Src), st, at, sinkKind, "rhs")...)
}

func (v *ownershipFuncVerifier) checkAggregateRValue(rv *RValue, at ownershipPoint) []OwnershipFinding {
	switch rv.Kind {
	case RValueStructLit:
		return v.checkAggregateFields(rv.StructLit.Fields, nil, "", at)
	case RValueArrayLit:
		return v.checkAggregateFields(nil, rv.ArrayLit.Elems, "", at)
	case RValueTupleLit:
		return v.checkAggregateFields(nil, rv.TupleLit.Elems, "", at)
	}
	return nil
}

func (v *ownershipFuncVerifier) checkAggregateFields(
	fields []StructLitField,
	elems []Operand,
	prefix string,
	at ownershipPoint,
) []OwnershipFinding {
	out := make([]OwnershipFinding, 0, len(fields)+len(elems))
	for i := range fields {
		out = append(out, v.checkOperandSink(&fields[i].Value, at, OwnershipSinkAggregateField,
			ownershipFieldPosition(prefix, i, fields[i].Name))...)
	}
	for i := range elems {
		out = append(out, v.checkOperandSink(&elems[i], at, OwnershipSinkAggregateField,
			prefixedOwnershipPosition(prefix, indexedOwnershipPosition("elem", i)))...)
	}
	return out
}

func (v *ownershipFuncVerifier) checkCrossing(cr *CrossingInstr, at ownershipPoint) []OwnershipFinding {
	var out []OwnershipFinding
	for i := range cr.Captures {
		out = append(out, v.checkOperandSink(&cr.Captures[i].Value, at, OwnershipSinkCrossingCapture,
			indexedOwnershipPosition("capture", i))...)
	}
	out = append(out, v.checkAggregateFields(cr.State.Fields, nil, "state", at)...)
	if cr.ConsumesHandle {
		out = append(out, v.checkOperandSink(&cr.Receiver, at, OwnershipSinkCrossingReceiver, "receiver")...)
	}
	for i := range cr.RemoteOps {
		out = append(out, v.checkOperandSink(&cr.RemoteOps[i].Value, at, OwnershipSinkCrossingRemoteValue,
			indexedOwnershipPosition("remote_op", i)+".value")...)
	}
	return out
}

// checkOperandSink is the ordinary row: gate on the position's own effective
// type owning heap, then run the use-then-definition resolution on it.
func (v *ownershipFuncVerifier) checkOperandSink(
	op *Operand,
	at ownershipPoint,
	kind OwnershipSinkKind,
	position string,
) []OwnershipFinding {
	if !v.operandOwnsHeap(op) {
		return nil
	}
	st := newOwnershipResolveState()
	if v.resolveOperandUse(op, at, st) {
		return nil
	}
	return v.findings(operandLocal(op), st, at, kind, position)
}

// checkTaskConsume verifies the handle an Await consumes unconditionally, or
// a Poll/Timeout consumes on its ReadyBB edge. The check is made at the
// instruction point, where the same reaching definitions feed both outgoing
// edges; it does not kill the handle on PendBB, which must retain it for the
// next poll and may store it in an async-state envelope.
//
// Task instructions spell their retryable handle as OperandCopy even though
// successful completion transfers that handle to the runtime. That COPY is an
// ABI/load spelling, not an aliasing ownership use, so the canonical bare-local
// shape resolves the local's definitions directly. Other operand kinds keep
// their ordinary use semantics. A projected, global, or absent COPY is not a
// valid retryable task slot and fails closed instead of escaping the check.
func (v *ownershipFuncVerifier) checkTaskConsume(op *Operand, at ownershipPoint) []OwnershipFinding {
	eff := v.effectiveOperand(op)
	ty := operandType(&eff)
	if eff.Kind == OperandCopy && ty == types.NoTypeID {
		return v.findings(operandLocal(&eff), newOwnershipResolveState(), at, OwnershipSinkTaskConsume, "task")
	}
	if !ownsHeapFor(v.typesIn, v.semaRes, ty) {
		return nil
	}
	st := newOwnershipResolveState()
	if eff.Kind == OperandCopy {
		if eff.Place.Kind != PlaceLocal || len(eff.Place.Proj) != 0 || eff.Place.Local == NoLocalID {
			return v.findings(operandLocal(&eff), st, at, OwnershipSinkTaskConsume, "task")
		}
		if v.resolvePlaceDefs(eff.Place.Local, at, st) {
			return nil
		}
		return v.findings(eff.Place.Local, st, at, OwnershipSinkTaskConsume, "task")
	}
	return v.checkOperandSink(&eff, at, OwnershipSinkTaskConsume, "task")
}

func operandLocal(op *Operand) LocalID {
	if op == nil || op.Place.Kind != PlaceLocal {
		return NoLocalID
	}
	return op.Place.Local
}

func rvalueLocal(rv *RValue) LocalID {
	src, ok := transferSourceOperand(rv)
	if !ok {
		return NoLocalID
	}
	return operandLocal(src)
}
