package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// AsyncStateFreeBuiltin releases a consumed async resume frame: the state
// box handed out by __task_state plus the payload box its locals were
// unpacked from. The native backend lowers it to rt_free of both boxes; the
// VM's garbage collector reclaims them automatically, so it treats the call
// as a no-op. Without this release every suspend leaks one state+payload
// pair per poll.
const AsyncStateFreeBuiltin = "__async_state_free"

// asyncStateFreeInstr builds the release call for the current invocation's
// state and payload boxes. Operands are copies: the VM must keep its
// bookkeeping untouched (the call is a no-op there), and the native backend
// nulls the slots itself after freeing.
func asyncStateFreeInstr(payloadLocal, stateLocal LocalID) Instr {
	return Instr{Kind: InstrCall, Call: CallInstr{
		Callee: Callee{Kind: CalleeValue, Name: AsyncStateFreeBuiltin},
		Args: []Operand{
			{Kind: OperandCopy, Place: Place{Local: payloadLocal}},
			{Kind: OperandCopy, Place: Place{Local: stateLocal}},
		},
		// This call IS the release of both boxes, so each position must have
		// been handed something the caller genuinely owned — the copies above
		// are how the backend gets to null the slots itself, not a sign the
		// boxes are borrowed.
		ArgContracts: []ArgContract{ArgContractTransferOwned, ArgContractTransferOwned},
	}}
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
		}},
	}})
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

		// The boxes this invocation resumed from are dead once the live
		// locals are repacked below; release them before the rebuild
		// overwrites the only remaining handles.
		appendInstr(f, pendingBB, asyncStateFreeInstr(payloadLocal, stateLocal))

		args := make([]Operand, 0, len(variants[variantIdx].locals))
		for _, localID := range variants[variantIdx].locals {
			args = append(args, operandForLocal(f, localID))
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
		stateType := types.NoTypeID
		if int(stateLocal) >= 0 && int(stateLocal) < len(f.Locals) {
			stateType = f.Locals[stateLocal].Type
		}
		appendInstr(f, pendingBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
			Dst: Place{Local: stateLocal},
			Src: RValue{Kind: RValueStructLit, StructLit: StructLit{
				TypeID: stateType,
				Fields: []StructLitField{
					{
						Name:  asyncStatePcField,
						Value: Operand{Kind: OperandConst, Type: intType, Const: Const{Kind: ConstInt, Type: intType, IntValue: int64(variants[variantIdx].resumeBB)}},
					},
					{
						Name:  asyncStatePayloadField,
						Value: operandForLocal(f, payloadLocal),
					},
				},
			}},
		}})
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
func rewriteAsyncReturns(f *Func, stateLocal, payloadLocal LocalID) {
	if f == nil {
		return
	}
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		if bb.Term.Kind != TermReturn {
			continue
		}
		if bb.Term.Return.Cancelled {
			// A cancelled return's state may be re-parked and re-entered by
			// the runtime while scope children drain (apply_poll_outcome
			// stores it back into task->state), so its boxes must stay
			// alive; the pair is abandoned once the task completes.
			bb.Term = Terminator{Kind: TermAsyncReturnCancelled, AsyncReturnCancelled: AsyncReturnCancelledTerm{
				State: operandForLocal(f, stateLocal),
			}}
			continue
		}
		// A successful completion never re-enters this state: mark_done
		// clears task->state without reading the returned pointer. Release
		// the resume boxes; the native lowering nulls the state slot, so the
		// AsyncReturn terminator hands the runtime a null it never reads.
		bb.Instrs = append(bb.Instrs, asyncStateFreeInstr(payloadLocal, stateLocal))
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
func buildAsyncConstructorState(f *Func, typesIn *types.Interner, semaRes *sema.Result, taskType, stateType, payloadType types.TypeID, pollFnID FuncID, startVariant stateVariant, intType types.TypeID) error {
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
		args = append(args, operandForLocal(f, localID))
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
	appendInstr(f, entry, Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: stateTmp},
		Src: RValue{Kind: RValueStructLit, StructLit: StructLit{
			TypeID: stateType,
			Fields: []StructLitField{
				{
					Name:  asyncStatePcField,
					Value: Operand{Kind: OperandConst, Type: intType, Const: Const{Kind: ConstInt, Type: intType, IntValue: int64(startVariant.resumeBB)}},
				},
				{
					Name:  asyncStatePayloadField,
					Value: operandForLocal(f, payloadTmp),
				},
			},
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
		// which frees it later (AsyncStateFreeBuiltin).
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
