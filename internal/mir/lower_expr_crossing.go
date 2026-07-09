package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/sema"
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

	l.emit(&Instr{Kind: InstrCrossing, Crossing: ins})
	return l.placeOperand(Place{Local: tmp}, resultType, consume), nil
}
