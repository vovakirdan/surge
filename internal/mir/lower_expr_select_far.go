package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// The canonical remote-select body: every remote select in a module ships
// the same synthetic poll function, whose whole body is the one anchored
// select operation (winner = rt_anchored_channel_select(); async-return
// winner). The runtime helper resolves the arm table through the body's
// pending binding and yields when parked, so the function needs no state
// fields of its own.
const selectBodyPollName = "__select_anchored_block$poll"

// lowerRemoteSelect emits the ChannelSelect crossing that feeds the select's
// winner-index local; the caller's arm dispatch consumes that local exactly
// as it consumes a local rt_select_poll result.
func (l *funcLowerer) lowerRemoteSelect(
	crossing *hir.CrossingData,
	dst Place,
	resultType types.TypeID,
	span source.Span,
) error {
	if l == nil || crossing == nil {
		return fmt.Errorf("mir: remote select: missing crossing payload")
	}
	if !l.opts.crossingEnabled(crossing.Kind) {
		return fmt.Errorf("mir: crossing %s lowering is not enabled", mirCrossingKindName(crossing.Kind))
	}
	ins := CrossingInstr{
		Kind:        crossing.Kind,
		Dst:         dst,
		PayloadType: crossing.PayloadType,
		ResultType:  resultType,
		ReadyBB:     NoBlockID,
		PendBB:      NoBlockID,
	}
	// A far select cannot know which `own binding` arm wins until the owner
	// shard commits it. Identify the exact bare-local shapes up front so only
	// UNIQUE roots enter the conditional-transfer protocol below. Reusing one
	// root in multiple SEND arms needs ownership-group semantics in the runtime;
	// leave that unsupported shape on the ordinary aliasing path so the
	// ownership verifier keeps reporting it instead of silently blessing two
	// pending owners of one value.
	returnCandidates := make([]farSelectReturnCandidate, len(crossing.RemoteOps))
	returnCounts := make(map[LocalID]int)
	for i := range crossing.RemoteOps {
		candidate, ok := l.farSelectReturnCandidate(crossing.RemoteOps[i].Value)
		if !ok {
			continue
		}
		returnCandidates[i] = candidate
		returnCounts[candidate.place.Local]++
	}
	for i := range crossing.RemoteOps {
		recv, err := l.lowerExpr(crossing.RemoteOps[i].Receiver, false)
		if err != nil {
			return err
		}
		op := CrossingRemoteOp{
			Method:         crossing.RemoteOps[i].Method,
			Receiver:       recv,
			ReceiverSymbol: crossing.RemoteOps[i].ReceiverSymbol,
			ReceiverType:   crossing.RemoteOps[i].ReceiverType,
		}
		if crossing.RemoteOps[i].Value != nil {
			candidate := returnCandidates[i]
			if candidate.ok && returnCounts[candidate.place.Local] == 1 {
				op.Value = candidate.value
				returnPlace := candidate.place
				op.ReturnPlace = &returnPlace
			} else {
				val, err := l.lowerExpr(crossing.RemoteOps[i].Value, false)
				if err != nil {
					return err
				}
				op.Value = val
			}
		}
		ins.RemoteOps = append(ins.RemoteOps, op)
	}
	bodyID, err := l.ensureSelectBodyPollFunc(span)
	if err != nil {
		return err
	}
	ins.BodyFuncID = bodyID

	pendingType := l.opaquePointerType()
	pendingLocal := l.newTemp(pendingType, "channel_select_pending", span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: pendingLocal},
			Src: RValue{Kind: RValueUse, Use: Operand{
				Kind:  OperandConst,
				Type:  pendingType,
				Const: Const{Kind: ConstInt, Type: pendingType, IntValue: 0},
			}},
		},
	})
	ins.Pending = Place{Local: pendingLocal}
	l.emit(&Instr{Kind: InstrCrossing, Crossing: ins})
	return nil
}

type farSelectReturnCandidate struct {
	value Operand
	place Place
	ok    bool
}

