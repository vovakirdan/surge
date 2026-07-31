package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/sema"
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
			SymbolID:    captures[i].Symbol,
			Name:        l.symbolName(captures[i].Symbol, field),
			Type:        captures[i].Type,
			FieldName:   field,
			CopyCapture: captures[i].Mode == sema.CrossingCaptureCopy,
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
	var ownedCaptures []LocalID
	for _, cap := range captures {
		localID := l.ensureLocal(cap.SymbolID, cap.Name, cap.Type, span)
		// An OWNED capture was moved into the state, and this unpack is where
		// that ownership continues into the body: the state keeps nothing, the
		// envelope is released shallowly, and the body is free to consume what
		// it got. That is a transfer, and the read now SAYS so instead of
		// leaving it to the comment below — a plain read would count a second
		// holder for a reference-counted capture, and nothing gives that one
		// back.
		//
		// A COPY capture is the opposite and stays a plain read: the local is a
		// second holder by design, which is exactly why one of the drops below
		// is synthesized for it.
		l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: localID},
			Src: RValue{Kind: RValueField, Field: FieldAccess{
				Object:    Operand{Kind: OperandCopy, Type: stateType, Place: Place{Local: stateLocal}},
				FieldName: cap.FieldName,
				MoveOut:   !cap.CopyCapture,
			}},
		}})
		// Unpacking moves the capture's ownership from the state's field into
		// this local, and for a COPY capture the local is the only holder from
		// here on: the envelope is released shallowly at every return, on the
		// stated assumption that nothing but the box is left, and that stops
		// being true unless the local reclaims what it holds. The local has no
		// scope-exit obligation of its own — sema knows the symbol as a binding
		// of the ENCLOSING function, not of this synthetic body — so the drop
		// is synthesized here, at the returns.
		//
		// Restricted to COPY captures on purpose. An owned capture may be
		// CONSUMED by the body (`take(own x)` is the whole point of moving one
		// in), and a synthesized drop cannot see that: move tracking is sema's,
		// and this local was never sema's to track. Dropping it unconditionally
		// double-frees whatever the body handed on. A Copy capture cannot be
		// consumed in that sense — a Copy read duplicates and leaves the source
		// intact — so nothing can have taken it away by the time this runs.
		if cap.CopyCapture && l.ownsHeap(cap.Type) {
			ownedCaptures = append(ownedCaptures, localID)
		}
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
	rewriteSpawnOnPollReturns(l.f, stateLocal, ownedCaptures)
	for i := range l.f.Blocks {
		if l.f.Blocks[i].Term.Kind == TermNone {
			l.f.Blocks[i].Term.Kind = TermUnreachable
		}
	}
	return l.f, nil
}

func rewriteSpawnOnPollReturns(f *Func, stateLocal LocalID, ownedCaptures []LocalID) {
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
		//
		// "Only the envelope is dead" holds because the unpacked locals are
		// reclaimed FIRST, right here. A capture that carries storage arrived
		// as this crossing's own copy, and the shallow release below would
		// abandon it.
		for _, localID := range ownedCaptures {
			bb.Instrs = append(bb.Instrs, Instr{Kind: InstrDrop, Drop: DropInstr{Place: Place{Local: localID}}})
		}
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
