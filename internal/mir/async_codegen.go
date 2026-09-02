package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// frameStateWrite stores one lifecycle word into the frame this activation is
// holding. It is an ordinary field store through the frame pointer, which is
// what makes it usable on the paths where the frame must survive the write:
// nothing is released, moved or renamed, so the pointer the runtime holds keeps
// meaning what it meant.
func frameStateWrite(stateLocal LocalID, word int64, intType types.TypeID) Instr {
	return Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: asyncStateFieldPlace(stateLocal, FrameStateField),
		Src: RValue{Kind: RValueUse, Use: Operand{
			Kind:  OperandConst,
			Type:  intType,
			Const: Const{Kind: ConstInt, Type: intType, IntValue: word},
		}},
	}}
}

// frameStatePackedField is the lifecycle word as a struct-literal field, for the
// frames that are born PACKED at their construction site rather than reaching
// that state by a store.
func frameStatePackedField(intType types.TypeID) StructLitField {
	return StructLitField{
		Name:  FrameStateField,
		Value: Operand{Kind: OperandConst, Type: intType, Const: Const{Kind: ConstInt, Type: intType, IntValue: FrameStatePacked}},
	}
}

// asyncStateFieldPlace addresses one field of the state box a poll is holding.
func asyncStateFieldPlace(stateLocal LocalID, field string) Place {
	return Place{
		Local: stateLocal,
		Proj:  []PlaceProj{{Kind: PlaceProjField, FieldName: field, FieldIdx: -1}},
	}
}

// stateVariant describes one variant of the async state union.
type stateVariant struct {
	name     string
	tagSym   symbols.SymbolID
	locals   []LocalID
	resumeBB BlockID
	isStart  bool
}

// buildAsyncPollEntry creates the entry block for the poll function with a pc-based dispatch.
func buildAsyncPollEntry(f *Func, stateLocal, pcLocal, payloadLocal LocalID, variants []stateVariant, scopeLocal LocalID, failfast bool, boolType, intType types.TypeID) BlockID {
	if f == nil {
		return NoBlockID
	}
	entryBB := newBlock(f)
	dispatchBB := newBlock(f)
	defaultBB := newBlock(f)
	setBlockTerm(f, defaultBB, Terminator{Kind: TermUnreachable})

	appendInstr(f, entryBB, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: stateLocal},
		Callee: Callee{Kind: CalleeValue, Name: "__task_state"},
	}})
	appendInstr(f, entryBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: pcLocal},
		Src: RValue{Kind: RValueField, Field: FieldAccess{
			Object:    Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
			FieldName: asyncStatePcField,
		}},
	}})
	appendInstr(f, entryBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: payloadLocal},
		Src: RValue{Kind: RValueField, Field: FieldAccess{
			Object:    Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
			FieldName: asyncStatePayloadField,
			// The payload is unpacked into resumed locals, and the frame's
			// own copy of those bytes is dead from here on — the next
			// suspension overwrites it and no release ever walks it. The
			// payload therefore transfers out of the state; an alias here
			// would leave every resumed local without an owned root in MIR
			// even though the runtime protocol hands it over.
			MoveOut: true,
		}},
	}})
	// The read above emptied the frame, so the frame is now SPENT and says so.
	//
	// Between the two there is no window a second reader can be in. They are
	// adjacent instructions in one block — no call, no yield, nothing that could
	// hand this activation's frame to anybody — so no schedule exists in which
	// the frame is empty while the word still reads PACKED. That is what lets a
	// plain store stand here instead of an ordering dance.
	appendInstr(f, entryBB, frameStateWrite(stateLocal, FrameStateSpent, intType))
	setBlockTerm(f, entryBB, Terminator{Kind: TermGoto, Goto: GotoTerm{Target: dispatchBB}})

	condLocal := addLocal(f, "__pc_match", boolType, LocalFlagCopy)
	nextCheck := defaultBB
	for i := len(variants) - 1; i >= 0; i-- {
		variant := variants[i]
		caseBB := newBlock(f)
		checkBB := newBlock(f)

		appendInstr(f, checkBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: condLocal},
			Src: RValue{Kind: RValueBinaryOp, Binary: BinaryOp{
				Op:   ast.ExprBinaryEq,
				Left: operandForLocal(f, pcLocal),
				Right: Operand{
					Kind:  OperandConst,
					Type:  intType,
					Const: Const{Kind: ConstInt, Type: intType, IntValue: int64(variant.resumeBB)},
				},
			}},
		}})
		setBlockTerm(f, checkBB, Terminator{Kind: TermIf, If: IfTerm{
			Cond: operandForLocal(f, condLocal),
			Then: caseBB,
			Else: nextCheck,
		}})

		for idx, localID := range variant.locals {
			appendInstr(f, caseBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: localID},
				Src: RValue{Kind: RValueTagPayload, TagPayload: TagPayload{
					Value:   Operand{Kind: OperandCopy, Place: Place{Local: payloadLocal}},
					TagName: variant.name,
					Index:   idx,
					// The resume envelope is never walked by a release, so
					// the fields transfer out rather than aliasing an
					// envelope whose bytes outlive this unpack.
					MoveOut: true,
				}},
			}})
		}
		if variant.isStart && scopeLocal != NoLocalID {
			appendInstr(f, caseBB, Instr{Kind: InstrCall, Call: CallInstr{
				HasDst: true,
				Dst:    Place{Local: scopeLocal},
				Callee: Callee{Kind: CalleeValue, Name: "rt_scope_enter"},
				Args: []Operand{{
					Kind:  OperandConst,
					Type:  boolType,
					Const: Const{Kind: ConstBool, Type: boolType, BoolValue: failfast},
				}},
				ArgContracts: borrowArgContracts(1),
			}})
		}
		setBlockTerm(f, caseBB, Terminator{Kind: TermGoto, Goto: GotoTerm{Target: variant.resumeBB}})
		nextCheck = checkBB
	}

	setBlockTerm(f, dispatchBB, Terminator{Kind: TermGoto, Goto: GotoTerm{Target: nextCheck}})

	return entryBB
}

