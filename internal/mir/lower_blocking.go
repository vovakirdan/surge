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
	// Transfers marks a capture the state literal MOVED in, so the unpack has
	// to take it out again: the caller's binding was spent, and a field left
	// looking initialized would be a second owner of what the body now holds.
	//
	// It is false for two unlike captures, and only one of them is free. A
	// capture owning no heap has nothing to hand on. A reference-counted one
	// was RETAINED into the field, so the frame holds a reference of its own
	// and something still has to give it back — see the unpack in
	// lowerBlockingFunc, which hands that reference to the body's local.
	Transfers bool
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

func (l *funcLowerer) blockingCaptureInfo(captures []hir.CapturedBinding) ([]blockingCaptureInfo, error) {
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
			// hir.CapturedBinding carries no capture mode — unlike a
			// crossing's, which sema classifies — so the question is asked of
			// the TYPE, through the same predicate the ordinary by-value
			// argument position uses. It answers the same thing here: the
			// state literal spends the caller's binding exactly as a call
			// spends an argument, and a reference-counted value is retained
			// into the field rather than moved out of the caller.
			Transfers: l.byValueArgContract(ty, false) == ArgContractTransferOwned,
		})
	}
	return out, nil
}

func (l *funcLowerer) blockingStateLiteral(stateType types.TypeID, captures []blockingCaptureInfo) (StructLit, error) {
	if stateType == types.NoTypeID {
		return StructLit{}, fmt.Errorf("mir: blocking: missing state type")
	}
	fields := make([]StructLitField, 0, len(captures)+1)
	// A blocking job's frame is built whether or not it captures anything, so
	// the lifecycle word is written unconditionally: an allocated frame whose
	// word was never written is the case this field exists to rule out.
	//
	// PACKED is true for the window this literal opens, and only for it. The
	// captures below arrive by the route their ownership asks for: a
	// transferring capture is MOVED in, while a reference-counted one is
	// RETAINED into the frame and leaves its holder standing. Both leave the
	// frame owing something, which is what PACKED says. A job the runtime
	// cancels before it ever
	// starts the body is reclaimed by destroying them through the frame's
	// descriptor — a walk, which is what PACKED asks for. The window closes at
	// the body's first instructions, where the captures come back out and
	// lowerBlockingFunc writes the other word.
	fields = append(fields, frameStatePackedField(l.types.Builtins().Int))
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
		if op, handled, err := l.lowerConstValue(capture.SymbolID, true, capture.Type); handled {
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
		// An OWNED capture was moved into the state, and this unpack is where
		// that ownership continues into the body: the state's field is spent,
		// the body is free to consume what it got, and the job's release must
		// not come back for it. That is a transfer, and the read SAYS so — a
		// plain read leaves the field looking initialized, which is a second
		// owner for anything the state is later destroyed through.
		//
		// A RETAINED capture stays a plain read, and the plain read is what
		// hands the frame's reference on. `Channel<T>` is the whole of this
		// family here: the literal retained a reference into the field
		// (captureOperand reads it consuming, which for a reference-counted
		// value is OperandRetain), and the job never gives that one back — the
		// worker spends the state cell before calling this body, so the release
		// frees the block without walking a field. Copying the handle word out
		// therefore moves that reference to the local, and the local owes it
		// back at every return — which is a drop obligation sema registers, in
		// registerBlockingBodyOwnership, beside the one it registers for the
		// transferring captures above.
		//
		// The reference-counted SCALAR the predicate also admits cannot arrive:
		// sema refuses a `float`-carrying blocking capture outright, because the
		// count is not atomic and the worker is another thread.
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: localID},
			Src: RValue{Kind: RValueField, Field: FieldAccess{
				Object:    Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
				FieldName: cap.FieldName,
				MoveOut:   cap.Transfers,
			}},
		}})
	}
	// Every capture that owned anything has now left the frame: a transferring
	// one was moved out, a retained one handed its reference to the local above.
	// So the frame owns nothing and says so — adjacent to the reads that made it
	// true, as in the crossing poll's entry: this is still the entry block, so
	// nothing between them can have handed the frame to another reader.
	//
	// The runtime asserts the same handover from its own side: the worker spends
	// the job's state cell immediately before it calls this body, so a cancel
	// landing mid-body releases the storage without walking it. That is what
	// makes SPENT the truth here rather than an ambition, and the two must not
	// be allowed to disagree — the cell answers a caller that already knows this
	// is a blocking job, the word answers a reader holding only the address.
	//
	// Written whether or not there are captures, because the submission builds a
	// frame either way and there is always storage here for the word to
	// describe. A capture-less CROSSING is the case that differs: it is handed a
	// null state, so there is nothing there to store into.
	spent := frameStateWrite(stateLocal, FrameStateSpent, l.types.Builtins().Int)
	l.emit(&spent)

	if err := l.lowerTaskBody(body); err != nil {
		return nil, err
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
