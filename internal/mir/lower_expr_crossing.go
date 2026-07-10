package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func mirCrossingKindName(kind sema.CrossingLoweringKind) string {
	switch kind {
	case sema.CrossingLoweringOnPlacement:
		return "on"
	case sema.CrossingLoweringOnFarHandle:
		return "on_far_handle"
	case sema.CrossingLoweringSpawnOn:
		return "spawn_on"
	case sema.CrossingLoweringFarTaskAwait:
		return "far_task_await"
	case sema.CrossingLoweringFarTaskCancel:
		return "far_task_cancel"
	case sema.CrossingLoweringChannelCreate:
		return "channel_create"
	default:
		return fmt.Sprintf("kind_%d", kind)
	}
}

func (l *funcLowerer) lowerCrossingExpr(e *hir.Expr, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}
	data, ok := e.Data.(hir.CrossingData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: crossing: unexpected payload %T", e.Data)
	}
	if !l.opts.crossingEnabled(data.Kind) {
		return Operand{}, fmt.Errorf("mir: crossing %s lowering is not enabled", mirCrossingKindName(data.Kind))
	}

	resultType := data.ResultType
	if resultType == types.NoTypeID {
		resultType = e.Type
	}
	tmp := l.newTemp(resultType, "crossing", e.Span)
	ins := CrossingInstr{
		Kind: data.Kind,
		Dst:  Place{Local: tmp},
		Destination: CrossingDestination{
			Kind:          data.Destination.Kind,
			Type:          data.Destination.Type,
			AnchorSymbol:  data.Destination.AnchorSymbol,
			OwnerAnchored: data.Destination.OwnerAnchored,
		},
		ReceiverSymbol: data.ReceiverSymbol,
		ReceiverType:   data.ReceiverType,
		ConsumesHandle: data.ConsumesHandle,
		PayloadType:    data.PayloadType,
		ResultType:     resultType,
		HandleType:     data.HandleType,
		ReadyBB:        NoBlockID,
		PendBB:         NoBlockID,
	}
	if data.Destination.Value != nil {
		op, err := l.lowerExpr(data.Destination.Value, false)
		if err != nil {
			return Operand{}, err
		}
		ins.Destination.Value = op
	}
	for i := range data.Captures {
		capOp, err := l.lowerExpr(data.Captures[i].Value, data.Captures[i].Mode != sema.CrossingCaptureCopy)
		if err != nil {
			return Operand{}, err
		}
		ins.Captures = append(ins.Captures, CrossingCapture{
			Symbol:  data.Captures[i].Symbol,
			Value:   capOp,
			Type:    data.Captures[i].Type,
			Mode:    data.Captures[i].Mode,
			Verdict: data.Captures[i].Verdict,
		})
	}
	for i := range data.RemoteOps {
		recv, err := l.lowerExpr(data.RemoteOps[i].Receiver, false)
		if err != nil {
			return Operand{}, err
		}
		ins.RemoteOps = append(ins.RemoteOps, CrossingRemoteOp{
			Method:         data.RemoteOps[i].Method,
			Receiver:       recv,
			ReceiverSymbol: data.RemoteOps[i].ReceiverSymbol,
			ReceiverType:   data.RemoteOps[i].ReceiverType,
		})
	}
	if data.Receiver != nil {
		recv, err := l.lowerExpr(data.Receiver, data.ConsumesHandle)
		if err != nil {
			return Operand{}, err
		}
		ins.Receiver = recv
	}
	switch {
	case data.Kind == sema.CrossingLoweringSpawnOn:
		if err := l.prepareSpawnOnCrossing(&ins, data.Body, e.Span); err != nil {
			return Operand{}, err
		}
	case data.Kind == sema.CrossingLoweringOnPlacement:
		if err := l.prepareOnPlacementCrossing(&ins, data.Body, e.Span); err != nil {
			return Operand{}, err
		}
	case data.Kind == sema.CrossingLoweringChannelCreate:
		l.prepareChannelCreateCrossing(&ins, e.Span)
	case crossingUsesPendingRetryState(data.Kind):
		l.prepareFarTaskLifecycleCrossing(&ins, e.Span)
	}

	l.emit(&Instr{Kind: InstrCrossing, Crossing: ins})
	return l.placeOperand(Place{Local: tmp}, resultType, consume), nil
}

type spawnOnCaptureInfo struct {
	SymbolID  symbols.SymbolID
	Name      string
	Type      types.TypeID
	FieldName string
}

