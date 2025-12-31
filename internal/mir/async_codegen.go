package mir

import (
	"fmt"

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
}

// buildAsyncPollEntry creates the entry block for the poll function with a switch on state.
func buildAsyncPollEntry(f *Func, stateLocal LocalID, variants []stateVariant, scopeLocal LocalID, failfast bool, boolType types.TypeID) BlockID {
	if f == nil {
		return NoBlockID
	}
	entryBB := newBlock(f)
	defaultBB := newBlock(f)
	setBlockTerm(f, defaultBB, Terminator{Kind: TermUnreachable})

	appendInstr(f, entryBB, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: stateLocal},
		Callee: Callee{Kind: CalleeValue, Name: "__task_state"},
	}})

	cases := make([]SwitchTagCase, 0, len(variants))
	for i := range variants {
		variantBB := newBlock(f)
		cases = append(cases, SwitchTagCase{TagName: variants[i].name, Target: variantBB})

		for idx, localID := range variants[i].locals {
			appendInstr(f, variantBB, Instr{Kind: InstrAssign, Assign: AssignInstr{
				Dst: Place{Local: localID},
				Src: RValue{Kind: RValueTagPayload, TagPayload: TagPayload{
					Value:   Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
					TagName: variants[i].name,
					Index:   idx,
				}},
			}})
		}
		if i == 0 && scopeLocal != NoLocalID {
			appendInstr(f, variantBB, Instr{Kind: InstrCall, Call: CallInstr{
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
		setBlockTerm(f, variantBB, Terminator{Kind: TermGoto, Goto: GotoTerm{Target: variants[i].resumeBB}})
	}

	setBlockTerm(f, entryBB, Terminator{Kind: TermSwitchTag, SwitchTag: SwitchTagTerm{
		Value:   Operand{Kind: OperandCopy, Place: Place{Local: stateLocal}},
		Cases:   cases,
		Default: defaultBB,
	}})

	return entryBB
}

// buildAsyncPendingBlocks creates the pending blocks that save state and yield.
func buildAsyncPendingBlocks(f *Func, stateLocal LocalID, sites []awaitSite, variants []stateVariant) error {
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
			Dst:    Place{Local: stateLocal},
			Callee: Callee{Kind: CalleeSym, Sym: variants[variantIdx].tagSym, Name: variants[variantIdx].name},
			Args:   args,
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
func buildAsyncConstructorState(f *Func, typesIn *types.Interner, semaRes *sema.Result, taskType, stateType types.TypeID, pollFnID FuncID, startVariant stateVariant) error {
	if f == nil {
		return nil
	}
	entry := newBlock(f)
	f.Entry = entry

	stateTmp := addLocal(f, "__state_init", stateType, localFlagsFor(typesIn, semaRes, stateType))
	taskTmp := addLocal(f, "__task", taskType, localFlagsFor(typesIn, semaRes, taskType))

	args := make([]Operand, 0, len(startVariant.locals))
	for _, localID := range startVariant.locals {
		args = append(args, operandForLocal(f, localID))
	}

	appendInstr(f, entry, Instr{Kind: InstrCall, Call: CallInstr{
		HasDst: true,
		Dst:    Place{Local: stateTmp},
		Callee: Callee{Kind: CalleeSym, Sym: startVariant.tagSym, Name: startVariant.name},
		Args:   args,
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
