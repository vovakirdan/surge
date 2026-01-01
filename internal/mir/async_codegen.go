package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

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
			args = append(args, operandForLocal(f, localID))
		}
		appendInstr(f, pendingBB, Instr{Kind: InstrCall, Call: CallInstr{
			HasDst: true,
			Dst:    Place{Local: payloadLocal},
			Callee: Callee{Kind: CalleeSym, Sym: variants[variantIdx].tagSym, Name: variants[variantIdx].name},
			Args:   args,
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
		case InstrTimeout:
			pollInstr.Timeout.PendBB = pendingBB
		default:
			return fmt.Errorf("mir: async: expected suspend instruction in %s", f.Name)
		}
	}
	return nil
}

// rewriteAsyncReturns transforms return terminators into AsyncReturn.
func rewriteAsyncReturns(f *Func, stateLocal LocalID) {
	if f == nil {
		return
	}
	for bi := range f.Blocks {
		bb := &f.Blocks[bi]
		if bb.Term.Kind != TermReturn {
			continue
		}
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
	for _, localID := range startVariant.locals {
		args = append(args, operandForLocal(f, localID))
	}

	appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: payloadTmp},
		Callee: Callee{Kind: CalleeSym, Sym: startVariant.tagSym, Name: startVariant.name},
		Args:   args,
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
			Type:  typesIn.Builtins().Int,
			Const: Const{Kind: ConstInt, Type: typesIn.Builtins().Int, IntValue: int64(pollFnID)},
		}, {
			Kind:  OperandMove,
			Place: Place{Local: stateTmp},
		}},
	}})
	setBlockTerm(f, entry, Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: Operand{Kind: OperandMove, Place: Place{Local: taskTmp}}}})
	return nil
}
