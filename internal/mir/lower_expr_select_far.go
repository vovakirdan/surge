package mir

import (
	"fmt"

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
			val, err := l.lowerExpr(crossing.RemoteOps[i].Value, false)
			if err != nil {
				return err
			}
			op.Value = val
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

// ensureSelectBodyPollFunc creates (once per module) the canonical select
// body poll function and returns its id.
func (l *funcLowerer) ensureSelectBodyPollFunc(span source.Span) (FuncID, error) {
	if l == nil || l.out == nil {
		return NoFuncID, fmt.Errorf("mir: remote select: missing module output")
	}
	for id, fn := range l.out.Funcs {
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