// buildAsyncPendingBlocks creates the pending blocks that save state and yield.
func buildAsyncPendingBlocks(f *Func, stateLocal, payloadLocal LocalID, sites []awaitSite, variants []stateVariant, intType types.TypeID) error {
	if f == nil {
		return nil
	}
	if len(variants) == 0 {
		return nil
	}
	for i := range sites {
		variantIdx := sites[i].stateIndex
		if variantIdx <= 0 || variantIdx >= len(variants) {
			return fmt.Errorf("mir: async: state index out of range")
		}
		pendingBB := newBlock(f)
		sites[i].pendingBB = pendingBB

		args := make([]Operand, 0, len(variants[variantIdx].locals))
		for _, localID := range variants[variantIdx].locals {
			args = append(args, operandForAsyncStateStore(f, localID))
		}
		appendInstr(f, pendingBB, Instr{Kind: InstrCall, Call: CallInstr{
			HasDst: true,
			Dst:    Place{Local: payloadLocal},
			Callee: Callee{Kind: CalleeSym, Sym: variants[variantIdx].tagSym, Name: variants[variantIdx].name},
			Args:   args,
			// A tag constructor: the resume payload union keeps every live
			// local across the suspension and is what releases them later.
			ArgContracts: storeArgContracts(len(args)),
		}})
		// The frame this invocation resumed from is the frame it suspends
		// into: it belongs to the activation, not to one suspension. Writing
		// the resume point and the repacked payload through it keeps the
		// runtime's pointer valid — which matters, because the block ends in
		// a yield and the C frame that could have held a fresh one is gone
		// the moment it does.
		appendInstr(f, pendingBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: asyncStateFieldPlace(stateLocal, asyncStatePcField),
			Src: RValue{Kind: RValueUse, Use: Operand{
				Kind:  OperandConst,
				Type:  intType,
				Const: Const{Kind: ConstInt, Type: intType, IntValue: int64(variants[variantIdx].resumeBB)},
			}},
		}})
		appendInstr(f, pendingBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: asyncStateFieldPlace(stateLocal, asyncStatePayloadField),
			Src: RValue{Kind: RValueUse, Use: operandForLocal(f, payloadLocal)},
		}})
		// The payload is in the frame, so the frame is PACKED and says so before
		// the yield hands it to the runtime. Reclaiming it from here on has to
		// walk what it holds; the word is how a reclamation that never saw this
		// block finds that out.
		appendInstr(f, pendingBB, frameStateWrite(stateLocal, FrameStatePacked, intType))
		setBlockTerm(f, pendingBB, Terminator{Kind: TermAsyncYield, AsyncYield: AsyncYieldTerm{
			State: operandForLocal(f, stateLocal),
		}})

		pollBB := sites[i].pollBB
		if pollBB < 0 || int(pollBB) >= len(f.Blocks) {
			return fmt.Errorf("mir: async: poll block out of range")
		}
		bb := &f.Blocks[pollBB]
		if sites[i].pollInstr < 0 || sites[i].pollInstr >= len(bb.Instrs) {
			return fmt.Errorf("mir: async: poll instruction out of range")
		}
		pollInstr := &bb.Instrs[sites[i].pollInstr]
		switch pollInstr.Kind {
		case InstrPoll:
			pollInstr.Poll.PendBB = pendingBB
		case InstrJoinAll:
			pollInstr.JoinAll.PendBB = pendingBB
		case InstrChanSend:
			pollInstr.ChanSend.PendBB = pendingBB
		case InstrChanRecv:
			pollInstr.ChanRecv.PendBB = pendingBB
		case InstrNetWait:
			pollInstr.NetWait.PendBB = pendingBB
		case InstrTimeout:
			pollInstr.Timeout.PendBB = pendingBB
		case InstrSelect:
			pollInstr.Select.PendBB = pendingBB
		case InstrCrossing:
			pollInstr.Crossing.PendBB = pendingBB
		default:
			return fmt.Errorf("mir: async: expected suspend instruction in %s", f.Name)
		}
	}
	return nil
}