// farSelectReturnCandidate recognizes only `own <bare local>` whose value has
// a real heap ownership obligation. The special form is intentionally found
// before generic unary lowering: generic `own` is representation-identity and
// materializes an alias temp, while this crossing needs an explicit MOVE plus
// success handback to model its conditional ownership truthfully.
func (l *funcLowerer) farSelectReturnCandidate(value *hir.Expr) (farSelectReturnCandidate, bool) {
	if l == nil || l.f == nil || value == nil || value.Kind != hir.ExprUnaryOp {
		return farSelectReturnCandidate{}, false
	}
	unary, ok := value.Data.(hir.UnaryOpData)
	if !ok || unary.Op != ast.ExprUnaryOwn || unary.Operand == nil || unary.Operand.Kind != hir.ExprVarRef {
		return farSelectReturnCandidate{}, false
	}
	ref, ok := unary.Operand.Data.(hir.VarRefData)
	if !ok || !ref.SymbolID.IsValid() {
		return farSelectReturnCandidate{}, false
	}
	local, ok := l.symToLocal[ref.SymbolID]
	if !ok || local == NoLocalID || int(local) < 0 || int(local) >= len(l.f.Locals) {
		return farSelectReturnCandidate{}, false
	}
	localInfo := l.f.Locals[local]
	localType := localInfo.Type
	if localType == types.NoTypeID || localInfo.Flags&LocalFlagOwnsHeap == 0 ||
		localInfo.Flags&LocalFlagCopy != 0 {
		return farSelectReturnCandidate{}, false
	}
	place := Place{Local: local}
	return farSelectReturnCandidate{
		value: Operand{Kind: OperandMove, Type: localType, Place: place},
		place: place,
		ok:    true,
	}, true
}

// ensureSelectBodyPollFunc creates (once per module) the canonical select
// body poll function and returns its id.
func (l *funcLowerer) ensureSelectBodyPollFunc(span source.Span) (FuncID, error) {
	if l == nil || l.out == nil {
		return NoFuncID, fmt.Errorf("mir: remote select: missing module output")
	}
	for _, __id := range l.out.SortedFuncIDs() {
		id := __id
		fn := l.out.Funcs[__id]
		if fn != nil && fn.Name == selectBodyPollName {
			return id, nil
		}
	}
	id := l.allocFuncID()
	if id == NoFuncID {
		return NoFuncID, fmt.Errorf("mir: remote select: failed to allocate poll function id")
	}
	fl := l.forkLowerer()
	if fl == nil {
		return NoFuncID, fmt.Errorf("mir: remote select: failed to fork lowerer")
	}
	winnerType := fl.types.Builtins().Uint64
	stateType := fl.opaquePointerType()
	fl.f = &Func{
		ID:     id,
		Sym:    symbols.NoSymbolID,
		Name:   selectBodyPollName,
		Span:   span,
		Result: winnerType,
	}
	stateLocal := addLocal(fl.f, "__state", stateType, localFlagsFor(fl.types, fl.sema, stateType))
	winnerLocal := addLocal(fl.f, "__winner", winnerType, localFlagsFor(fl.types, fl.sema, winnerType))
	entry := fl.newBlock()
	fl.f.Entry = entry
	fl.cur = entry
	fl.emit(&Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: stateLocal},
		Callee: Callee{Kind: CalleeValue, Name: "__task_state"},
	}})
	fl.emit(&Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: winnerLocal},
		Callee: Callee{Kind: CalleeValue, Name: "rt_anchored_channel_select"},
	}})
	fl.setTerm(&Terminator{Kind: TermAsyncReturn, AsyncReturn: AsyncReturnTerm{
		State:    Operand{Kind: OperandCopy, Type: stateType, Place: Place{Local: stateLocal}},
		HasValue: true,
		Value:    Operand{Kind: OperandMove, Type: winnerType, Place: Place{Local: winnerLocal}},
	}})
	l.out.Funcs[id] = fl.f
	return id, nil
}
