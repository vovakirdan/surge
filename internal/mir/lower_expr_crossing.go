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
	if data.Kind == sema.CrossingLoweringSpawnOn {
		if err := l.prepareSpawnOnCrossing(&ins, data.Body, e.Span); err != nil {
			return Operand{}, err
		}
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

func (l *funcLowerer) spawnOnCaptureInfo(captures []CrossingCapture) []spawnOnCaptureInfo {
	if len(captures) == 0 {
		return nil
	}
	out := make([]spawnOnCaptureInfo, 0, len(captures))
	for i := range captures {
		field := fmt.Sprintf("__cap%d", i)
		out = append(out, spawnOnCaptureInfo{
			SymbolID:  captures[i].Symbol,
			Name:      l.symbolName(captures[i].Symbol, field),
			Type:      captures[i].Type,
			FieldName: field,
		})
	}
	return out
}

func (l *funcLowerer) lowerSpawnOnPollFunc(id FuncID, name string, body *hir.Block, result, stateType types.TypeID, captures []spawnOnCaptureInfo, span source.Span) (*Func, error) {
	if l == nil {
		return nil, nil
	}
	l.f = &Func{
		ID:       id,
		Sym:      symbols.NoSymbolID,
		Name:     name,
		Span:     span,
		Result:   result,
		IsAsync:  false,
		Failfast: false,
	}

	stateLocal := addLocal(l.f, "__state", stateType, localFlagsFor(l.types, l.sema, stateType))
	entry := l.newBlock()
	l.f.Entry = entry
	l.cur = entry
	l.emit(&Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: stateLocal},
		Callee: Callee{Kind: CalleeValue, Name: "__task_state"},
	}})
	for _, cap := range captures {
		localID := l.ensureLocal(cap.SymbolID, cap.Name, cap.Type, span)
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: localID},
			Src: RValue{Kind: RValueField, Field: FieldAccess{
				Object:    Operand{Kind: OperandCopy, Type: stateType, Place: Place{Local: stateLocal}},
				FieldName: cap.FieldName,
			}},
		}})
	}

	exitBB := l.newBlock()
	resultLocal := NoLocalID
	hasResult := result != types.NoTypeID && !l.isNothingType(result)
	if hasResult {
		resultLocal = addLocal(l.f, "__result", result, localFlagsFor(l.types, l.sema, result))
	}
	l.returnStack = append(l.returnStack, returnCtx{
		exit:      exitBB,
		hasResult: hasResult,
		result:    Place{Local: resultLocal},
	})
	if body != nil {
		if err := l.lowerBlock(body); err != nil {
			return nil, err
		}
	}
	l.returnStack = l.returnStack[:len(l.returnStack)-1]
	if !l.curBlock().Terminated() {
		l.setTerm(&Terminator{Kind: TermUnreachable})
	}
	l.startBlock(exitBB)
	if hasResult {
		l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{
			HasValue: true,
			Value:    Operand{Kind: OperandMove, Type: result, Place: Place{Local: resultLocal}},
		}})
	} else {
		l.setTerm(&Terminator{Kind: TermReturn})
	}
	rewriteSpawnOnPollReturns(l.f, stateLocal)
	for i := range l.f.Blocks {
		if l.f.Blocks[i].Term.Kind == TermNone {
			l.f.Blocks[i].Term.Kind = TermUnreachable
		}
	}
	return l.f, nil
}

func rewriteSpawnOnPollReturns(f *Func, stateLocal LocalID) {
	if f == nil {
		return
	}
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		if bb.Term.Kind != TermReturn {
			continue
		}
		term := bb.Term.Return
		bb.Term = Terminator{Kind: TermAsyncReturn, AsyncReturn: AsyncReturnTerm{
			State: Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
		}}
		if term.HasValue {
			bb.Term.AsyncReturn.HasValue = true
			bb.Term.AsyncReturn.Value = term.Value
		}
	}
}

func (l *funcLowerer) opaquePointerType() types.TypeID {
	if l == nil || l.types == nil {
		return types.NoTypeID
	}
	return l.types.Intern(types.MakePointer(l.types.Builtins().Uint8))
}

func (l *funcLowerer) symbolName(symID symbols.SymbolID, fallback string) string {
	if l == nil || l.symbols == nil || l.symbols.Table == nil ||
		l.symbols.Table.Symbols == nil || l.symbols.Table.Strings == nil || !symID.IsValid() {
		return fallback
	}
	sym := l.symbols.Table.Symbols.Get(symID)
	if sym == nil || sym.Name == source.NoStringID {
		return fallback
	}
	name := l.symbols.Table.Strings.MustLookup(sym.Name)
	if name == "" {
		return fallback
	}
	return name
}