// rewriteAsyncReturns transforms return terminators into AsyncReturn.
func rewriteAsyncReturns(f *Func, stateLocal LocalID, intType types.TypeID) {
	if f == nil {
		return
	}
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		if bb.Term.Kind != TermReturn {
			continue
		}
		// Both outcomes leave the frame empty, and both say so. A return is
		// reached from a resumed body, and a resume unpacked the payload into
		// locals at entry — so whatever reclaims this frame afterwards, on
		// either leg, must reclaim storage and nothing else. The two legs differ
		// in WHO reclaims it, not in what is left: a cancelled return leaves the
		// frame reachable from the runtime, an ordinary one does not.
		bb.Instrs = append(bb.Instrs, frameStateWrite(stateLocal, FrameStateSpent, intType))
		if bb.Term.Return.Cancelled {
			bb.Term = Terminator{Kind: TermAsyncReturnCancelled, AsyncReturnCancelled: AsyncReturnCancelledTerm{
				State: operandForLocal(f, stateLocal),
			}}
			continue
		}
		newTerm := Terminator{Kind: TermAsyncReturn, AsyncReturn: AsyncReturnTerm{
			State: operandForLocal(f, stateLocal),
		}}
		if bb.Term.Return.HasValue {
			newTerm.AsyncReturn.HasValue = true
			newTerm.AsyncReturn.Value = bb.Term.Return.Value
		}
		bb.Term = newTerm
	}
}

