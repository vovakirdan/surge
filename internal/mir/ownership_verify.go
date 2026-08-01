package mir

import (
	"fmt"

	"surge/internal/sema"
	"surge/internal/source"
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
	default:
		return "unknown"
	}
}

// OwnershipFinding reports one consuming sink whose value this pass could not
// establish as owned. It is DATA: the pass returns these and changes nothing.
type OwnershipFinding struct {
	Function string
	FuncID   FuncID
	// Local is the local whose value the sink consumes, or NoLocalID where the
	// sink's operand names no place at all.
	Local LocalID
	// LocalName is the local's MIR name, for a message that reads without a
	// dump next to it.
	LocalName string
	// DefSite names the definition that failed to resolve — "parameter" for an
	// entry root, "bb2#1" for an instruction — or "use" when the operand
	// occupying the sink was itself aliasing and no definition was ever
	// queried.
	DefSite string
	// ConsumingSite is where the release or consumption happens.
	ConsumingSite string
	ConsumingKind OwnershipSinkKind
	Span          source.Span
}

func (f OwnershipFinding) String() string {
	return fmt.Sprintf("%s: %s of %s (def %s) at %s",
		f.Function, f.ConsumingKind, f.localLabel(), f.DefSite, f.ConsumingSite)
}

func (f *OwnershipFinding) localLabel() string {
	if f.Local == NoLocalID {
		return "<no place>"
	}
	if f.LocalName == "" {
		return fmt.Sprintf("L%d", f.Local)
	}
	return fmt.Sprintf("L%d(%s)", f.Local, f.LocalName)
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
		return v.checkReleasedPlace(ins.Drop.Place, ins.Drop.Shallow, at, OwnershipSinkDrop)
	case InstrEnvelopeRelease:
		return v.checkReleasedPlace(ins.EnvelopeRelease.Place, false, at, OwnershipSinkEnvelopeRelease)
	case InstrCall:
		return v.checkCallArgs(&ins.Call, at)
	case InstrAssign:
		return v.checkAssign(&ins.Assign, at)
	case InstrCrossing:
		return v.checkCrossing(&ins.Crossing, at)
	case InstrBlocking:
		return v.checkAggregateFields(ins.Blocking.State.Fields, nil, at)
	case InstrChanSend:
		return v.checkOperandSink(&ins.ChanSend.Value, at, OwnershipSinkChanSend)
	case InstrSelect:
		var out []OwnershipFinding
		for i := range ins.Select.Arms {
			arm := &ins.Select.Arms[i]
			if arm.Kind != SelectArmChanSend {
				continue
			}
			out = append(out, v.checkOperandSink(&arm.Value, at, OwnershipSinkSelectSend)...)
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
		return v.checkOperandSink(&term.Return.Value, at, OwnershipSinkReturn)
	case TermAsyncReturn:
		if !term.AsyncReturn.HasValue || !ownsHeapFor(v.typesIn, v.semaRes, v.f.Result) {
			return nil
		}
		return v.checkOperandSink(&term.AsyncReturn.Value, at, OwnershipSinkReturn)
	case TermAsyncYield:
		return v.checkOperandSink(&term.AsyncYield.State, at, OwnershipSinkAsyncState)
	case TermAsyncReturnCancelled:
		return v.checkOperandSink(&term.AsyncReturnCancelled.State, at, OwnershipSinkAsyncState)
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
func (v *ownershipFuncVerifier) checkReleasedPlace(p Place, shallow bool, at ownershipPoint, kind OwnershipSinkKind) []OwnershipFinding {
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
	return []OwnershipFinding{v.finding(p.Local, st, at, kind)}
}

func (v *ownershipFuncVerifier) checkCallArgs(call *CallInstr, at ownershipPoint) []OwnershipFinding {
	var out []OwnershipFinding
	for i, contract := range call.ArgContracts {
		if i >= len(call.Args) {
			break
		}
		arg := &call.Args[i]
		switch contract {
		case ArgContractUnresolved:
			// A callee shape the lowering could not classify is reported
			// directly, with no reaching-def query: the gap itself is the
			// finding, and passing it through silently is the failure mode the
			// marker exists to prevent.
			out = append(out, v.finding(operandLocal(arg), nil, at, OwnershipSinkUnresolvedContract))
		case ArgContractTransferOwned, ArgContractStore:
			out = append(out, v.checkOperandSink(arg, at, OwnershipSinkCallArg)...)
		}
	}
	return out
}

func (v *ownershipFuncVerifier) checkAssign(assign *AssignInstr, at ownershipPoint) []OwnershipFinding {
	out := v.checkAggregateRValue(&assign.Src, at)
	if len(assign.Dst.Proj) == 0 {
		return out
	}
	// A write into a place that is already inside a live container: the value
	// written in is kept there and released by a later drop of the container.
	ty, ok := placeTypeIn(v.typesIn, v.f, v.globals, assign.Dst)
	if !ok || !ownsHeapFor(v.typesIn, v.semaRes, ty) {
		return out
	}
	st := newOwnershipResolveState()
	if v.resolveRValueUse(&assign.Src, ty, at, st) {
		return out
	}
	return append(out, v.finding(rvalueLocal(&assign.Src), st, at, OwnershipSinkProjectedAssign))
}

func (v *ownershipFuncVerifier) checkAggregateRValue(rv *RValue, at ownershipPoint) []OwnershipFinding {
	switch rv.Kind {
	case RValueStructLit:
		return v.checkAggregateFields(rv.StructLit.Fields, nil, at)
	case RValueArrayLit:
		return v.checkAggregateFields(nil, rv.ArrayLit.Elems, at)
	case RValueTupleLit:
		return v.checkAggregateFields(nil, rv.TupleLit.Elems, at)
	}
	return nil
}

func (v *ownershipFuncVerifier) checkAggregateFields(fields []StructLitField, elems []Operand, at ownershipPoint) []OwnershipFinding {
	out := make([]OwnershipFinding, 0, len(fields)+len(elems))
	for i := range fields {
		out = append(out, v.checkOperandSink(&fields[i].Value, at, OwnershipSinkAggregateField)...)
	}
	for i := range elems {
		out = append(out, v.checkOperandSink(&elems[i], at, OwnershipSinkAggregateField)...)
	}
	return out
}

func (v *ownershipFuncVerifier) checkCrossing(cr *CrossingInstr, at ownershipPoint) []OwnershipFinding {
	var out []OwnershipFinding
	for i := range cr.Captures {
		out = append(out, v.checkOperandSink(&cr.Captures[i].Value, at, OwnershipSinkCrossingCapture)...)
	}
	out = append(out, v.checkAggregateFields(cr.State.Fields, nil, at)...)
	if cr.ConsumesHandle {
		out = append(out, v.checkOperandSink(&cr.Receiver, at, OwnershipSinkCrossingReceiver)...)
	}
	for i := range cr.RemoteOps {
		out = append(out, v.checkOperandSink(&cr.RemoteOps[i].Value, at, OwnershipSinkCrossingRemoteValue)...)
	}
	return out
}

// checkOperandSink is the ordinary row: gate on the position's own effective
// type owning heap, then run the use-then-definition resolution on it.
func (v *ownershipFuncVerifier) checkOperandSink(op *Operand, at ownershipPoint, kind OwnershipSinkKind) []OwnershipFinding {
	if !v.operandOwnsHeap(op) {
		return nil
	}
	st := newOwnershipResolveState()
	if v.resolveOperandUse(op, at, st) {
		return nil
	}
	return []OwnershipFinding{v.finding(operandLocal(op), st, at, kind)}
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

func (v *ownershipFuncVerifier) finding(local LocalID, st *ownershipResolveState, at ownershipPoint, kind OwnershipSinkKind) OwnershipFinding {
	def := "use"
	if st != nil && st.hasBlame {
		def = describeDefSite(st.blame)
	}
	if kind == OwnershipSinkUnresolvedContract {
		def = "unresolved contract"
	}
	name := ""
	span := v.f.Span
	if local != NoLocalID && int(local) < len(v.f.Locals) {
		name = v.f.Locals[local].Name
		span = v.f.Locals[local].Span
	}
	return OwnershipFinding{
		Function:      v.f.Name,
		FuncID:        v.f.ID,
		Local:         local,
		LocalName:     name,
		DefSite:       def,
		ConsumingSite: describePoint(v.f, at),
		ConsumingKind: kind,
		Span:          span,
	}
}

func describeDefSite(d ownershipDefSite) string {
	if d.IsParamRoot() {
		return "parameter"
	}
	return fmt.Sprintf("bb%d#%d", d.Block, d.Instr)
}

func describePoint(f *Func, at ownershipPoint) string {
	if f != nil && int(at.Block) < len(f.Blocks) && at.Instr >= len(f.Blocks[at.Block].Instrs) {
		return fmt.Sprintf("bb%d#term", at.Block)
	}
	return fmt.Sprintf("bb%d#%d", at.Block, at.Instr)
}
