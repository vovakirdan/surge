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
	// The unpack above emptied the frame's fields into locals, so the frame is
	// SPENT and says so — and, as in the async poll's entry, the write is
	// adjacent to the reads it describes: this is still the entry block, and
	// nothing between them can have handed the frame to another reader.
	//
	// Written only when there IS a frame. A capture-less crossing builds no
	// state at all and is handed a null one, so there is no storage here for a
	// word to describe and no store to make through a null pointer.
	if len(captures) > 0 {
		spent := frameStateWrite(stateLocal, FrameStateSpent, l.types.Builtins().Int)
		l.emit(&spent)
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
	rewriteSpawnOnPollReturns(l.f, stateLocal, ownedCaptures, len(captures) > 0, l.types.Builtins().Int)
	for i := range l.f.Blocks {
		if l.f.Blocks[i].Term.Kind == TermNone {
			l.f.Blocks[i].Term.Kind = TermUnreachable
		}
	}
	return l.f, nil
}

// rewriteSpawnOnPollReturns closes every return of a crossing body. hasFrame is
// false for a capture-less crossing, which is handed a null state and therefore
// has no word to write.
func rewriteSpawnOnPollReturns(f *Func, stateLocal LocalID, ownedCaptures []LocalID, hasFrame bool, intType types.TypeID) {
	if f == nil {
		return
	}
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		if bb.Term.Kind != TermReturn {
			continue
		}
		term := bb.Term.Return
		// A completed body never re-enters this frame. The captures were
		// unpacked into locals at entry, so nothing in the frame owns anything
		// and it is SPENT — whoever reclaims it reclaims storage alone.
		//
		// SPENT is truthful here ONLY BECAUSE the drops below run first. A COPY
		// capture that carries storage arrived as this crossing's own copy, and
		// the local is its only holder; leaving it alive while the frame claims
		// to hold nothing would strand exactly that copy. The order is the
		// claim: reclaim what the locals hold, and only then say the frame holds
		// nothing.
		for _, localID := range ownedCaptures {
			bb.Instrs = append(bb.Instrs, Instr{Kind: InstrDrop, Drop: DropInstr{Place: Place{Local: localID}}})
		}
		if hasFrame {
			bb.Instrs = append(bb.Instrs, frameStateWrite(stateLocal, FrameStateSpent, intType))
		}
		bb.Term = Terminator{Kind: TermAsyncReturn, AsyncReturn: AsyncReturnTerm{
			State: Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
		}}
		if term.HasValue {
			bb.Term.AsyncReturn.HasValue = true
			bb.Term.AsyncReturn.Value = term.Value
		}
	}
}

// appendFrameStatePacked writes the lifecycle word into a crossing's state
// literal, which is where a crossing frame is born PACKED: the captures below it
// are moved in, and the body cannot run before the destination has the frame.
//
// It is skipped for a capture-less crossing, and the condition is captureCount
// rather than a flag because that is exactly the condition the backends use to
// decide whether to build a frame at all — a state literal with no fields is
// lowered as a null state. Writing the word unconditionally would make those
// crossings allocate a frame to hold one number, and skipping it while the
// backend built one would leave the word unwritten, which is the thing this
// field exists to prevent. Tying both to one count keeps them one decision.
func (l *funcLowerer) appendFrameStatePacked(lit *StructLit, captureCount int) {
	if l == nil || lit == nil || captureCount == 0 {
		return
	}
	lit.Fields = append(lit.Fields, frameStatePackedField(l.types.Builtins().Int))
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
