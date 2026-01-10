package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

type blockingCaptureInfo struct {
	SymbolID  symbols.SymbolID
	Name      string
	Type      types.TypeID
	FieldName string
}

func (l *funcLowerer) lowerBlockingExpr(e *hir.Expr, consume bool) (Operand, error) {
	data, ok := e.Data.(hir.BlockingData)
	if !ok {
		return Operand{}, fmt.Errorf("mir: blocking: unexpected payload %T", e.Data)
	}
	payload, ok := l.taskPayloadType(e.Type)
	if !ok {
		return Operand{}, fmt.Errorf("mir: blocking: expected Task<T> type, got %v", e.Type)
	}
	blockingID := l.allocFuncID()
	if blockingID == NoFuncID {
		return Operand{}, fmt.Errorf("mir: blocking: failed to allocate function id")
	}
	name := fmt.Sprintf("__blocking_block$%d", blockingID)
	captures, err := l.blockingCaptureInfo(data.Captures)
	if err != nil {
		return Operand{}, err
	}
	stateType, err := buildBlockingStateStruct(l.types, name, captures)
	if err != nil {
		return Operand{}, err
	}
	fl := l.forkLowerer()
	if fl == nil {
		return Operand{}, fmt.Errorf("mir: blocking: failed to fork lowerer")
	}
	fn, err := fl.lowerBlockingFunc(blockingID, name, data.Body, payload, stateType, captures, e.Span)
	if err != nil {
		return Operand{}, err
	}
	if l.out != nil {
		l.out.Funcs[blockingID] = fn
	}

	stateLit, err := l.blockingStateLiteral(stateType, captures)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "blocking", e.Span)
	l.emit(&Instr{
		Kind: InstrBlocking,
		Blocking: BlockingInstr{
			Dst:    Place{Local: tmp},
			FuncID: blockingID,
			State:  stateLit,
		},
	})
	return l.placeOperand(Place{Local: tmp}, e.Type, consume), nil
}

func (l *funcLowerer) blockingCaptureInfo(captures []hir.BlockingCapture) ([]blockingCaptureInfo, error) {
	if len(captures) == 0 {
		return nil, nil
	}
	out := make([]blockingCaptureInfo, 0, len(captures))
	for i, cap := range captures {
		field := fmt.Sprintf("__cap%d", i)
		ty := l.captureType(cap.SymbolID)
		if ty == types.NoTypeID {
			return nil, fmt.Errorf("mir: blocking: missing capture type for %s", cap.Name)
		}
		name := cap.Name
		if name == "" {
			name = field
		}
		out = append(out, blockingCaptureInfo{
			SymbolID:  cap.SymbolID,
			Name:      name,
			Type:      ty,
			FieldName: field,
		})
	}
	return out, nil
}

func (l *funcLowerer) blockingStateLiteral(stateType types.TypeID, captures []blockingCaptureInfo) (StructLit, error) {
	if stateType == types.NoTypeID {
		return StructLit{}, fmt.Errorf("mir: blocking: missing state type")
	}
	fields := make([]StructLitField, 0, len(captures))
	for _, cap := range captures {
		val, err := l.captureOperand(cap)
		if err != nil {
			return StructLit{}, err
		}
		fields = append(fields, StructLitField{
			Name:  cap.FieldName,
			Value: val,
		})
	}
	return StructLit{TypeID: stateType, Fields: fields}, nil
}

func (l *funcLowerer) captureOperand(capture blockingCaptureInfo) (Operand, error) {
	if capture.SymbolID.IsValid() {
		if local, ok := l.symToLocal[capture.SymbolID]; ok {
			ty := capture.Type
			if ty == types.NoTypeID && l.f != nil && int(local) >= 0 && int(local) < len(l.f.Locals) {
				if lty := l.f.Locals[local].Type; lty != types.NoTypeID {
					ty = lty
				}
			}
			return l.placeOperand(Place{Local: local}, ty, true), nil
		}
		if l.symToGlobal != nil {
			if global, ok := l.symToGlobal[capture.SymbolID]; ok {
				ty := capture.Type
				if ty == types.NoTypeID && l.out != nil && int(global) >= 0 && int(global) < len(l.out.Globals) {
					if gty := l.out.Globals[global].Type; gty != types.NoTypeID {
						ty = gty
					}
				}
				return l.placeOperand(Place{Kind: PlaceGlobal, Global: global}, ty, true), nil
			}
		}
		if op, handled, err := l.lowerConstValue(capture.SymbolID, true); handled {
			return op, err
		}
	}
	if capture.Name == "" {
		return Operand{}, fmt.Errorf("mir: blocking: unresolved capture")
	}
	return Operand{}, fmt.Errorf("mir: blocking: unresolved capture %s", capture.Name)
}

func (l *funcLowerer) captureType(symID symbols.SymbolID) types.TypeID {
	if l == nil {
		return types.NoTypeID
	}
	if l.sema != nil && l.sema.BindingTypes != nil {
		if ty := l.sema.BindingTypes[symID]; ty != types.NoTypeID {
			return ty
		}
	}
	if l.symbols != nil && l.symbols.Table != nil && l.symbols.Table.Symbols != nil {
		if sym := l.symbols.Table.Symbols.Get(symID); sym != nil && sym.Type != types.NoTypeID {
			return sym.Type
		}
	}
	if l.f != nil {
		if local, ok := l.symToLocal[symID]; ok {
			if int(local) >= 0 && int(local) < len(l.f.Locals) {
				return l.f.Locals[local].Type
			}
		}
	}
	return types.NoTypeID
}

func (l *funcLowerer) lowerBlockingFunc(id FuncID, name string, body *hir.Block, result, stateType types.TypeID, captures []blockingCaptureInfo, span source.Span) (*Func, error) {
	if l == nil {
		return nil, nil
	}
	l.f = &Func{
		ID:         id,
		Sym:        symbols.NoSymbolID,
		Name:       name,
		Span:       span,
		Result:     result,
		IsAsync:    false,
		Failfast:   false,
		ParamCount: 1,
	}

	stateLocal := addLocal(l.f, "__state", stateType, localFlagsFor(l.types, l.sema, stateType))
	entry := l.newBlock()
	l.f.Entry = entry
	l.cur = entry

	for _, cap := range captures {
		localID := l.ensureLocal(cap.SymbolID, cap.Name, cap.Type, span)
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: localID},
			Src: RValue{Kind: RValueField, Field: FieldAccess{
				Object:    Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
				FieldName: cap.FieldName,
			}},
		}})
	}

	if body != nil {
		if err := l.lowerBlock(body); err != nil {
			return nil, err
		}
	}

	if !l.curBlock().Terminated() {
		if result == types.NoTypeID || l.isNothingType(result) {
			l.setTerm(&Terminator{Kind: TermReturn})
		} else {
			l.setTerm(&Terminator{Kind: TermUnreachable})
		}
	}
	for i := range l.f.Blocks {
		if l.f.Blocks[i].Term.Kind == TermNone {
			l.f.Blocks[i].Term.Kind = TermUnreachable
		}
	}

	return l.f, nil
}
