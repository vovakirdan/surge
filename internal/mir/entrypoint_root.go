package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Keep the source signature for entrypoint argument and exit-code resolution.
// Its process activation is lowered independently as a coroutine on the root
// shard; sema rejects source calls to a synchronous entrypoint.
func (l *funcLowerer) lowerRootEntrypoint(fn *hir.Func) (*Func, error) {
	if l.types == nil || l.types.Strings == nil {
		return nil, fmt.Errorf("mir: root entrypoint requires a type and string registry")
	}
	body := *fn
	body.Flags |= hir.FuncAsync
	body.Flags &^= hir.FuncEntrypoint
	body.Name = "__surge_root$entry"
	root, err := l.forkLowerer().lowerFunc(l.allocFuncID(), &body)
	if err != nil {
		return nil, err
	}
	// The root uses its own ABI; explicitly async entrypoints can additionally
	// retain an ordinary callable body under their source symbol.
	root.Sym = symbols.NoSymbolID
	if root.Result == types.NoTypeID {
		root.Result = l.types.Builtins().Nothing
	}
	root.AsyncTaskType, _ = rootTaskTypes(l.types, root.Result)
	return root, nil
}

func routeEntrypointToRoot(start, root *Func, entrySymbol symbols.SymbolID, in *types.Interner, facts *sema.Result) error {
	if start == nil || root == nil || in == nil {
		return fmt.Errorf("mir: root entrypoint requires startup, body and types")
	}
	taskType, resultType := rootTaskTypes(in, root.Result)
	for bi := range start.Blocks {
		for ii := range start.Blocks[bi].Instrs {
			ins := start.Blocks[bi].Instrs[ii]
			if ins.Kind != InstrCall || ins.Call.Callee.Kind != CalleeSym || ins.Call.Callee.Sym != entrySymbol {
				continue
			}
			return replaceRootCall(start, bi, ii, &ins.Call, root, taskType, resultType, in, facts)
		}
	}
	return fmt.Errorf("mir: startup does not call its user entrypoint")
}

func replaceRootCall(start *Func, bi, ii int, original *CallInstr, root *Func, taskType, resultType types.TypeID, in *types.Interner, facts *sema.Result) error {
	task := addLocal(start, "__root_task", taskType, localFlagsFor(in, facts, taskType))
	result := addLocal(start, "__root_result", resultType, localFlagsFor(in, facts, resultType))
	resultView := Operand{Kind: OperandCopy, Type: resultType, Place: Place{Local: result}}
	success := addLocal(start, "__root_success", in.Builtins().Bool, LocalFlagCopy)
	continuation := newBlock(start)
	failed := newBlock(start)
	block := &start.Blocks[bi]
	start.Blocks[continuation].Instrs = append([]Instr(nil), block.Instrs[ii+1:]...)
	start.Blocks[continuation].Term = block.Term
	call := *original
	call.Callee = Callee{Kind: CalleeValue, Name: root.Name}
	call.HasDst, call.Dst = true, Place{Local: task}
	block.Instrs = append(block.Instrs[:ii],
		Instr{Kind: InstrCall, Call: call},
		Instr{Kind: InstrAwait, Await: AwaitInstr{Dst: Place{Local: result}, Task: Operand{Kind: OperandCopy, Type: taskType, Place: Place{Local: task}}}},
		Instr{Kind: InstrAssign, Assign: AssignInstr{Dst: Place{Local: success}, Src: RValue{Kind: RValueTagTest, TagTest: TagTest{Value: resultView, TagName: "Success"}}}},
	)
	block.Term = Terminator{Kind: TermIf, If: IfTerm{Cond: operandForLocal(start, success), Then: continuation, Else: failed}}
	var unpack []Instr
	if original.HasDst {
		unpack = append(unpack, Instr{Kind: InstrAssign, Assign: AssignInstr{Dst: original.Dst, Src: RValue{Kind: RValueTagPayload, TagPayload: TagPayload{
			Value: resultView, TagName: "Success", Index: 0, MoveOut: true,
		}}}})
	}
	unpack = append(unpack, Instr{Kind: InstrEnvelopeRelease, EnvelopeRelease: EnvelopeReleaseInstr{Place: Place{Local: result}}})
	start.Blocks[continuation].Instrs = append(unpack, start.Blocks[continuation].Instrs...)
	appendInstr(start, failed, Instr{Kind: InstrDrop, Drop: DropInstr{Place: Place{Local: result}}})
	appendInstr(start, failed, Instr{Kind: InstrCall, Call: CallInstr{
		Callee:       Callee{Kind: CalleeValue, Name: "rt_exit"},
		Args:         []Operand{{Kind: OperandConst, Type: in.Builtins().Int, Const: Const{Kind: ConstInt, Type: in.Builtins().Int, IntValue: 1}}},
		ArgContracts: borrowArgContracts(1),
	}})
	setBlockTerm(start, failed, Terminator{Kind: TermReturn})
	return nil
}