// buildAsyncConstructorState builds the constructor function that creates the initial task.
func buildAsyncConstructorState(f *Func, typesIn *types.Interner, semaRes *sema.Result, taskType, stateType, payloadType types.TypeID, pollFnID FuncID, startVariant stateVariant, intType types.TypeID, residents residentSet, startResidents []LocalID) error {
	if f == nil {
		return nil
	}
	entry := newBlock(f)
	f.Entry = entry

	stateTmp := addLocal(f, "__state_init", stateType, localFlagsFor(typesIn, semaRes, stateType))
	payloadTmp := addLocal(f, "__payload_init", payloadType, localFlagsFor(typesIn, semaRes, payloadType))
	taskTmp := addLocal(f, "__task", taskType, localFlagsFor(typesIn, semaRes, taskType))

	args := make([]Operand, 0, len(startVariant.locals))
	farTaskParams := make([]LocalID, 0, len(startVariant.locals))
	for _, localID := range startVariant.locals {
		args = append(args, operandForAsyncInitialStateStore(f, localID, typesIn))
		if int(localID) >= 0 && int(localID) < len(f.Locals) &&
			IsDirectFarTaskType(typesIn, f.Locals[localID].Type) {
			farTaskParams = append(farTaskParams, localID)
		}
	}
	for _, localID := range farTaskParams {
		appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{
			Callee: Callee{Kind: CalleeValue, Name: "rt_far_task_begin_transfer"},
			Args:   []Operand{{Kind: OperandCopy, Place: Place{Local: localID}}},
			// Handoff bookkeeping around the handle; it stores nothing.
			ArgContracts: borrowArgContracts(1),
		}})
	}

	appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: payloadTmp},
		Callee: Callee{Kind: CalleeSym, Sym: startVariant.tagSym, Name: startVariant.name},
		Args:   args,
		// A tag constructor: the start payload union keeps the captured
		// locals for the task's whole life.
		ArgContracts: storeArgContracts(len(args)),
	}})
	stateFields := []StructLitField{
		{
			// Built PACKED: the start payload below carries the
			// captured locals, and the task is handed this frame before
			// anything polls it. The word is true from the frame's first
			// instant, so a frame the scheduler drops before its first
			// poll is still reclaimable by what it says.
			Name:  FrameStateField,
			Value: Operand{Kind: OperandConst, Type: intType, Const: Const{Kind: ConstInt, Type: intType, IntValue: FrameStatePacked}},
		},
		{
			Name:  asyncStatePcField,
			Value: Operand{Kind: OperandConst, Type: intType, Const: Const{Kind: ConstInt, Type: intType, IntValue: int64(startVariant.resumeBB)}},
		},
		{
			Name:  asyncStatePayloadField,
			Value: operandForLocal(f, payloadTmp),
		},
	}
	// A promoted PARAMETER used to arrive in the start payload and be unpacked
	// into a fresh slot at every poll. It now goes straight to its resident field,
	// which is the same value delivered to storage that does not move.
	//
	// A promoted BODY local gets no entry here on purpose, and the omission is
	// meaningful rather than an oversight: it has no value yet. Its field's bytes
	// are zero, because buildComposite zeroes an extent precisely so that a
	// literal naming only some members leaves the rest reading as uninitialized
	// instead of as the corpse of an earlier temporary. The body's own assignment,
	// redirected onto the field, is its first write, and the frame never drops a
	// resident generically -- a drop reaches it only through the ordinary
	// obligation of the place it already is, which the body emits only where the
	// place is live. A frame discarded before that assignment therefore drops
	// nothing.
	for _, localID := range startResidents {
		stateFields = append(stateFields, StructLitField{
			Name:  residents.fields[localID],
			Value: operandForAsyncInitialStateStore(f, localID, typesIn),
		})
	}
	appendInstr(f, entry, Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: stateTmp},
		Src: RValue{Kind: RValueStructLit, StructLit: StructLit{
			TypeID: stateType,
			Fields: stateFields,
		}},
	}})
	appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: taskTmp},
		Callee: Callee{Kind: CalleeValue, Name: "__task_create"},
		Args: []Operand{{
			Kind:  OperandConst,
			Type:  typesIn.Builtins().Int64,
			Const: Const{Kind: ConstInt, Type: typesIn.Builtins().Int64, IntValue: int64(pollFnID)},
		}, {
			Kind:  OperandMove,
			Place: Place{Local: stateTmp},
		}},
		// The poll id is a plain number; the state box moves into the task,
		// which reclaims it by what the frame's lifecycle word then says.
		ArgContracts: []ArgContract{ArgContractBorrow, ArgContractStore},
	}})
	for _, localID := range farTaskParams {
		appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{
			Callee: Callee{Kind: CalleeValue, Name: "rt_far_task_finish_transfer"},
			Args: []Operand{
				{Kind: OperandCopy, Place: Place{Local: localID}},
				{Kind: OperandCopy, Place: Place{Local: taskTmp}},
			},
			// Handoff bookkeeping around both handles; it stores neither.
			ArgContracts: borrowArgContracts(2),
		}})
	}
	setBlockTerm(f, entry, Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: Operand{Kind: OperandMove, Place: Place{Local: taskTmp}}}})
	return nil
}
