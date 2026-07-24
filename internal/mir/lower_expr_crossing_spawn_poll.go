package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
		exit:           exitBB,
		hasResult:      hasResult,
		result:         Place{Local: resultLocal},
		tempFrameDepth: len(l.tempDropFrames),
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
		// A completed body never re-enters this state: the captures were
		// unpacked into locals at entry, so only the envelope box itself is
		// dead. Release it shallowly (the native lowering nulls the slot,
		// handing the runtime a null it never reads); the pending-side
		// __surge_drop_call path only runs for states that were never
		// handed off, so no state sees both releases.
		bb.Instrs = append(bb.Instrs, Instr{Kind: InstrCall, Call: CallInstr{
			Callee: Callee{Kind: CalleeValue, Name: AsyncStateFreeBuiltin},
			Args:   []Operand{{Kind: OperandCopy, Place: Place{Local: stateLocal}}},
		}})
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