func (l *funcLowerer) prepareSpawnOnCrossing(ins *CrossingInstr, body *hir.Block, span source.Span) error {
	if l == nil || ins == nil {
		return nil
	}
	if body == nil {
		return fmt.Errorf("mir: spawn_on: missing body")
	}
	pollID := l.allocFuncID()
	if pollID == NoFuncID {
		return fmt.Errorf("mir: spawn_on: failed to allocate poll function id")
	}
	name := fmt.Sprintf("__spawn_on_block$%d$poll", pollID)
	captures := l.spawnOnCaptureInfo(ins.Captures)
	stateType, err := buildSpawnOnStateStruct(l.types, name, captures)
	if err != nil {
		return err
	}
	fl := l.forkLowerer()
	if fl == nil {
		return fmt.Errorf("mir: spawn_on: failed to fork lowerer")
	}
	fn, err := fl.lowerSpawnOnPollFunc(pollID, name, body, ins.PayloadType, stateType, captures, span)
	if err != nil {
		return err
	}
	if l.out != nil {
		l.out.Funcs[pollID] = fn
	}

	stateLit := StructLit{TypeID: stateType}
	for i := range captures {
		if i >= len(ins.Captures) {
			return fmt.Errorf("mir: spawn_on: capture state mismatch")
		}
		stateLit.Fields = append(stateLit.Fields, StructLitField{
			Name:  captures[i].FieldName,
			Value: ins.Captures[i].Value,
		})
	}

	pendingType := l.opaquePointerType()
	pendingLocal := l.newTemp(pendingType, "spawn_on_pending", span)
	handleLocal := l.newTemp(pendingType, "spawn_on_handle", span)
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
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: handleLocal},
			Src: RValue{Kind: RValueUse, Use: Operand{
				Kind:  OperandConst,
				Type:  pendingType,
				Const: Const{Kind: ConstInt, Type: pendingType, IntValue: 0},
			}},
		},
	})

	ins.BodyFuncID = pollID
	ins.State = stateLit
	ins.Pending = Place{Local: pendingLocal}
	ins.Handle = Place{Local: handleLocal}
	return nil
}

// prepareOnPlacementCrossing lowers the immediate `on placement` body into a
// destination poll function exactly like `spawn on`, but the crossing keeps
// only the pending-retry slot: there is no publicly observable far Task
// handle for the dedicated execute/reply category.
func (l *funcLowerer) prepareOnPlacementCrossing(ins *CrossingInstr, body *hir.Block, span source.Span) error {
	if l == nil || ins == nil {
		return nil
	}
	if body == nil {
		return fmt.Errorf("mir: on: missing body")
	}
	pollID := l.allocFuncID()
	if pollID == NoFuncID {
		return fmt.Errorf("mir: on: failed to allocate poll function id")
	}
	name := fmt.Sprintf("__on_block$%d$poll", pollID)
	captures := l.spawnOnCaptureInfo(ins.Captures)
	stateType, err := buildSpawnOnStateStruct(l.types, name, captures)
	if err != nil {
		return err
	}
	fl := l.forkLowerer()
	if fl == nil {
		return fmt.Errorf("mir: on: failed to fork lowerer")
	}
	fn, err := fl.lowerSpawnOnPollFunc(pollID, name, body, ins.PayloadType, stateType, captures, span)
	if err != nil {
		return err
	}
	if l.out != nil {
		l.out.Funcs[pollID] = fn
	}

	stateLit := StructLit{TypeID: stateType}
	for i := range captures {
		if i >= len(ins.Captures) {
			return fmt.Errorf("mir: on: capture state mismatch")
		}
		stateLit.Fields = append(stateLit.Fields, StructLitField{
			Name:  captures[i].FieldName,
			Value: ins.Captures[i].Value,
		})
	}

	pendingType := l.opaquePointerType()
	pendingLocal := l.newTemp(pendingType, "on_pending", span)
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

	ins.BodyFuncID = pollID
	ins.State = stateLit
	ins.Pending = Place{Local: pendingLocal}
	return nil
}

func crossingUsesPendingRetryState(kind sema.CrossingLoweringKind) bool {
	return kind == sema.CrossingLoweringSpawnOn ||
		kind == sema.CrossingLoweringOnPlacement ||
		kind == sema.CrossingLoweringFarTaskAwait ||
		kind == sema.CrossingLoweringFarTaskCancel ||
		kind == sema.CrossingLoweringChannelCreate
}

// prepareChannelCreateCrossing persists the pending-retry slot and the
// caller-side handle slot for the channel producer; there is no body
// function — the destination creates the channel itself.
func (l *funcLowerer) prepareChannelCreateCrossing(ins *CrossingInstr, span source.Span) {
	if l == nil || ins == nil {
		return
	}
	pendingType := l.opaquePointerType()
	pendingLocal := l.newTemp(pendingType, "channel_create_pending", span)
	handleLocal := l.newTemp(pendingType, "channel_create_handle", span)
	for _, local := range []LocalID{pendingLocal, handleLocal} {
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: Place{Local: local},
				Src: RValue{Kind: RValueUse, Use: Operand{
					Kind:  OperandConst,
					Type:  pendingType,
					Const: Const{Kind: ConstInt, Type: pendingType, IntValue: 0},
				}},
			},
		})
	}
	ins.Pending = Place{Local: pendingLocal}
	ins.Handle = Place{Local: handleLocal}
}

func (l *funcLowerer) prepareFarTaskLifecycleCrossing(ins *CrossingInstr, span source.Span) {
	if l == nil || ins == nil {
		return
	}
	pendingType := l.opaquePointerType()
	pendingLocal := l.newTemp(pendingType, "far_task_pending", span)
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
}
